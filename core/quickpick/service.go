package quickpick

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/navidrome/navidrome/model"
)

type Service interface {
	Get(context.Context, string) (*model.QuickPickResponse, error)
	RecordPlaylistPlay(context.Context, string, string) error
}

type service struct {
	ds      model.DataStore
	metrics model.QuickPickMetricsRepository
}

func New(ds model.DataStore, metrics model.QuickPickMetricsRepository) Service {
	return &service{ds: ds, metrics: metrics}
}

type songCandidate struct {
	song  model.MediaFile
	score float64
}

type playlistCandidate struct {
	playlist model.Playlist
	score    float64
}

func (s *service) Get(ctx context.Context, userID string) (*model.QuickPickResponse, error) {
	now := time.Now().UTC()
	recent, err := s.metrics.SongRecentPlays(userID, now.AddDate(0, 0, -30))
	if err != nil {
		return nil, err
	}

	byID := map[string]model.MediaFile{}
	for _, options := range []model.QueryOptions{
		{Sort: "play_count", Order: "desc", Max: 150},
		{Sort: "play_date", Order: "desc", Max: 150},
		{Sort: "starred_at", Order: "desc", Max: 100},
	} {
		files, err := s.ds.MediaFile(ctx).GetAll(options)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			byID[file.ID] = file
		}
	}

	songs := make([]songCandidate, 0, len(byID))
	songScores := make(map[string]float64, len(byID))
	for _, song := range byID {
		score := 3*math.Log1p(float64(song.PlayCount)) + 5*math.Log1p(float64(recent[song.ID]))
		if song.PlayDate != nil {
			days := math.Max(0, now.Sub(song.PlayDate.UTC()).Hours()/24)
			score += 5 * math.Exp(-days/14)
		}
		if song.Starred {
			score += 4
		}
		if score == 0 && !song.CreatedAt.IsZero() {
			days := math.Max(0, now.Sub(song.CreatedAt.UTC()).Hours()/24)
			score = .1 * math.Exp(-days/30)
		}
		songs = append(songs, songCandidate{song: song, score: score})
		songScores[song.ID] = score
	}
	sort.SliceStable(songs, func(i, j int) bool { return songs[i].score > songs[j].score })

	playlistMetrics, err := s.metrics.PlaylistMetrics(userID, now.AddDate(0, 0, -30))
	if err != nil {
		return nil, err
	}
	playlists, err := s.ds.Playlist(ctx).GetAll(model.QueryOptions{Sort: "name", Order: "asc", Max: 100})
	if err != nil {
		return nil, err
	}
	pls := make([]playlistCandidate, 0, len(playlists))
	for _, playlist := range playlists {
		metric := playlistMetrics[playlist.ID]
		score := 3*math.Log1p(float64(metric.TotalStarts)) + 5*math.Log1p(float64(metric.RecentStarts))
		if metric.LastPlayed != nil {
			days := math.Max(0, now.Sub(metric.LastPlayed.UTC()).Hours()/24)
			score += 4 * math.Exp(-days/14)
		}
		withTracks, getErr := s.ds.Playlist(ctx).GetWithTracks(playlist.ID, false, false)
		if getErr == nil {
			var affinity float64
			for _, track := range withTracks.Tracks {
				affinity = math.Max(affinity, songScores[track.MediaFileID])
			}
			score += affinity * .35
		}
		if score > 0 {
			pls = append(pls, playlistCandidate{playlist: playlist, score: score})
		}
	}
	sort.SliceStable(pls, func(i, j int) bool { return pls[i].score > pls[j].score })

	playlistSlots := min(2, len(pls))
	if len(pls) > 2 && (len(songs) < 7 || pls[2].score >= songs[min(6, len(songs)-1)].score) {
		playlistSlots = 3
	}
	items := make([]model.QuickPickItem, 0, 9)
	for i := 0; i < playlistSlots && len(items) < 9; i++ {
		playlist := pls[i].playlist
		items = append(items, model.QuickPickItem{Kind: model.QuickPickPlaylist, Playlist: &playlist, Score: pls[i].score})
	}
	for i := 0; i < len(songs) && len(items) < 9; i++ {
		song := songs[i].song
		items = append(items, model.QuickPickItem{Kind: model.QuickPickSong, Song: &song, Score: songs[i].score})
	}
	return &model.QuickPickResponse{Items: items}, nil
}

func (s *service) RecordPlaylistPlay(ctx context.Context, userID, playlistID string) error {
	if _, err := s.ds.Playlist(ctx).Get(playlistID); err != nil {
		return err
	}
	return s.metrics.RecordPlaylistPlay(userID, playlistID, time.Now().UTC())
}
