package quickpick

import (
	"context"
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
