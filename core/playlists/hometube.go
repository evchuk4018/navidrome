package playlists

import (
	"context"
	"fmt"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

func videoRepository(ctx context.Context, ds model.DataStore) model.HomeTubeVideoRepository {
	return ds.Resource(ctx, model.HomeTubeVideo{}).(model.HomeTubeVideoRepository)
}

func (s *playlists) GetVideo(ctx context.Context, id string) (*model.HomeTubeVideo, error) {
	return videoRepository(ctx, s.ds).Get(id)
}
func (s *playlists) SaveVideo(ctx context.Context, video *model.HomeTubeVideo) error {
	return videoRepository(ctx, s.ds).Put(video)
}

func (s *playlists) AddEntries(ctx context.Context, playlistID string, entries []model.PlaylistEntry) (int, error) {
	if _, err := s.checkTracksEditable(ctx, playlistID); err != nil {
		return 0, err
	}
	count := 0
	err := s.ds.WithTxImmediate(func(tx model.DataStore) error {
		for _, entry := range entries {
			if entry.ID == "" {
				return fmt.Errorf("entry ID is required")
			}
			switch entry.Source {
			case "", "music":
				if _, err := tx.MediaFile(ctx).Get(entry.ID); err != nil {
					return err
				}
			case "hometube":
				if entry.Video != nil {
					if entry.Video.ID != entry.ID {
						return fmt.Errorf("video ID mismatch")
					}
					if err := videoRepository(ctx, tx).Put(entry.Video); err != nil {
						return err
					}
				}
				if _, err := videoRepository(ctx, tx).Get(entry.ID); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported entry source")
			}
		}
		var err error
		count, err = tx.Playlist(ctx).Tracks(playlistID, false).AddEntries(entries)
		return err
	})
	return count, err
}

func (s *playlists) SetVideoFavorite(ctx context.Context, id string, favorite bool) error {
	user, ok := request.UserFrom(ctx)
	if !ok {
		return model.ErrNotAuthorized
	}
	return s.ds.WithTxImmediate(func(tx model.DataStore) error {
		repo := videoRepository(ctx, tx)
		if _, err := repo.Get(id); err != nil {
			return err
		}
		if err := repo.SetFavorite(id, favorite); err != nil {
			return err
		}
		playlist, err := findLikedMusicPlaylist(ctx, tx.Playlist(ctx), user.ID)
		if err != nil {
			return err
		}
		if playlist == nil {
			if !favorite {
				return nil
			}
			playlist = &model.Playlist{Name: LikedMusicPlaylistName, OwnerID: user.ID, Public: false}
			if err := tx.Playlist(ctx).Put(playlist); err != nil {
				return err
			}
		}
		if playlist.IsSmartPlaylist() {
			return model.ErrNotAuthorized
		}
		if playlist.Public {
			playlist.Public = false
			if err := tx.Playlist(ctx).Put(playlist, "public"); err != nil {
				return err
			}
		}
		tracks := tx.Playlist(ctx).Tracks(playlist.ID, false)
		all, err := tracks.GetAll()
		if err != nil {
			return err
		}
		var positions []string
		for _, track := range all {
			if track.Source == "hometube" && track.VideoID == id {
				positions = append(positions, track.ID)
			}
		}
		if favorite {
			if len(positions) > 0 {
				return nil
			}
			_, err = tracks.AddEntries([]model.PlaylistEntry{{Source: "hometube", ID: id}})
			return err
		}
		if len(positions) > 0 {
			return tracks.Delete(positions...)
		}
		return nil
	})
}
