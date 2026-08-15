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
	songs []agents.Song
}

func (f fakeSimilarityProvider) GetSimilarSongsByTrackAll(context.Context, string, string, string, string, int) ([]agents.Song, error) {
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
