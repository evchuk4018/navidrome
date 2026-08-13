package playlists_test

import (
	"context"
	"testing"

	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/criteria"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/tests"
)

func TestSyncLikedMusicCreatesPrivatePlaylistAndAddsSong(t *testing.T) {
	ds, playlistRepo, tracks := likedMusicStore(nil, nil)
	ctx := request.WithUser(context.Background(), model.User{ID: "user-1"})

	id, err := playlists.SyncLikedMusic(ctx, ds, "song-1", true)
	if err != nil {
		t.Fatalf("SyncLikedMusic returned error: %v", err)
	}
	if id == "" || playlistRepo.Last == nil {
		t.Fatalf("expected a created playlist, got id=%q playlist=%+v", id, playlistRepo.Last)
	}
	if playlistRepo.Last.Name != playlists.LikedMusicPlaylistName || playlistRepo.Last.OwnerID != "user-1" || playlistRepo.Last.Public {
		t.Fatalf("unexpected automatic playlist: %+v", playlistRepo.Last)
	}
	if len(tracks.AddedIds) != 1 || tracks.AddedIds[0] != "song-1" {
		t.Fatalf("expected song to be added once, got %v", tracks.AddedIds)
	}
}

func TestSyncLikedMusicOwnerScopesAdminLookup(t *testing.T) {
	existing := model.Playlist{ID: "other-liked", Name: "LIKED MUSIC", OwnerID: "other-user"}
	ds, playlistRepo, _ := likedMusicStore(model.Playlists{existing}, nil)
	ctx := request.WithUser(context.Background(), model.User{ID: "admin-1", IsAdmin: true})

	_, err := playlists.SyncLikedMusic(ctx, ds, "song-1", true)
	if err != nil {
		t.Fatalf("SyncLikedMusic returned error: %v", err)
	}
	if playlistRepo.Last == nil || playlistRepo.Last.OwnerID != "admin-1" {
		t.Fatalf("expected a playlist owned by the current admin, got %+v", playlistRepo.Last)
	}
}

func TestSyncLikedMusicReusesPrivatePlaylistAndDoesNotDuplicate(t *testing.T) {
	existing := model.Playlist{ID: "liked-1", Name: "Liked Music", OwnerID: "user-1", Public: true}
	tracks := &tests.MockPlaylistTrackRepo{Data: model.PlaylistTracks{{ID: "1", MediaFileID: "song-1"}}}
	ds, playlistRepo, _ := likedMusicStore(model.Playlists{existing}, tracks)
	ctx := request.WithUser(context.Background(), model.User{ID: "user-1"})

	id, err := playlists.SyncLikedMusic(ctx, ds, "song-1", true)
	if err != nil {
		t.Fatalf("SyncLikedMusic returned error: %v", err)
	}
	if id != "liked-1" || playlistRepo.Last == nil || playlistRepo.Last.Public {
		t.Fatalf("expected the existing playlist to be reused and made private: id=%q last=%+v", id, playlistRepo.Last)
	}
	if len(tracks.AddedIds) != 0 {
		t.Fatalf("expected no duplicate track, got %v", tracks.AddedIds)
	}
}

func TestSyncLikedMusicUnstarRemovesEveryOccurrence(t *testing.T) {
	existing := model.Playlist{ID: "liked-1", Name: playlists.LikedMusicPlaylistName, OwnerID: "user-1"}
	tracks := &tests.MockPlaylistTrackRepo{Data: model.PlaylistTracks{
		{ID: "1", MediaFileID: "song-1"},
		{ID: "2", MediaFileID: "song-2"},
		{ID: "3", MediaFileID: "song-1"},
	}}
	ds, _, _ := likedMusicStore(model.Playlists{existing}, tracks)
	ctx := request.WithUser(context.Background(), model.User{ID: "user-1"})

	_, err := playlists.SyncLikedMusic(ctx, ds, "song-1", false)
	if err != nil {
		t.Fatalf("SyncLikedMusic returned error: %v", err)
	}
	if len(tracks.DeletedIds) != 2 || tracks.DeletedIds[0] != "1" || tracks.DeletedIds[1] != "3" {
		t.Fatalf("expected both matching positions to be deleted, got %v", tracks.DeletedIds)
	}
}

func TestSyncLikedMusicRejectsSmartPlaylist(t *testing.T) {
	existing := model.Playlist{
		ID:      "liked-smart",
		Name:    playlists.LikedMusicPlaylistName,
		OwnerID: "user-1",
		Rules:   &criteria.Criteria{Expression: criteria.Contains{"title": "song"}},
	}
	ds, _, tracks := likedMusicStore(model.Playlists{existing}, &tests.MockPlaylistTrackRepo{})
	ctx := request.WithUser(context.Background(), model.User{ID: "user-1"})

	if _, err := playlists.SyncLikedMusic(ctx, ds, "song-1", true); err == nil {
		t.Fatal("expected smart playlist error")
	}
	if len(tracks.AddedIds) != 0 {
		t.Fatalf("smart playlist should not receive tracks: %v", tracks.AddedIds)
	}
}

func TestSyncLikedMusicRequiresUser(t *testing.T) {
	ds, _, _ := likedMusicStore(nil, nil)
	if _, err := playlists.SyncLikedMusic(context.Background(), ds, "song-1", true); err != model.ErrNotAuthorized {
		t.Fatalf("expected ErrNotAuthorized, got %v", err)
	}
}

func likedMusicStore(data model.Playlists, tracks model.PlaylistTrackRepository) (*tests.MockDataStore, *tests.MockPlaylistRepo, *tests.MockPlaylistTrackRepo) {
	playlistRepo := tests.CreateMockPlaylistRepo()
	playlistRepo.SetData(data)
	trackRepo, ok := tracks.(*tests.MockPlaylistTrackRepo)
	if !ok {
		trackRepo = &tests.MockPlaylistTrackRepo{}
	}
	playlistRepo.TracksRepo = trackRepo
	return &tests.MockDataStore{MockedPlaylist: playlistRepo}, playlistRepo, trackRepo
}
