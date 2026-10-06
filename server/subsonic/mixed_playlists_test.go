package subsonic

import (
	"context"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
)

type mixedSubsonicService struct {
	playlists.Playlists
	playlist model.Playlist
	removed  []int
}

func (s *mixedSubsonicService) Get(context.Context, string) (*model.Playlist, error) {
	return &s.playlist, nil
}
func (s *mixedSubsonicService) GetWithTracks(context.Context, string) (*model.Playlist, error) {
	return &s.playlist, nil
}
func (s *mixedSubsonicService) Update(_ context.Context, _ string, _ *string, _ *string, _ *bool, _ []string, indices []int) error {
	s.removed = indices
	return nil
}
func TestMixedPlaylistSubsonicProjection(t *testing.T) {
	tests.Init(t, false)
	service := &mixedSubsonicService{playlist: model.Playlist{ID: "mixed", HasVideos: true, SongCount: 4, Duration: 150, Tracks: model.PlaylistTracks{
		{ID: "1", Source: "hometube", VideoID: "video-a"},
		{ID: "2", Source: "music", MediaFileID: "song-a", MediaFile: model.MediaFile{ID: "song-a", Duration: 20}},
		{ID: "3", Source: "hometube", VideoID: "video-b"},
		{ID: "4", Source: "music", MediaFileID: "song-b", MediaFile: model.MediaFile{ID: "song-b", Duration: 30}},
	}}}
	router := &Router{playlists: service}
	result, err := router.getPlaylist(context.Background(), "mixed")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Playlist.Entry) != 2 || result.Playlist.SongCount != 2 || result.Playlist.Duration != 50 {
		t.Fatalf("incorrect song-only projection: %+v", result.Playlist)
	}
	req := httptest.NewRequest("GET", "/updatePlaylist?playlistId=mixed&songIndexToRemove=0&songIndexToRemove=1", nil)
	if _, err = router.UpdatePlaylist(req); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(service.removed, []int{1, 3}) {
		t.Fatalf("incorrect mixed row indices: %v", service.removed)
	}
}
