package playlists_test

import (
	"context"
	"errors"
	"testing"

	"github.com/navidrome/navidrome/core/playlists"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/criteria"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/persistence"
	"github.com/navidrome/navidrome/tests"
)

func TestMixedPlaylistPersistenceAndPermissions(t *testing.T) {
	tests.Init(t, false)
	ctx := request.WithUser(context.Background(), model.User{ID: "mixed-owner", IsAdmin: true})
	db.Init(ctx)
	ds := persistence.New(db.Db())
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(ds.User(ctx).Put(&model.User{ID: "mixed-owner", UserName: "mixed-owner", IsAdmin: true}))
	must(ds.MediaFile(ctx).Put(&model.MediaFile{ID: "mixed-song", Title: "Song", LibraryID: 1, Path: "/mixed/song.mp3"}))
	svc := playlists.NewPlaylists(ds, nil)
	id, err := svc.Create(ctx, "", "Mixed", nil)
	must(err)
	video := &model.HomeTubeVideo{ID: "mixed-video", Title: "Video", ChannelName: "Channel", DurationSeconds: 60}
	entries := []model.PlaylistEntry{{Source: "music", ID: "mixed-song"}, {Source: "hometube", ID: video.ID, Video: video}, {Source: "hometube", ID: video.ID}}
	count, err := svc.AddEntries(ctx, id, entries)
	must(err)
	if count != 3 {
		t.Fatalf("added %d entries", count)
	}
	saved, err := svc.GetWithTracks(ctx, id)
	must(err)
	if len(saved.Tracks) != 3 || !saved.HasVideos || len(saved.MediaFiles()) != 1 {
		t.Fatalf("unexpected mixed playlist: %+v", saved)
	}
	// Legacy replacement cannot silently erase saved videos, including an empty replacement.
	if _, err = svc.Create(ctx, id, "Replacement", nil); err == nil {
		t.Fatal("legacy replacement accepted")
	}
	must(svc.ReorderTrack(ctx, id, 3, 1))
	saved, err = svc.GetWithTracks(ctx, id)
	must(err)
	if saved.Tracks[0].VideoID != video.ID {
		t.Fatal("video reorder failed")
	}
	intruder := request.WithUser(ctx, model.User{ID: "mixed-intruder"})
	if _, err = svc.AddEntries(intruder, id, entries); err == nil {
		t.Fatal("unauthorized append accepted")
	}
	smart := &model.Playlist{Name: "Smart", OwnerID: "mixed-owner", Rules: &criteria.Criteria{Expression: criteria.Contains{"title": "Song"}}}
	must(ds.Playlist(ctx).Put(smart))
	if _, err = svc.AddEntries(ctx, smart.ID, entries); !errors.Is(err, model.ErrNotAuthorized) {
		t.Fatalf("smart playlist append: %v", err)
	}
	// Validation and metadata writes are one transaction.
	bad := &model.HomeTubeVideo{ID: "rolled-back-video", Title: "Rollback"}
	if _, err = svc.AddEntries(ctx, id, []model.PlaylistEntry{{Source: "hometube", ID: bad.ID, Video: bad}, {Source: "unknown", ID: "x"}}); err == nil {
		t.Fatal("bad source accepted")
	}
	if _, err = svc.GetVideo(ctx, bad.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("metadata was not rolled back: %v", err)
	}
	must(svc.SetVideoFavorite(ctx, video.ID, true))
	must(svc.SetVideoFavorite(ctx, video.ID, true))
	liked, err := svc.GetAll(ctx)
	must(err)
	var likedID string
	for _, p := range liked {
		if p.Name == playlists.LikedMusicPlaylistName && p.OwnerID == "mixed-owner" {
			likedID = p.ID
			if p.Public {
				t.Fatal("favorites playlist is public")
			}
		}
	}
	likedPlaylist, err := svc.GetWithTracks(ctx, likedID)
	must(err)
	if len(likedPlaylist.Tracks) != 1 || !likedPlaylist.Tracks[0].Video.Starred {
		t.Fatalf("video favorite was not synchronized: %+v", likedPlaylist.Tracks)
	}
	other, err := svc.GetVideo(intruder, video.ID)
	must(err)
	if other.Starred {
		t.Fatal("favorite leaked between users")
	}
	must(svc.SetVideoFavorite(ctx, video.ID, false))
	likedPlaylist, err = svc.GetWithTracks(ctx, likedID)
	must(err)
	if len(likedPlaylist.Tracks) != 0 {
		t.Fatal("unfavorite did not remove video")
	}
	must(svc.RemoveTracks(ctx, id, []string{"1"}))
	saved, err = svc.GetWithTracks(ctx, id)
	must(err)
	if len(saved.Tracks) != 2 {
		t.Fatal("removal failed")
	}
}
