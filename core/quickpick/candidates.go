package quickpick

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

const (
	playCountPoolSize = 200
	recentPoolSize    = 200
	likedPoolSize     = 250
	fallbackPoolMin   = 40
	fallbackRandomMax = 75
)

const (
	quickPickTrackExposurePrefix    = "track:"
	quickPickPlaylistExposurePrefix = "playlist:"
)

type recalledSong struct {
	file       model.MediaFile
	fromPlays  bool
	fromRecent bool
	fromLiked  bool
}

type songCandidate struct {
	song          model.MediaFile
	baseScore     float64
	adjustedScore float64
	baseRank      int
	fromLiked     bool
}

type playlistCandidate struct {
	playlist         model.Playlist
	baseScore        float64
	normalizedScore  float64
	adjustedScore    float64
	hasAdjustedScore bool
}

func (s *service) recallSongs(ctx context.Context) ([]recalledSong, error) {
	type recallQuery struct {
		options model.QueryOptions
		source  string
	}
	queries := []recallQuery{
		{options: model.QueryOptions{Sort: "play_count", Order: "desc", Max: playCountPoolSize}, source: "plays"},
		{options: model.QueryOptions{Sort: "play_date", Order: "desc", Max: recentPoolSize}, source: "recent"},
		{options: model.QueryOptions{Sort: "starred_at", Order: "desc", Max: likedPoolSize}, source: "liked"},
	}

	byID := make(map[string]recalledSong)
	for _, query := range queries {
		files, err := s.ds.MediaFile(ctx).GetAll(query.options)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.ID == "" {
				continue
			}
			candidate, exists := byID[file.ID]
			if !exists {
				candidate.file = file
			} else {
				candidate.file.Starred = candidate.file.Starred || file.Starred
			}
			switch query.source {
			case "plays":
				candidate.fromPlays = true
			case "recent":
				candidate.fromRecent = true
			case "liked":
				// The starred_at query can include a trailing row that is not
				// actually starred. Preserve only the explicit annotation signal.
				candidate.fromLiked = candidate.fromLiked || file.Starred
			}
			byID[file.ID] = candidate
		}
	}

	if len(byID) < fallbackPoolMin {
		files, err := s.ds.MediaFile(ctx).GetRandom(model.QueryOptions{Max: fallbackRandomMax})
		if err != nil && len(byID) == 0 {
			return nil, err
		}
		for _, file := range files {
			if file.ID == "" {
				continue
			}
			candidate, exists := byID[file.ID]
			if !exists {
				candidate.file = file
			} else {
				candidate.file.Starred = candidate.file.Starred || file.Starred
			}
			candidate.fromLiked = candidate.fromLiked || file.Starred
			byID[file.ID] = candidate
		}
	}

	result := make([]recalledSong, 0, len(byID))
	for _, candidate := range byID {
		result = append(result, candidate)
	}
	return result, nil
}

func (s *service) rankSongs(ctx context.Context, userID string, now time.Time, recent map[string]int64) ([]songCandidate, map[string]float64, error) {
	recalled, err := s.recallSongs(ctx)
	if err != nil {
		return nil, nil, err
	}

	rankingInputs := make([]recommendations.Candidate, 0, len(recalled))
	likedByID := make(map[string]bool, len(recalled))
	for _, recalledSong := range recalled {
		key := quickPickCandidateKey(recalledSong.file.ID)
		rankingInputs = append(rankingInputs, recommendations.Candidate{
			Key:       key,
			MediaFile: recalledSong.file,
		})
		likedByID[recalledSong.file.ID] = recalledSong.fromLiked
	}
	s.applyTasteAffinities(userID, rankingInputs)

	exposures := s.readExposureMetrics(userID, trackExposureKeysFromCandidates(rankingInputs))
	fatigue := fatigueForCandidates(rankingInputs, exposures, now)
	baseRanked := recommendations.Rank(rankingInputs, recommendations.Options{
		Now:         now,
		RecentPlays: recent,
	})
	adjustedRanked := recommendations.Rank(rankingInputs, recommendations.Options{
		Now:         now,
		RecentPlays: recent,
		Fatigue:     fatigue,
	})
	adjustedByKey := make(map[string]recommendations.RankedCandidate, len(adjustedRanked))
	for _, ranked := range adjustedRanked {
		adjustedByKey[ranked.Key] = ranked
	}

	songs := make([]songCandidate, 0, len(baseRanked))
	songScores := make(map[string]float64, len(baseRanked))
	for index, ranked := range baseRanked {
		adjusted := ranked
		if value, ok := adjustedByKey[ranked.Key]; ok {
			adjusted = value
		}
		candidate := songCandidate{
			song:          ranked.MediaFile,
			baseScore:     ranked.Score,
			adjustedScore: adjusted.Score,
			baseRank:      index + 1,
			fromLiked:     likedByID[ranked.ID],
		}
		songs = append(songs, candidate)
		songScores[ranked.ID] = ranked.Score
	}
	return songs, songScores, nil
}

