package quickpick

import (
	"context"
	"testing"
	"time"

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

func TestQuickPickKeepsTheRealFavoriteAtTheTop(t *testing.T) {
	media := tests.CreateMockMediaFileRepo()
	files := model.MediaFiles{{ID: "favorite", Title: "Favorite", Annotations: model.Annotations{PlayCount: 100}}}
	for i := 0; i < 8; i++ {
		files = append(files, model.MediaFile{ID: string(rune('a' + i)), Title: "Other", Annotations: model.Annotations{PlayCount: int64(i + 1)}})
	}
	media.SetData(files)
	ds := &tests.MockDataStore{MockedMediaFile: media, MockedPlaylist: tests.CreateMockPlaylistRepo()}
	svc := New(ds, fakeMetrics{recent: map[string]int64{"favorite": 20}})
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
