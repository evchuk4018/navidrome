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
		Similarity:         weights.Similarity * providerSimilarity(candidate.SimilarityScores),
		SeedAffinity:       weights.SeedAffinity * clamp(candidate.SeedAffinity, 0, 1),
		SessionAffinity:    weights.SessionAffinity * clamp(candidate.SessionAffinity, 0, 1),
		PlayHistory:        weights.PlayHistory * normalizedCount(candidate.PlayCount),
		RecentListening:    weights.RecentListening * normalizedCount(recentPlays),
		Starred:            weights.Starred * boolScore(candidate.Starred),
		Recency:            weights.Recency * recencyPenalty(candidate.PlayDate, options.Now),
		Fatigue:            weights.Fatigue * -clamp(fatigue, 0, 1),
		TransitionAffinity: weights.TransitionAffinity * clamp(candidate.TransitionAffinity, -1, 1),
	}
}

func (s ScoreBreakdown) total() float64 {
	return s.Similarity + s.SeedAffinity + s.SessionAffinity + s.PlayHistory + s.RecentListening + s.Starred +
		s.Recency + s.Fatigue + s.TransitionAffinity
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