func (s *service) rankPlaylists(ctx context.Context, userID string, now time.Time, songScores map[string]float64) ([]playlistCandidate, error) {
	playlistMetrics, err := s.metrics.PlaylistMetrics(userID, now.AddDate(0, 0, -30))
	if err != nil {
		return nil, err
	}
	playlists, err := s.ds.Playlist(ctx).GetAll(model.QueryOptions{Sort: "updated_at", Order: "desc", Max: 100})
	if err != nil {
		return nil, err
	}

	result := make([]playlistCandidate, 0, len(playlists))
	for _, playlist := range playlists {
		metric := playlistMetrics[playlist.ID]
		score := 3*math.Log1p(float64(metric.TotalStarts)) + 5*math.Log1p(float64(metric.RecentStarts))
		if metric.LastPlayed != nil {
			days := math.Max(0, now.Sub(metric.LastPlayed.UTC()).Hours()/24)
			score += 4 * math.Exp(-days/14)
		}
		if playlist.Starred {
			score += 1
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
			result = append(result, playlistCandidate{playlist: playlist, baseScore: score})
		}
	}

	if len(result) == 0 {
		return nil, nil
	}
	minScore, maxScore := result[0].baseScore, result[0].baseScore
	for _, candidate := range result[1:] {
		minScore = math.Min(minScore, candidate.baseScore)
		maxScore = math.Max(maxScore, candidate.baseScore)
	}
	for index := range result {
		if maxScore == minScore {
			result[index].normalizedScore = 1
		} else {
			result[index].normalizedScore = (result[index].baseScore - minScore) / (maxScore - minScore)
		}
	}
	exposureKeys := make([]string, 0, len(result))
	for _, candidate := range result {
		exposureKeys = append(exposureKeys, playlistExposureKey(candidate.playlist.ID))
	}
	exposures := s.readExposureMetrics(userID, exposureKeys)
	for index := range result {
		fatigue := exposureFatigue(exposures[playlistExposureKey(result[index].playlist.ID)], now)
		result[index].adjustedScore = result[index].normalizedScore - .70*fatigue
		result[index].hasAdjustedScore = true
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].adjustedScore != result[right].adjustedScore {
			return result[left].adjustedScore > result[right].adjustedScore
		}
		if result[left].normalizedScore != result[right].normalizedScore {
			return result[left].normalizedScore > result[right].normalizedScore
		}
		return result[left].playlist.ID < result[right].playlist.ID
	})
	return result, nil
}

func (s *service) applyTasteAffinities(userID string, candidates []recommendations.Candidate) {
	if len(candidates) == 0 || s.metrics == nil {
		return
	}
	taste, ok := s.metrics.(recommendations.TasteAffinityRepository)
	if !ok {
		return
	}
	identities := make([]recommendations.TasteCandidateIdentity, 0, len(candidates))
	for _, candidate := range candidates {
		identities = append(identities, recommendations.TasteIdentityForMediaFile(candidate.Key, candidate.MediaFile))
	}
	affinity, err := taste.AffinityForCandidates(userID, identities)
	if err != nil {
		return
	}
	for index := range candidates {
		value := affinity[candidates[index].Key]
		candidates[index].TasteAffinity = value.Score
		candidates[index].TasteDetails = value
	}
}

func (s *service) readExposureMetrics(userID string, keys []string) map[string]model.QuickPickExposureMetric {
	result := map[string]model.QuickPickExposureMetric{}
	if s.metrics == nil || len(keys) == 0 {
		return result
	}
	metrics, err := s.metrics.ExposureMetrics(userID, keys)
	if err != nil {
		return result
	}
	for key, metric := range metrics {
		result[key] = metric
	}
	return result
}

func exposureFatigue(metric model.QuickPickExposureMetric, now time.Time) float64 {
	if metric.LastShownAt.IsZero() || now.IsZero() {
		return 0
	}
	age := now.Sub(metric.LastShownAt.UTC())
	if age < 0 {
		age = 0
	}
	recency := math.Exp(-float64(age) / float64(36*time.Hour))
	countFactor := math.Min(1, .70+.10*math.Log1p(float64(metric.ShowCount)))
	return clampQuickPick(recency*countFactor, 0, 1)
}

func fatigueForCandidates(candidates []recommendations.Candidate, exposures map[string]model.QuickPickExposureMetric, now time.Time) map[string]float64 {
	fatigue := make(map[string]float64, len(candidates))
	for _, candidate := range candidates {
		value := exposureFatigue(exposures[trackExposureKey(candidate.ID)], now)
		if value == 0 {
			continue
		}
		fatigue[candidate.Key] = value
	}
	return fatigue
}

func trackExposureKeysFromCandidates(candidates []recommendations.Candidate) []string {
	keys := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if key := trackExposureKey(candidate.ID); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func trackExposureKey(mediaFileID string) string {
	mediaFileID = strings.TrimSpace(mediaFileID)
	if mediaFileID == "" {
		return ""
	}
	return quickPickTrackExposurePrefix + mediaFileID
}

func playlistExposureKey(playlistID string) string {
	playlistID = strings.TrimSpace(playlistID)
	if playlistID == "" {
		return ""
	}
	return quickPickPlaylistExposurePrefix + playlistID
}

func quickPickCandidateKey(mediaFileID string) string {
	return "quickpick:" + mediaFileID
}

func clampQuickPick(value, lower, upper float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value < lower {
		return lower
	}
	if value > upper {
		return upper
	}
	return value
}
