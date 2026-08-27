package quickpick

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

type Service interface {
	Get(context.Context, string) (*model.QuickPickResponse, error)
	RecordPlaylistPlay(context.Context, string, string) error
}

type SimilarityProvider interface {
	GetSimilarSongsByTrackAll(context.Context, string, string, string, string, int) ([]agents.Song, error)
}

type service struct {
	ds      model.DataStore
	metrics model.QuickPickMetricsRepository
	agents  SimilarityProvider
	matcher *matcher.Matcher
}

func New(ds model.DataStore, metrics model.QuickPickMetricsRepository, ag *agents.Agents, m *matcher.Matcher) Service {
	return &service{ds: ds, metrics: metrics, agents: ag, matcher: m}
}

const (
	recommendationSeedCount = 5
	recommendationPerSeed   = 5
	recommendationLimit     = 4
)

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
	rankingInputs := make([]recommendations.Candidate, 0, len(byID))
	for _, song := range byID {
		rankingInputs = append(rankingInputs, recommendations.Candidate{
			Key:       "quickpick:" + song.ID,
			MediaFile: song,
		})
	}
	for _, rankedSong := range recommendations.Rank(rankingInputs, recommendations.Options{
		Now:         now,
		RecentPlays: recent,
	}) {
		songs = append(songs, songCandidate{song: rankedSong.MediaFile, score: rankedSong.Score})
		songScores[rankedSong.ID] = rankedSong.Score
	}

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
	items := make([]model.QuickPickItem, 0, 13)
	for i := 0; i < playlistSlots && len(items) < 9; i++ {
		playlist := pls[i].playlist
		items = append(items, model.QuickPickItem{Kind: model.QuickPickPlaylist, Playlist: &playlist, Score: pls[i].score})
	}
	for i := 0; i < len(songs) && len(items) < 9; i++ {
		song := songs[i].song
		items = append(items, model.QuickPickItem{Kind: model.QuickPickSong, Song: &song, Score: songs[i].score})
	}

	recommendations, err := s.recommendations(ctx, songs, now, recent)
	if err != nil {
		return nil, err
	}
	items = append(items, recommendations...)
	return &model.QuickPickResponse{Items: items}, nil
}

// recommendations surfaces similar tracks (via the configured similarity
// agents, e.g. Last.fm) for the user's top songs. A recommendation is only
// returned when it also exists in the library, so it can act as a seed for a
// quick play mix without downloading anything up front.
func (s *service) recommendations(ctx context.Context, songs []songCandidate, now time.Time, recent map[string]int64) ([]model.QuickPickItem, error) {
	if s.agents == nil || s.matcher == nil || len(songs) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var providerCandidates []agents.Song
	for i := 0; i < len(songs) && i < recommendationSeedCount; i++ {
		seed := songs[i].song
		similar, err := s.agents.GetSimilarSongsByTrackAll(ctx, seed.ID, seed.Title, seed.Artist, seed.MbzRecordingID, recommendationPerSeed)
		if err != nil {
			continue
		}
		for _, song := range similar {
			key := agents.CandidateID(song)
			if seen[key] {
				continue
			}
			seen[key] = true
			providerCandidates = append(providerCandidates, song)
		}
	}
	if len(providerCandidates) == 0 {
		return nil, nil
	}
	matches, err := s.matcher.MatchSongsIndexed(ctx, providerCandidates)
	if err != nil {
		return nil, nil
	}
	rankedCandidates := make([]recommendations.Candidate, 0, len(providerCandidates))
	sources := make(map[string]agents.Song, len(providerCandidates))
	for i, candidate := range providerCandidates {
		local, ok := matches[i]
		if !ok || local.Missing {
			continue
		}
		key := "quickpick:" + local.ID
		rankedCandidates = append(rankedCandidates, recommendations.Candidate{
			Key:              key,
			MediaFile:        local,
			SimilarityScores: candidate.SimilarityScores,
		})
		if _, exists := sources[key]; !exists {
			sources[key] = candidate
		}
	}
	ranked := recommendations.Rank(rankedCandidates, recommendations.Options{
		Now:         now,
		RecentPlays: recent,
		Limit:       recommendationLimit,
	})
	items := make([]model.QuickPickItem, 0, len(ranked))
	for _, rankedCandidate := range ranked {
		candidate, ok := sources[rankedCandidate.Key]
		if !ok {
			continue
		}
		local := rankedCandidate.MediaFile
		items = append(items, model.QuickPickItem{
			Kind: model.QuickPickRecommendationKind,
			Song: &local,
			Recommendation: &model.QuickPickRecommendation{
				Title:         candidate.Name,
				Artist:        firstSongArtist(candidate),
				Album:         candidate.Album,
				RecordingMBID: candidate.MBID,
			},
			Score: rankedCandidate.Score,
		})
	}
	return items, nil
}

func firstSongArtist(song agents.Song) string {
	if len(song.Artists) == 0 {
		return ""
	}
	return song.Artists[0].Name
}

func (s *service) RecordPlaylistPlay(ctx context.Context, userID, playlistID string) error {
	if _, err := s.ds.Playlist(ctx).Get(playlistID); err != nil {
		return err
	}
	return s.metrics.RecordPlaylistPlay(userID, playlistID, time.Now().UTC())
}
