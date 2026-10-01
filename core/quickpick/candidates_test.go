package quickpick

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
)

type shortlistMetrics struct{}

func (shortlistMetrics) SongRecentPlays(string, time.Time) (map[string]int64, error) {
	return map[string]int64{}, nil
}
func (shortlistMetrics) PlaylistMetrics(string, time.Time) (map[string]model.PlaylistPlayMetric, error) {
	return map[string]model.PlaylistPlayMetric{}, nil
}
func (shortlistMetrics) RecordPlaylistPlay(string, string, time.Time) error { return nil }
func (shortlistMetrics) ExposureMetrics(string, []string) (map[string]model.QuickPickExposureMetric, error) {
	return map[string]model.QuickPickExposureMetric{}, nil
}
func (shortlistMetrics) RecordExposures(string, []string, time.Time) error           { return nil }
func (shortlistMetrics) RecordImpressions(string, string, []string, time.Time) error { return nil }
func (shortlistMetrics) LatestView(string) (*model.QuickPickView, error)             { return nil, nil }
func (shortlistMetrics) RecordClick(string, string, string, time.Time) error         { return nil }

type batchShortlistMetrics struct {
	shortlistMetrics
	calls int
}

func (m *batchShortlistMetrics) PlaylistTrackAffinities(ids []string, _ map[string]float64) (map[string]float64, error) {
	m.calls++
	result := make(map[string]float64, len(ids))
	for _, id := range ids {
		result[id] = 1
	}
	return result, nil
}

type countingPlaylistRepo struct {
	*tests.MockPlaylistRepo
	withTracksCalls int
}

func (r *countingPlaylistRepo) GetWithTracks(id string, refreshSmartPlaylist, includeMissing bool) (*model.Playlist, error) {
	r.withTracksCalls++
	return r.MockPlaylistRepo.GetWithTracks(id, refreshSmartPlaylist, includeMissing)
}

func TestRankPlaylistsHydratesOnlyBoundedShortlist(t *testing.T) {
	base := tests.CreateMockPlaylistRepo()
	playlists := make(model.Playlists, 100)
	now := time.Now().UTC()
	for i := range playlists {
		playlists[i] = model.Playlist{ID: string(rune('a'+(i%26))) + string(rune('A'+i/26)), CreatedAt: now.Add(-time.Duration(i) * time.Hour)}
	}
	base.SetData(playlists)
	repo := &countingPlaylistRepo{MockPlaylistRepo: base}
	ds := &tests.MockDataStore{MockedPlaylist: repo}
	svc := &service{ds: ds, metrics: shortlistMetrics{}}
	result, err := svc.rankPlaylists(context.Background(), "user", now, map[string]float64{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) == 0 {
		t.Fatal("expected new playlists in exploration bucket")
	}
	if repo.withTracksCalls > playlistAffinityShortlist {
		t.Fatalf("GetWithTracks calls = %d, want <= %d", repo.withTracksCalls, playlistAffinityShortlist)
	}
}

func TestRankPlaylistsUsesBatchAffinityWhenAvailable(t *testing.T) {
	base := tests.CreateMockPlaylistRepo()
	playlists := make(model.Playlists, 40)
	now := time.Now().UTC()
	for i := range playlists {
		playlists[i] = model.Playlist{ID: string(rune('a'+(i%26))) + string(rune('A'+i/26)), CreatedAt: now.Add(-time.Duration(i) * time.Hour)}
	}
	base.SetData(playlists)
	repo := &countingPlaylistRepo{MockPlaylistRepo: base}
	metrics := &batchShortlistMetrics{}
	ds := &tests.MockDataStore{MockedPlaylist: repo}
	svc := &service{ds: ds, metrics: metrics}
	if _, err := svc.rankPlaylists(context.Background(), "user", now, map[string]float64{}); err != nil {
		t.Fatal(err)
	}
	if metrics.calls != 1 {
		t.Fatalf("batch affinity calls = %d, want 1", metrics.calls)
	}
	if repo.withTracksCalls != 0 {
		t.Fatalf("GetWithTracks calls = %d, want 0 when batch affinity is available", repo.withTracksCalls)
	}
}

type changingRecallRepo struct {
	*tests.MockMediaFileRepo
	pool        model.MediaFiles
	random      model.MediaFiles
	randomLimit int
	blocked     map[string]bool
}

func (r *changingRecallRepo) GetAll(...model.QueryOptions) (model.MediaFiles, error) {
	return r.pool, nil
}

func (r *changingRecallRepo) GetRandom(options ...model.QueryOptions) (model.MediaFiles, error) {
	r.randomLimit = options[0].Max
	return r.random, nil
}

func (r *changingRecallRepo) Get(id string) (*model.MediaFile, error) {
	if r.blocked[id] {
		return nil, model.ErrNotAuthorized
	}
	return r.MockMediaFileRepo.Get(id)
}

func TestRecallSongsAlwaysSamplesAndHydratesRetainedClicks(t *testing.T) {
	base := tests.CreateMockMediaFileRepo()
	base.SetData(model.MediaFiles{
		{ID: "clicked", Title: "Retained"},
		{ID: "missing", Missing: true},
		{ID: "blocked"},
	})
	repo := &changingRecallRepo{
		MockMediaFileRepo: base,
		random:            model.MediaFiles{{ID: "random", Title: "Discovery"}},
		blocked:           map[string]bool{"blocked": true},
	}
	for i := 0; i < 60; i++ {
		repo.pool = append(repo.pool, model.MediaFile{ID: fmt.Sprintf("favorite-%d", i), Annotations: model.Annotations{PlayCount: 50}})
	}
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: repo}}
	recalled, err := svc.recallSongs(context.Background(), "track:clicked", "track:missing", "track:blocked", "track:deleted")
	if err != nil {
		t.Fatal(err)
	}
	if repo.randomLimit != fallbackRandomMax {
		t.Fatalf("random sample limit = %d, want %d even with 60 favorites", repo.randomLimit, fallbackRandomMax)
	}
	seen := map[string]bool{}
	for _, song := range recalled {
		seen[song.file.ID] = true
	}
	if !seen["random"] || !seen["clicked"] || seen["missing"] || seen["blocked"] || seen["deleted"] {
		t.Fatalf("recalled song eligibility = %v", seen)
	}

	svc.metrics = fakeMetrics{view: &model.QuickPickView{
		ViewID: "previous", ItemKeys: []string{"track:clicked", "track:blocked", "track:missing"},
		ClickedItemKeys: []string{"track:clicked", "track:blocked", "track:missing"},
	}}
	response, err := svc.Get(context.Background(), "user")
	if err != nil || len(response.Items) != 12 {
		t.Fatalf("Get() = %#v, %v; want 12 library songs", response, err)
	}
	retained := false
	for _, item := range response.Items {
		retained = retained || item.Song.ID == "clicked"
		if item.Song.ID == "blocked" || item.Song.ID == "missing" {
			t.Fatalf("inaccessible or missing retained song was served: %#v", item)
		}
	}
	if !retained {
		t.Fatal("clicked song outside recall pool did not survive into discovery")
	}
}
