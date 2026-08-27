package recommendations

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/model"
)

func TestRankUsesAllSignalsAndExposesBreakdown(t *testing.T) {
	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	candidate := Candidate{
		SeedAffinity: 0.5,
		MediaFile: model.MediaFile{
			ID: "song-1",
			Annotations: model.Annotations{
				PlayCount: 9,
				PlayDate:  timePtr(now.Add(-24 * time.Hour)),
				Starred:   true,
			},
		},
		SimilarityScores: []agents.SimilarityScore{{
			Provider:        "lastfm",
			Score:           0.8,
			NormalizedScore: 0.8,
		}},
	}

	weights := Weights{
		Similarity:      2,
		SeedAffinity:    3,
		PlayHistory:     3,
		RecentListening: 4,
		Starred:         5,
		Recency:         6,
		Fatigue:         7,
	}
	results := Rank([]Candidate{candidate}, Options{
		Now:         now,
		RecentPlays: map[string]int64{"song-1": 4},
		Fatigue:     map[string]float64{"song-1": 0.25},
		Weights:     weights,
	})

	if len(results) != 1 {
		t.Fatalf("Rank() returned %d candidates, want 1", len(results))
	}
	result := results[0]
	if result.ID != "song-1" {
		t.Fatalf("ranked candidate ID = %q, want song-1", result.ID)
	}
	if result.Breakdown.Similarity <= 0 {
		t.Error("similarity contribution should be positive")
	}
	if result.Breakdown.SeedAffinity <= 0 {
		t.Error("seed affinity contribution should be positive")
	}
	if result.Breakdown.SessionAffinity != 0 {
		t.Error("session affinity should be zero when it is not provided")
	}
	if result.Breakdown.PlayHistory <= 0 {
		t.Error("play history contribution should be positive")
	}
	if result.Breakdown.RecentListening <= 0 {
		t.Error("recent listening contribution should be positive")
	}
	if result.Breakdown.Starred <= 0 {
		t.Error("starred contribution should be positive")
	}
	if result.Breakdown.Recency >= 0 {
		t.Error("recency contribution should be a penalty for recently played tracks")
	}
	if result.Breakdown.Fatigue >= 0 {
		t.Error("fatigue contribution should be a penalty")
	}

	breakdownTotal := result.Breakdown.Similarity +
		result.Breakdown.SeedAffinity +
		result.Breakdown.SessionAffinity +
		result.Breakdown.PlayHistory +
		result.Breakdown.RecentListening +
		result.Breakdown.Starred +
		result.Breakdown.Recency +
		result.Breakdown.Fatigue
	if !almostEqual(result.Score, breakdownTotal) {
		t.Errorf("score = %v, sum of breakdown = %v", result.Score, breakdownTotal)
	}
}

func TestRankUsesCandidateKeyForExternalSignals(t *testing.T) {
	results := Rank([]Candidate{{
		Key: "discovery:mbid:one",
	}}, Options{
		RecentPlays: map[string]int64{"discovery:mbid:one": 3},
		Fatigue:     map[string]float64{"discovery:mbid:one": 0.5},
		Weights:     Weights{RecentListening: 1, Fatigue: 1},
	})
	if len(results) != 1 {
		t.Fatalf("Rank() returned %d candidates, want 1", len(results))
	}
	if results[0].Breakdown.RecentListening <= 0 {
		t.Error("recent listening should use the candidate key when no media ID exists")
	}
	if results[0].Breakdown.Fatigue >= 0 {
		t.Error("fatigue should use the candidate key when no media ID exists")
	}
}

func TestRankUsesSeedAffinity(t *testing.T) {
	results := Rank([]Candidate{{Key: "strong", SeedAffinity: 0.9}, {Key: "weak", SeedAffinity: 0.1}}, Options{})
	if got := results[0].Key; got != "strong" {
		t.Fatalf("first candidate key = %q, want strong", got)
	}
	if results[0].Breakdown.SeedAffinity <= results[1].Breakdown.SeedAffinity {
		t.Fatalf("seed affinity contributions = %v, %v; want strong > weak", results[0].Breakdown.SeedAffinity, results[1].Breakdown.SeedAffinity)
	}
}

