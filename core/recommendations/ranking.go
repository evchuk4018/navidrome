// Package recommendations contains the shared, stateless ranking primitives used
// by recommendation consumers such as Quick Pick and Personal Radio.
package recommendations

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/model"
)

const (
	countSaturation    = 10
	recencyHalfLife    = 14 * 24 * time.Hour
	transitionHalfLife = 90 * 24 * time.Hour
)

// TasteAffinity contains the four explainable dimensions used to compose a
// candidate's long-term taste signal. Every value, including Score, is
// bounded to [0,1]. The ranker does not calculate these values; a
// personalization repository attaches them in batches before ranking.
type TasteAffinity struct {
	Track  float64
	Artist float64
	Genre  float64
	Album  float64
	Score  float64
}

// TasteCandidateIdentity is the batch lookup identity for a candidate. A
// candidate may have several genre or artist keys when the source exposes
// multiple tags/participants.
type TasteCandidateIdentity struct {
	Key        string
	TrackKey   string
	ArtistKeys []string
	GenreKeys  []string
	AlbumKey   string
}

// TasteAffinityRepository is intentionally optional for consumers. This lets
// lightweight callers and tests use the shared ranker without a database while
// the SQL store can provide a persistent, restart-safe profile.
type TasteAffinityRepository interface {
	Rebuild(userID string, now time.Time) error
	EnsureFresh(userID string, now time.Time) error
	AffinityForCandidates(userID string, candidates []TasteCandidateIdentity) (map[string]TasteAffinity, error)
}

// ComposeTasteAffinity combines entity-level evidence into the bootstrap
// long-term taste signal. The weights are deliberately explicit and bounded;
// later learned-ranking work can tune their influence without changing the
// persisted evidence model.
func ComposeTasteAffinity(track, artist, genre, album float64) TasteAffinity {
	affinity := TasteAffinity{
		Track:  clamp(track, 0, 1),
		Artist: clamp(artist, 0, 1),
		Genre:  clamp(genre, 0, 1),
		Album:  clamp(album, 0, 1),
	}
	affinity.Score = clamp(
		0.45*affinity.Track+
			0.30*affinity.Artist+
			0.20*affinity.Genre+
			0.05*affinity.Album,
		0, 1,
	)
	return affinity
}

// Candidate is a track with optional similarity-provider metadata. Key can be
// used when the candidate is not backed by a library MediaFile, or when the
// caller needs an identity distinct from the MediaFile fields. SeedAffinity is
// a caller-provided [0,1] compatibility score for the candidate and its seed;
// SessionAffinity represents aggregate in-session compatibility.
type Candidate struct {
	Key                string
	SeedAffinity       float64
	SessionAffinity    float64
	TransitionAffinity float64
	TasteAffinity      float64
	TasteDetails       TasteAffinity
	model.MediaFile
	SimilarityScores []agents.SimilarityScore
}

// Weights controls the contribution of each ranking signal. Recency and
// Fatigue are penalties, so their contributions are negative.
type Weights struct {
	Similarity         float64
	SeedAffinity       float64
	SessionAffinity    float64
	PlayHistory        float64
	RecentListening    float64
	Starred            float64
	Recency            float64
	Fatigue            float64
	TransitionAffinity float64
	TasteAffinity      float64
}

// DefaultWeights returns the default signal weights used by Rank.
func DefaultWeights() Weights {
	return Weights{
		Similarity:         1,
		SeedAffinity:       1,
		SessionAffinity:    1,
		PlayHistory:        0.25,
		RecentListening:    0.75,
		Starred:            0.75,
		Recency:            0.5,
		Fatigue:            1,
		TransitionAffinity: 1.5,
		TasteAffinity:      0.8,
	}
}

// Options supplies the user and session signals used for ranking. RecentPlays
// and Fatigue are keyed by Candidate.Key when set, then MediaFile.ID; Path is
// used when an ID is unavailable.
// A zero Now deliberately omits time-dependent scoring, which keeps callers
// that do not have a reference clock deterministic.
type Options struct {
	Now         time.Time
	RecentPlays map[string]int64
	Fatigue     map[string]float64
	Weights     Weights
	// Limit caps the returned results. Zero or a negative value means no limit.
	Limit int
}

