package quickpick

import (
	"context"
	"testing"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
)

type fakeMetrics struct {
	recent    map[string]int64
	playlists map[string]model.PlaylistPlayMetric
}

func (f fakeMetrics) SongRecentPlays(string, time.Time) (map[string]int64, error) {
	return f.recent, nil
}
func (f fakeMetrics) PlaylistMetrics(string, time.Time) (map[string]model.PlaylistPlayMetric, error) {
	return f.playlists, nil
}
func (f fakeMetrics) RecordPlaylistPlay(string, string, time.Time) error { return nil }

type fakeSimilarityProvider struct {
	songs   []agents.Song
	bySeed map[string][]agents.Song
}

func (f fakeSimilarityProvider) GetSimilarSongsByTrackAll(context.Context, seedID string, string, string, string, int) ([]agents.Song, error) {
	if songs, ok := f.bySeed[seedID]; ok {
		return songs, nil
	}
	return f.songs, nil
}

func TestQuickPickKeepsTheRealFavoriteAtTheTop(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{{ID: "favorite", Title: "Favorite", Annotations: model.Annotations{PlayCount: 100}}}
	for i := 0; i < 8; i++ {
		files = append(files, model.MediaFile{ID: string(rune('a' + i)), Title: "Other", Annotations: model.Annotations{PlayCount: int64(i + 1)}})
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := New(ds, fakeMetrics{recent: map[string]int64{"favorite": 20}}, nil, nil)
	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 9 {
		t.Fatalf("got %d items, want 9", len(response.Items))
	}
	if response.Items[0].Song == nil || response.Items[0].Song.ID != "favorite" {
		t.Fatalf("favorite was rotated out of the top position: %#v", response.Items[0])
	}
}

func TestQuickPickSurfacesLibraryMatchedRecommendations(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Genre: "Pop", Annotations: model.Annotations{PlayCount: 50}},
		{ID: "matched", Title: "Similar One", Artist: "Other Artist", Genre: "Pop", Annotations: model.Annotations{PlayCount: 1}},
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		agents: fakeSimilarityProvider{songs: []agents.Song{
			{ID: "matched", Name: "Similar One", Artists: []agents.Artist{{Name: "Other Artist"}}},
		}},
		matcher: matcher.New(ds),
	}
	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	var rec *model.QuickPickItem
	for i := range response.Items {
		if response.Items[i].Kind == model.QuickPickRecommendationKind {
			rec = &response.Items[i]
			break
		}
	}
	if rec == nil {
		t.Fatalf("expected a recommendation tile, got %#v", response.Items)
	}
	if rec.Song == nil || rec.Song.ID != "matched" {
		t.Fatalf("recommendation should carry its matched library song, got %#v", rec)
	}
	if rec.Recommendation == nil || rec.Recommendation.Title != "Similar One" {
		t.Fatalf("recommendation metadata missing: %#v", rec)
	}
}

func TestQuickPickOrdersMatchedRecommendationsBySimilarity(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	media.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Annotations: model.Annotations{PlayCount: 50}},
		{ID: "matched-low", Title: "Low Match", Artist: "Other Artist"},
		{ID: "matched-high", Title: "High Match", Artist: "Other Artist"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		agents: fakeSimilarityProvider{bySeed: map[string][]agents.Song{"seed": {
			{ID: "matched-low", Name: "Low Match", Artists: []agents.Artist{{Name: "Other Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.2, NormalizedScore: 0.2}}},
			{ID: "matched-high", Name: "High Match", Artists: []agents.Artist{{Name: "Other Artist"}}, SimilarityScores: []agents.SimilarityScore{{Provider: "provider", Score: 0.9, NormalizedScore: 0.9}}},
		}}},
		matcher: matcher.New(ds),
	}

	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	var recommendationIDs []string
	for _, item := range response.Items {
		if item.Kind == model.QuickPickRecommendationKind {
			recommendationIDs = append(recommendationIDs, item.Song.ID)
		}
	}
	if len(recommendationIDs) != 2 {
		t.Fatalf("got %d recommendation tiles, want 2: %#v", len(recommendationIDs), response.Items)
	}
	if recommendationIDs[0] != "matched-high" || recommendationIDs[1] != "matched-low" {
		t.Fatalf("recommendation order = %v, want [matched-high matched-low]", recommendationIDs)
	}
}

func TestQuickPickKeepsDistinctMBIDRecommendationsWithSharedMetadata(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	media.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Annotations: model.Annotations{PlayCount: 50}},
		{ID: "library-a", Title: "Shared Track", Artist: "Shared Artist", MbzRecordingID: "recording-a"},
		{ID: "library-b", Title: "Shared Track", Artist: "Shared Artist", MbzRecordingID: "recording-b"},
	})
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		agents: fakeSimilarityProvider{bySeed: map[string][]agents.Song{"seed": {
			{Name: "Shared Track", MBID: "recording-a", Artists: []agents.Artist{{Name: "Shared Artist"}}, CandidateID: "mbid:recording-a"},
			{Name: "Shared Track", MBID: "recording-b", Artists: []agents.Artist{{Name: "Shared Artist"}}, CandidateID: "mbid:recording-b"},
		}}},
		matcher: matcher.New(ds),
	}

	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, item := range response.Items {
		if item.Kind == model.QuickPickRecommendationKind {
			got[item.Recommendation.RecordingMBID] = item.Song.ID
		}
	}
	if len(got) != 2 || got["recording-a"] != "library-a" || got["recording-b"] != "library-b" {
		t.Fatalf("distinct recommendations = %#v, want both recording-a/library-a and recording-b/library-b", got)
	}
}

func TestQuickPickSkipsUnmatchedRecommendations(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	media.SetData(model.MediaFiles{
		{ID: "seed", Title: "Seed Song", Artist: "Seed Artist", Genre: "Pop", Annotations: model.Annotations{PlayCount: 50}},
	})
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := &service{
		ds:      ds,
		metrics: fakeMetrics{recent: map[string]int64{"seed": 20}},
		agents: fakeSimilarityProvider{songs: []agents.Song{
			{ID: "not-in-library", Name: "Fresh Track", Artists: []agents.Artist{{Name: "New Artist"}}},
		}},
		matcher: matcher.New(ds),
	}
	response, err := svc.Get(context.Background(), "user")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range response.Items {
		if item.Kind == model.QuickPickRecommendationKind {
			t.Fatalf("unmatched recommendation should not be surfaced: %#v", item)
		}
	}
}