func TestTransitionAffinityIsSmoothedAndBounded(t *testing.T) {
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	if got := TransitionAffinity(model.RadioTransitionFeedback{}, now); got != 0 {
		t.Fatalf("zero transition affinity = %v, want 0", got)
	}
	one := TransitionAffinity(model.RadioTransitionFeedback{
		AttemptCount: 1, AcceptedCount: 1, LastPositiveAt: timePtr(now),
	}, now)
	repeated := TransitionAffinity(model.RadioTransitionFeedback{
		AttemptCount: 8, AcceptedCount: 8, CompletedCount: 4, LastPositiveAt: timePtr(now),
	}, now)
	negative := TransitionAffinity(model.RadioTransitionFeedback{
		AttemptCount: 8, EarlySkipCount: 8, LastNegativeAt: timePtr(now),
	}, now)
	if !(one > 0 && repeated > one && negative < 0) {
		t.Fatalf("transition affinities = one:%v repeated:%v negative:%v", one, repeated, negative)
	}
	if repeated > 1 || negative < -1 {
		t.Fatalf("transition affinity escaped bounds: repeated:%v negative:%v", repeated, negative)
	}
	mixed := TransitionAffinity(model.RadioTransitionFeedback{
		AttemptCount: 8, AcceptedCount: 3, EarlySkipCount: 2, LastPositiveAt: timePtr(now), LastNegativeAt: timePtr(now),
	}, now)
	if math.Abs(mixed) >= math.Abs(one) {
		t.Fatalf("mixed affinity = %v, want closer to neutral than one positive = %v", mixed, one)
	}
}

func TestTransitionAffinityDecaysWithAge(t *testing.T) {
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	feedback := model.RadioTransitionFeedback{
		AttemptCount: 8, CompletedCount: 8, LastPositiveAt: timePtr(now.Add(-180 * 24 * time.Hour)),
	}
	fresh := TransitionAffinity(feedback, now.Add(-180*24*time.Hour))
	old := TransitionAffinity(feedback, now)
	if !(fresh > old && old > 0) {
		t.Fatalf("fresh affinity = %v, old affinity = %v; want fresh > old > 0", fresh, old)
	}
}

func TestRankIncludesTransitionContributionInBreakdown(t *testing.T) {
	results := Rank([]Candidate{{Key: "learned", TransitionAffinity: 0.5}}, Options{
		Weights: Weights{TransitionAffinity: 2},
	})
	if len(results) != 1 {
		t.Fatalf("Rank() returned %d candidates, want 1", len(results))
	}
	if !almostEqual(results[0].Breakdown.TransitionAffinity, 1) || !almostEqual(results[0].Score, 1) {
		t.Fatalf("transition breakdown/score = %v/%v, want 1/1", results[0].Breakdown.TransitionAffinity, results[0].Score)
	}
}