// ScoreBreakdown contains weighted contributions to a candidate's final score.
// Recency and Fatigue are negative when they apply. The fields sum to the
// RankedCandidate.Score value.
type ScoreBreakdown struct {
	Similarity         float64
	SeedAffinity       float64
	SessionAffinity    float64
	PlayHistory        float64
	RecentListening    float64
	Starred            float64
	Recency            float64
	Fatigue            float64
	TransitionAffinity float64
	// TasteTrackAffinity, TasteArtistAffinity, TasteGenreAffinity, and
	// TasteAlbumAffinity are raw explainability fields. TasteAffinity is the
	// weighted contribution included in the total score.
	TasteTrackAffinity  float64
	TasteArtistAffinity float64
	TasteGenreAffinity  float64
	TasteAlbumAffinity  float64
	TasteAffinity       float64
}

// RankedCandidate is a candidate and its total score plus inspectable score
// components.
type RankedCandidate struct {
	Candidate
	Score     float64
	Breakdown ScoreBreakdown
}

// Rank returns candidates in descending score order. Equal scores are ordered
// by stable candidate identity, so the result does not depend on input order.
// Ranking does not mutate candidates, their similarity metadata, or the maps in
// Options. Candidates with the same identity are returned once and their
// provider scores are merged.
func Rank(candidates []Candidate, options Options) []RankedCandidate {
	if len(candidates) == 0 {
		return nil
	}

	weights := options.Weights
	if weights == (Weights{}) {
		weights = DefaultWeights()
	}

	merged := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		key := candidateIdentity(candidate)
		if existing, ok := merged[key]; ok {
			merged[key] = mergeCandidates(existing, candidate)
			continue
		}
		merged[key] = cloneCandidate(candidate)
	}

	results := make([]RankedCandidate, 0, len(merged))
	for _, candidate := range merged {
		breakdown := scoreCandidate(candidate, options, weights)
		results = append(results, RankedCandidate{
			Candidate: candidate,
			Score:     breakdown.total(),
			Breakdown: breakdown,
		})
	}

	slices.SortFunc(results, func(left, right RankedCandidate) int {
		if left.Score != right.Score {
			if left.Score > right.Score {
				return -1
			}
			return 1
		}
		return cmp.Compare(candidateIdentity(left.Candidate), candidateIdentity(right.Candidate))
	})

	if options.Limit > 0 && options.Limit < len(results) {
		results = results[:options.Limit]
	}
	return results
}

func scoreCandidate(candidate Candidate, options Options, weights Weights) ScoreBreakdown {
	recentPlays := lookupInt64(options.RecentPlays, candidate.Key, candidate.MediaFile)
	fatigue := lookupFloat64(options.Fatigue, candidate.Key, candidate.MediaFile)

	return ScoreBreakdown{
		Similarity:          weights.Similarity * providerSimilarity(candidate.SimilarityScores),
		SeedAffinity:        weights.SeedAffinity * clamp(candidate.SeedAffinity, 0, 1),
		SessionAffinity:     weights.SessionAffinity * clamp(candidate.SessionAffinity, 0, 1),
		PlayHistory:         weights.PlayHistory * normalizedCount(candidate.PlayCount),
		RecentListening:     weights.RecentListening * normalizedCount(recentPlays),
		Starred:             weights.Starred * boolScore(candidate.Starred),
		Recency:             weights.Recency * recencyPenalty(candidate.PlayDate, options.Now),
		Fatigue:             weights.Fatigue * -clamp(fatigue, 0, 1),
		TransitionAffinity:  weights.TransitionAffinity * clamp(candidate.TransitionAffinity, -1, 1),
		TasteTrackAffinity:  clamp(candidate.TasteDetails.Track, 0, 1),
		TasteArtistAffinity: clamp(candidate.TasteDetails.Artist, 0, 1),
		TasteGenreAffinity:  clamp(candidate.TasteDetails.Genre, 0, 1),
		TasteAlbumAffinity:  clamp(candidate.TasteDetails.Album, 0, 1),
		TasteAffinity:       weights.TasteAffinity * effectiveTasteAffinity(candidate),
	}
}

func (s ScoreBreakdown) total() float64 {
	return s.Similarity + s.SeedAffinity + s.SessionAffinity + s.PlayHistory + s.RecentListening + s.Starred +
		s.Recency + s.Fatigue + s.TransitionAffinity + s.TasteAffinity
}

func effectiveTasteAffinity(candidate Candidate) float64 {
	if candidate.TasteAffinity != 0 {
		return clamp(candidate.TasteAffinity, 0, 1)
	}
	return clamp(candidate.TasteDetails.Score, 0, 1)
}

