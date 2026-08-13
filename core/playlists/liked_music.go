package playlists

import (
	"context"
	"fmt"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

const LikedMusicPlaylistName = "liked music"

// SyncLikedMusic keeps the current user's automatic liked-music playlist in sync with
// a song's starred annotation. The caller should provide the datastore from its active
// transaction when atomicity with another annotation change is required.
//
// The playlist is deliberately resolved by both owner and name. Admin users can see
// every playlist, but must never accidentally mutate another user's liked-music list.
func SyncLikedMusic(ctx context.Context, ds model.DataStore, mediaFileID string, liked bool) (string, error) {
	if strings.TrimSpace(mediaFileID) == "" {
		return "", fmt.Errorf("media file id is required")
	}
	owner, ok := request.UserFrom(ctx)
	if !ok || strings.TrimSpace(owner.ID) == "" {
		return "", model.ErrNotAuthorized
	}

	playlist, err := findLikedMusicPlaylist(ctx, ds.Playlist(ctx), owner.ID)
	if err != nil {
		return "", err
	}

	if playlist == nil {
		if !liked {
			return "", nil
		}
		playlist = &model.Playlist{
			Name:    LikedMusicPlaylistName,
			OwnerID: owner.ID,
			Public:  false,
		}
		if err := ds.Playlist(ctx).Put(playlist); err != nil {
			return "", fmt.Errorf("create %q playlist: %w", LikedMusicPlaylistName, err)
		}
	}

	if playlist.IsSmartPlaylist() {
		return "", fmt.Errorf("playlist %q is a smart playlist and cannot be synchronized", LikedMusicPlaylistName)
	}

	// The automatic playlist is private. Enforce that invariant if a user already had a
	// manually-created playlist with the reserved name.
	if playlist.Public {
		playlist.Public = false
		if err := ds.Playlist(ctx).Put(playlist, "public"); err != nil {
			return "", fmt.Errorf("make %q playlist private: %w", LikedMusicPlaylistName, err)
		}
	}

	tracks := ds.Playlist(ctx).Tracks(playlist.ID, false)
	if tracks == nil {
		return "", fmt.Errorf("load %q playlist tracks: %w", LikedMusicPlaylistName, model.ErrNotFound)
	}

	if liked {
		ids, err := tracks.GetMediaFileIDs()
		if err != nil {
			return "", fmt.Errorf("read %q playlist tracks: %w", LikedMusicPlaylistName, err)
		}
		for _, id := range ids {
			if id == mediaFileID {
				return playlist.ID, nil
			}
		}
		if _, err := tracks.Add([]string{mediaFileID}); err != nil {
			return "", fmt.Errorf("add song to %q playlist: %w", LikedMusicPlaylistName, err)
		}
		return playlist.ID, nil
	}

	allTracks, err := tracks.GetAll()
	if err != nil {
		return "", fmt.Errorf("read %q playlist tracks: %w", LikedMusicPlaylistName, err)
	}
	positions := make([]string, 0)
	for _, track := range allTracks {
		if track.MediaFileID == mediaFileID {
			positions = append(positions, track.ID)
		}
	}
	if len(positions) > 0 {
		if err := tracks.Delete(positions...); err != nil {
			return "", fmt.Errorf("remove song from %q playlist: %w", LikedMusicPlaylistName, err)
		}
	}
	return playlist.ID, nil
}

func findLikedMusicPlaylist(ctx context.Context, repo model.PlaylistRepository, ownerID string) (*model.Playlist, error) {
	playlists, err := repo.GetAll(model.QueryOptions{
		Filters: squirrel.And{
			squirrel.Eq{"playlist.name": LikedMusicPlaylistName},
			squirrel.Eq{"playlist.owner_id": ownerID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("find %q playlist: %w", LikedMusicPlaylistName, err)
	}

	var match *model.Playlist
	for i := range playlists {
		playlist := &playlists[i]
		if playlist.OwnerID != ownerID || !strings.EqualFold(strings.TrimSpace(playlist.Name), LikedMusicPlaylistName) {
			continue
		}
		if match != nil && match.ID != playlist.ID {
			return nil, fmt.Errorf("multiple %q playlists exist for user %q", LikedMusicPlaylistName, ownerID)
		}
		match = playlist
	}
	return match, nil
}