func TestRankUsesDefaultWeightsAndSortsDeterministically(t *testing.T) {
	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	candidates := []Candidate{
		{MediaFile: model.MediaFile{ID: "b", Title: "Same"}},
		{MediaFile: model.MediaFile{ID: "a", Title: "Same"}},
		{MediaFile: model.MediaFile{ID: "c", Title: "Better"}, SimilarityScores: []agents.SimilarityScore{{NormalizedScore: 1}}},
	}

	results := Rank(candidates, Options{Now: now})
	if got := rankedIDs(results); !reflect.DeepEqual(got, []string{"c", "a", "b"}) {
		t.Fatalf("ranked IDs = %v, want [c a b]", got)
	}

	reversed := append([]Candidate(nil), candidates...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	if got := rankedIDs(Rank(reversed, Options{Now: now})); !reflect.DeepEqual(got, []string{"c", "a", "b"}) {
		t.Fatalf("ranked IDs after input reorder = %v, want [c a b]", got)
	}
}

func TestRankDeduplicatesCandidatesAndMergesProviderScores(t *testing.T) {
	candidates := []Candidate{
		{
			MediaFile: model.MediaFile{ID: "song-1", Title: "Song"},
			SimilarityScores: []agents.SimilarityScore{{
				Provider:        "lastfm",
				NormalizedScore: 0.4,
			}},
		},
		{
			MediaFile: model.MediaFile{ID: "song-1", Title: "Song"},
			SimilarityScores: []agents.SimilarityScore{{
				Provider:        "listenbrainz",
				NormalizedScore: 0.9,
			}},
		},
		{
			MediaFile: model.MediaFile{ID: "song-2", Title: "Other"},
		},
	}

	results := Rank(candidates, Options{Weights: Weights{Similarity: 1}})
	if len(results) != 2 {
		t.Fatalf("Rank() returned %d candidates, want 2 after deduplication", len(results))
	}
	if got := results[0].ID; got != "song-1" {
		t.Fatalf("first candidate ID = %q, want song-1", got)
	}
	if got := len(results[0].SimilarityScores); got != 2 {
		t.Fatalf("merged similarity score count = %d, want 2", got)
	}
	if !almostEqual(results[0].Breakdown.Similarity, 0.9) {
		t.Errorf("merged similarity = %v, want 0.9", results[0].Breakdown.Similarity)
	}
}

func TestRankDoesNotMutateInputs(t *testing.T) {
	candidates := []Candidate{{
		MediaFile: model.MediaFile{ID: "song-1"},
		SimilarityScores: []agents.SimilarityScore{{
			Provider:        "provider",
			NormalizedScore: 0.5,
		}},
	}, {
		MediaFile: model.MediaFile{ID: "song-2"},
	}}
	originalCandidates := append([]Candidate(nil), candidates...)
	originalScores := append([]agents.SimilarityScore(nil), candidates[0].SimilarityScores...)
	recent := map[string]int64{"song-1": 2}
	fatigue := map[string]float64{"song-1": 0.4}

	_ = Rank(candidates, Options{RecentPlays: recent, Fatigue: fatigue})

	if !reflect.DeepEqual(candidates, originalCandidates) {
		t.Fatal("Rank() mutated candidate order or values")
	}
	if !reflect.DeepEqual(candidates[0].SimilarityScores, originalScores) {
		t.Fatal("Rank() mutated candidate similarity scores")
	}
	if !reflect.DeepEqual(recent, map[string]int64{"song-1": 2}) {
		t.Fatal("Rank() mutated recent play counts")
	}
	if !reflect.DeepEqual(fatigue, map[string]float64{"song-1": 0.4}) {
		t.Fatal("Rank() mutated fatigue values")
	}
}

func TestRankHandlesLimitAndInvalidScores(t *testing.T) {
	candidates := []Candidate{
		{MediaFile: model.MediaFile{ID: "a"}, SimilarityScores: []agents.SimilarityScore{{NormalizedScore: math.NaN()}}},
		{MediaFile: model.MediaFile{ID: "b"}, SimilarityScores: []agents.SimilarityScore{{NormalizedScore: math.Inf(1)}}},
		{MediaFile: model.MediaFile{ID: "c"}, SimilarityScores: []agents.SimilarityScore{{NormalizedScore: 0.5}}},
	}

	results := Rank(candidates, Options{Limit: 2, Weights: Weights{Similarity: 1}})
	if len(results) != 2 {
		t.Fatalf("Rank() returned %d candidates, want limit 2", len(results))
	}
	if results[0].ID != "c" {
		t.Fatalf("first candidate ID = %q, want c", results[0].ID)
	}
	if math.IsNaN(results[0].Score) || math.IsInf(results[0].Score, 0) {
		t.Fatalf("first candidate score = %v, want finite", results[0].Score)
	}
}

func TestRankWithZeroReferenceTimeOmitsTimeDependentSignals(t *testing.T) {
	candidates := []Candidate{{MediaFile: model.MediaFile{
		ID: "song-1",
		Annotations: model.Annotations{
			PlayDate: timePtr(time.Now().Add(-time.Hour)),
		},
	}}}

	result := Rank(candidates, Options{Fatigue: map[string]float64{"song-1": 0.5}})[0]
	if result.Breakdown.Recency != 0 {
		t.Errorf("recency = %v, want 0 without reference time", result.Breakdown.Recency)
	}
	if result.Breakdown.Fatigue >= 0 {
		t.Errorf("fatigue = %v, want a penalty", result.Breakdown.Fatigue)
	}
}

func rankedIDs(results []RankedCandidate) []string {
	ids := make([]string, len(results))
	for i, result := range results {
		ids[i] = result.ID
	}
	return ids
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func almostEqual(left, right float64) bool {
	return math.Abs(left-right) < 1e-9
}