// TransitionAffinity converts contextual playback history into a bounded
// signed signal. Positive outcomes raise the signal, early skips lower it,
// confidence prevents one observation from dominating, and old evidence fades
// without changing the persisted aggregate.
func TransitionAffinity(feedback model.RadioTransitionFeedback, now time.Time) float64 {
	positive := float64(feedback.AcceptedCount) +
		1.5*float64(feedback.CompletedCount) + 2*float64(feedback.KeepCount)
	negative := 1.5*float64(feedback.EarlySkipCount) + 0.25*float64(feedback.NeutralSkipCount)
	observations := positive + negative
	rawPreference := (positive + 2) / (observations + 4)
	confidence := 1 - math.Exp(-float64(maxInt(feedback.AttemptCount, 0))/4)
	affinity := (2*rawPreference - 1) * confidence

	if !now.IsZero() {
		lastEvidence := latestTransitionEvidence(feedback)
		if !lastEvidence.IsZero() {
			age := now.Sub(lastEvidence.UTC())
			if age < 0 {
				age = 0
			}
			affinity *= math.Exp(-float64(age) / float64(transitionHalfLife))
		}
	}
	return clamp(affinity, -1, 1)
}

func latestTransitionEvidence(feedback model.RadioTransitionFeedback) time.Time {
	latest := time.Time{}
	for _, candidate := range []*time.Time{feedback.LastAttemptAt, feedback.LastPositiveAt, feedback.LastNegativeAt} {
		if candidate != nil && candidate.After(latest) {
			latest = *candidate
		}
	}
	return latest
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func providerSimilarity(scores []agents.SimilarityScore) float64 {
	var best float64
	for _, score := range scores {
		normalized := clamp(score.NormalizedScore, 0, 1)
		if normalized > best {
			best = normalized
		}
	}
	return best
}

func normalizedCount(count int64) float64 {
	if count <= 0 {
		return 0
	}
	return min(1, math.Log1p(float64(count))/math.Log1p(countSaturation))
}

func recencyPenalty(playDate *time.Time, now time.Time) float64 {
	if playDate == nil || now.IsZero() {
		return 0
	}
	age := now.Sub(playDate.UTC())
	if age < 0 {
		age = 0
	}
	return -math.Exp(-float64(age) / float64(recencyHalfLife))
}
func lookupInt64(values map[string]int64, key string, mediaFile model.MediaFile) int64 {
	if key != "" {
		if value, ok := values[key]; ok {
			return value
		}
	}
	if value, ok := values[mediaFile.ID]; ok {
		return value
	}
	if mediaFile.ID == "" {
		return values[mediaFile.Path]
	}
	return 0
}
func lookupFloat64(values map[string]float64, key string, mediaFile model.MediaFile) float64 {
	if key != "" {
		if value, ok := values[key]; ok {
			return value
		}
	}
	if value, ok := values[mediaFile.ID]; ok {
		return value
	}
	if mediaFile.ID == "" {
		return values[mediaFile.Path]
	}
	return 0
}

func mergeCandidates(left, right Candidate) Candidate {
	merged := left
	if merged.Key == "" {
		merged.Key = right.Key
	}
	if candidateSortKey(right.MediaFile) < candidateSortKey(left.MediaFile) {
		merged.MediaFile = right.MediaFile
	}
	if math.Abs(right.TransitionAffinity) > math.Abs(left.TransitionAffinity) {
		merged.TransitionAffinity = right.TransitionAffinity
	}
	merged.SessionAffinity = combineAffinity(left.SessionAffinity, right.SessionAffinity)
	if right.SeedAffinity > left.SeedAffinity {
		merged.SeedAffinity = right.SeedAffinity
	}
	if right.TasteAffinity > left.TasteAffinity || right.TasteDetails.Score > left.TasteDetails.Score {
		merged.TasteAffinity = right.TasteAffinity
		merged.TasteDetails = right.TasteDetails
	}
	merged.SimilarityScores = mergeSimilarityScores(left.SimilarityScores, right.SimilarityScores)
	return merged
}

func combineAffinity(left, right float64) float64 {
	left = clamp(left, 0, 1)
	right = clamp(right, 0, 1)
	return 1 - (1-left)*(1-right)
}

func mergeSimilarityScores(left, right []agents.SimilarityScore) []agents.SimilarityScore {
	merged := make(map[string]agents.SimilarityScore, len(left)+len(right))
	for _, score := range append(append([]agents.SimilarityScore(nil), left...), right...) {
		provider := strings.ToLower(strings.TrimSpace(score.Provider))
		existing, ok := merged[provider]
		if !ok || betterSimilarity(score, existing) {
			merged[provider] = score
		}
	}

	result := make([]agents.SimilarityScore, 0, len(merged))
	for _, score := range merged {
		result = append(result, score)
	}
	slices.SortFunc(result, func(left, right agents.SimilarityScore) int {
		return cmp.Compare(strings.ToLower(strings.TrimSpace(left.Provider)), strings.ToLower(strings.TrimSpace(right.Provider)))
	})
	return result
}

func betterSimilarity(left, right agents.SimilarityScore) bool {
	leftNormalized := clamp(left.NormalizedScore, 0, 1)
	rightNormalized := clamp(right.NormalizedScore, 0, 1)
	if leftNormalized != rightNormalized {
		return leftNormalized > rightNormalized
	}
	return finiteValue(left.Score) > finiteValue(right.Score)
}

func cloneCandidate(candidate Candidate) Candidate {
	clone := candidate
	clone.SimilarityScores = append([]agents.SimilarityScore(nil), candidate.SimilarityScores...)
	return clone
}

func candidateIdentity(candidate Candidate) string {
	if key := strings.TrimSpace(candidate.Key); key != "" {
		return key
	}
	return mediaFileIdentity(candidate.MediaFile)
}

func mediaFileIdentity(mediaFile model.MediaFile) string {
	if id := strings.TrimSpace(mediaFile.ID); id != "" {
		return "id:" + id
	}
	if path := strings.TrimSpace(mediaFile.Path); path != "" {
		return "path:" + path
	}
	return "title:" + normalize(mediaFile.Title) + "|artist:" + normalize(mediaFile.Artist) + "|album:" + normalize(mediaFile.Album)
}

// TasteIdentityForMediaFile creates the stable entity keys shared by all
// recommendation surfaces. Track identity follows the radio MBID-first rule;
// the textual fallback is only used for a candidate that has neither a local
// media-file ID nor a recording MBID.
func TasteIdentityForMediaFile(key string, mediaFile model.MediaFile) TasteCandidateIdentity {
	key = strings.TrimSpace(key)
	trackKey := model.RadioTrackKey(mediaFile.MbzRecordingID, mediaFile.ID)
	if trackKey == "" {
		trackKey = "track:title:" + normalize(mediaFile.Title) + "|artist:" + normalize(mediaFile.Artist)
	}
	if key == "" {
		key = trackKey
	}

	artistKeys := make([]string, 0, 2)
	if artistID := strings.TrimSpace(mediaFile.ArtistID); artistID != "" {
		artistKeys = append(artistKeys, "artist:id:"+normalize(artistID))
	}
	if artist := normalize(mediaFile.Artist); artist != "" {
		artistKeys = append(artistKeys, "artist:name:"+artist)
	}

	genreKeys := make([]string, 0, 1+len(mediaFile.Genres))
	if genre := normalize(mediaFile.Genre); genre != "" {
		genreKeys = append(genreKeys, "genre:"+genre)
	}
	for _, genre := range mediaFile.Genres {
		if value := normalize(genre.Name); value != "" {
			genreKeys = appendUnique(genreKeys, "genre:"+value)
		}
	}

	albumKey := ""
	if albumID := strings.TrimSpace(mediaFile.AlbumID); albumID != "" {
		albumKey = "album:id:" + normalize(albumID)
	} else if album := normalize(mediaFile.Album); album != "" {
		albumKey = "album:name:" + album + "|artist:" + normalize(mediaFile.Artist)
	}

	return TasteCandidateIdentity{
		Key:        key,
		TrackKey:   trackKey,
		ArtistKeys: artistKeys,
		GenreKeys:  genreKeys,
		AlbumKey:   albumKey,
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func candidateSortKey(mediaFile model.MediaFile) string {
	return mediaFileIdentity(mediaFile) + "|title:" + normalize(mediaFile.Title) + "|artist:" + normalize(mediaFile.Artist) + "|album:" + normalize(mediaFile.Album) + "|path:" + strings.TrimSpace(mediaFile.Path)
}

func normalize(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func boolScore(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func clamp(value, lower, upper float64) float64 {
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

func finiteValue(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}
