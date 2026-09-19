package quickpick

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

type Service interface {
	Get(context.Context, string) (*model.QuickPickResponse, error)
	RecordPlaylistPlay(context.Context, string, string) error
	RecordImpressions(context.Context, string, string, []string) error
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

func (s *service) Get(ctx context.Context, userID string) (*model.QuickPickResponse, error) {
	now := time.Now().UTC()
	recent := map[string]int64{}
	if s.metrics != nil {
		var err error
		recent, err = s.metrics.SongRecentPlays(userID, now.AddDate(0, 0, -30))
		if err != nil {
			return nil, err
		}
	}

	songs, songScores, err := s.rankSongs(ctx, userID, now, recent)
	if err != nil {
		return nil, err
	}
	playlists, err := s.rankPlaylists(ctx, userID, now, songScores)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(songs)+len(playlists))
	for _, candidate := range songs {
		if key := trackExposureKey(candidate.song.ID); key != "" {
			keys = append(keys, key)
		}
	}
	for _, candidate := range playlists {
		if key := playlistExposureKey(candidate.playlist.ID); key != "" {
			keys = append(keys, key)
		}
	}
	exposures := s.readExposureMetrics(userID, keys)
	composed := composeQuickPick(songs, playlists, compositionOptions{Limit: quickPickLimit, Now: now, Exposures: exposures})
	viewID := id.NewRandom()
	for index := range composed.Items {
		item := &composed.Items[index]
		item.ViewID = viewID
		item.ItemKey = quickPickItemKey(*item)
	}
	return &model.QuickPickResponse{Items: composed.Items, ViewID: viewID}, nil
}

const quickPickLimit = 12

func quickPickItemKey(item model.QuickPickItem) string {
	if item.ItemKey != "" {
		return item.ItemKey
	}
	switch item.Kind {
	case model.QuickPickPlaylist:
		if item.Playlist != nil {
			return playlistExposureKey(item.Playlist.ID)
		}
	case model.QuickPickSong, model.QuickPickRecommendationKind:
		if item.Song != nil {
			return trackExposureKey(item.Song.ID)
		}
	}
	return ""
}

// recommendations surfaces similar tracks through the configured similarity
// agents. A recommendation is only returned when it also exists in the
// library, so it can act as a seed for a quick play mix without downloading
// anything up front.
func (s *service) recommendations(
	ctx context.Context,
	userID string,
	seedSongs []model.MediaFile,
	excludedMediaFileIDs map[string]struct{},
	now time.Time,
	recent map[string]int64,
) ([]model.QuickPickItem, error) {
	if s.agents == nil || s.matcher == nil || len(seedSongs) == 0 {
		return nil, nil
	}
	seenProviderCandidates := map[string]bool{}
	var providerCandidates []agents.Song
	for i := 0; i < len(seedSongs) && i < recommendationSeedCount; i++ {
		seed := seedSongs[i]
		similar, err := s.agents.GetSimilarSongsByTrackAll(ctx, seed.ID, seed.Title, seed.Artist, seed.MbzRecordingID, recommendationPerSeed)
		if err != nil {
			continue
		}
		for _, song := range similar {
			key := agents.CandidateID(song)
			if seenProviderCandidates[key] {
				continue
			}
			seenProviderCandidates[key] = true
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
	seenLocalMediaFiles := make(map[string]struct{}, len(providerCandidates))
	for i, candidate := range providerCandidates {
		local, ok := matches[i]
		if !ok || local.Missing || local.ID == "" {
			continue
		}
		if _, excluded := excludedMediaFileIDs[local.ID]; excluded {
			continue
		}
		if _, seen := seenLocalMediaFiles[local.ID]; seen {
			continue
		}
		seenLocalMediaFiles[local.ID] = struct{}{}
		key := quickPickCandidateKey(local.ID)
		rankedCandidates = append(rankedCandidates, recommendations.Candidate{
			Key:              key,
			MediaFile:        local,
			SimilarityScores: candidate.SimilarityScores,
		})
		if _, exists := sources[key]; !exists {
			sources[key] = candidate
		}
	}
	if len(rankedCandidates) == 0 {
		return nil, nil
	}

	s.applyTasteAffinities(userID, rankedCandidates)
	exposures := s.readExposureMetrics(userID, trackExposureKeysFromCandidates(rankedCandidates))
	ranked := recommendations.Rank(rankedCandidates, recommendations.Options{
		Now:         now,
		RecentPlays: recent,
		Fatigue:     fatigueForCandidates(rankedCandidates, exposures, now),
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

func (s *service) RecordImpressions(ctx context.Context, userID, viewID string, itemKeys []string) error {
	viewID = strings.TrimSpace(viewID)
	if viewID == "" {
		return errors.New("quick pick viewId is required")
	}
	if s.metrics == nil {
		return errors.New("quick pick metrics are not configured")
	}
	return s.metrics.RecordImpressions(userID, viewID, itemKeys, time.Now().UTC())
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
	if s.metrics == nil {
		return errors.New("quick pick metrics are not configured")
	}
	return s.metrics.RecordPlaylistPlay(userID, playlistID, time.Now().UTC())
}
