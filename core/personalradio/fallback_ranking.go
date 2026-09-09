package personalradio

import (
	"math"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

const (
	localFallbackRecencyHalfLife    = 14 * 24 * time.Hour
	localFallbackLibraryHalfLife    = 90 * 24 * time.Hour
	localFallbackCountSaturation    = 10
	localFallbackYearWindow         = 20
	localFallbackHistoryWeight      = 0.65
	localFallbackPreferenceWeight   = 0.35
	localFallbackRecentRepeatWeight = 0.25
	localFallbackNegativeTransition = 0.5
)

// buildLocalFallbackFeatures turns the local metadata and user/session
// signals available to Personal Radio into the normalized feature vector used
// by the fallback ranker. Provider candidates intentionally do not use this
// path because their provider similarity score is already authoritative.
func buildLocalFallbackFeatures(candidate recommendations.Candidate, seeds []radioSeed, mode string, now time.Time, feedbackFatigue float64) recommendations.LocalFallbackFeatures {
	freshness := localFallbackFreshness(candidate.PlayDate, now)
	discoveryValue := localFallbackDiscoveryValue(candidate, now)
	history := localFallbackListeningHistory(candidate)
	transition := clampLocalFallback(candidate.TransitionAffinity, 0, 1)
	if candidate.TransitionAffinity < 0 {
		transition = 0
	}

	fatigue := clampLocalFallback(
		feedbackFatigue+
			localFallbackRecentRepeatWeight*(1-freshness)+
			localFallbackNegativeTransition*maxLocalFallback(-candidate.TransitionAffinity, 0),
		0,
		1,
	)

	var discoveryPreference float64
	switch model.NormalizeRadioMode(mode) {
	case model.RadioModeFamiliar:
		discoveryPreference = 1 - discoveryValue
	case model.RadioModeDiscover:
		discoveryPreference = discoveryValue
	default:
		discoveryPreference = 0.5*discoveryValue + 0.5*history
	}

	seedSimilarity, genreOverlap := localFallbackSessionAffinity(candidate.MediaFile, seeds)
	return recommendations.LocalFallbackFeatures{
		SeedSimilarity:      seedSimilarity,
		ListeningHistory:    history,
		TransitionRelevance: transition,
		GenreOverlap:        genreOverlap,
		Freshness:           freshness,
		DiscoveryPreference: clampLocalFallback(discoveryPreference, 0, 1),
		FatiguePenalty:      fatigue,
	}
}

func localFallbackSessionAffinity(candidate model.MediaFile, seeds []radioSeed) (float64, float64) {
	var totalWeight, seedSimilarity, genreOverlap float64
	for _, seed := range seeds {
		if seed.File == nil || seed.Weight <= 0 {
			continue
		}
		totalWeight += seed.Weight
		seedSimilarity += seed.Weight * localFallbackSeedSimilarity(*seed.File, candidate)
		genreOverlap += seed.Weight * localFallbackGenreOverlap(*seed.File, candidate)
	}
	if totalWeight <= 0 {
		return 0, 0
	}
	return clampLocalFallback(seedSimilarity/totalWeight, 0, 1), clampLocalFallback(genreOverlap/totalWeight, 0, 1)
}

func localFallbackSeedSimilarity(seed, candidate model.MediaFile) float64 {
	artist := localFallbackArtistRelationship(seed, candidate)
	year := localFallbackYearProximity(seed, candidate)
	album := localFallbackAlbumRelationship(seed, candidate)
	return clampLocalFallback(0.60*artist+0.25*year+0.15*album, 0, 1)
}

func localFallbackGenreOverlap(seed, candidate model.MediaFile) float64 {
	return clampLocalFallback(genreAffinity(genreSet(seed), genreSet(candidate))/3, 0, 1)
}

func localFallbackArtistRelationship(seed, candidate model.MediaFile) float64 {
	seedIDs, seedNames := localFallbackArtistKeys(seed)
	candidateIDs, candidateNames := localFallbackArtistKeys(candidate)
	for key := range seedIDs {
		if candidateIDs[key] {
			return 1
		}
	}
	for key := range seedNames {
		if candidateNames[key] {
			return 0.85
		}
	}
	return 0
}

func localFallbackArtistKeys(file model.MediaFile) (map[string]bool, map[string]bool) {
	ids := map[string]bool{}
	names := map[string]bool{}
	addID := func(value string) {
		if value = normalizeLocalFallback(value); value != "" {
			ids[value] = true
		}
	}
	addName := func(value string) {
		if value = normalizeLocalFallback(value); value != "" {
			names[value] = true
		}
	}
	addID(file.ArtistID)
	addID(file.MbzArtistID)
	addID(file.AlbumArtistID)
	addID(file.MbzAlbumArtistID)
	addName(file.Artist)
	addName(file.AlbumArtist)
	for _, participant := range file.Participants.AllArtists() {
		addID(participant.ID)
		addID(participant.MbzArtistID)
		addName(participant.Name)
		addName(participant.SortArtistName)
	}
	return ids, names
}

func localFallbackAlbumRelationship(seed, candidate model.MediaFile) float64 {
	if equalLocalFallback(seed.AlbumID, candidate.AlbumID) || equalLocalFallback(seed.MbzAlbumID, candidate.MbzAlbumID) {
		return 1
	}
	if normalizeLocalFallback(seed.Album) != "" && equalLocalFallback(seed.Album, candidate.Album) &&
		localFallbackArtistRelationship(seed, candidate) > 0 {
		return 0.85
	}
	return 0
}

func localFallbackYearProximity(seed, candidate model.MediaFile) float64 {
	seedYear, candidateYear := localFallbackYear(seed), localFallbackYear(candidate)
	if seedYear == 0 || candidateYear == 0 {
		return 0
	}
	difference := math.Abs(float64(seedYear - candidateYear))
	return clampLocalFallback(1-difference/localFallbackYearWindow, 0, 1)
}

func localFallbackYear(file model.MediaFile) int {
	if file.OriginalYear != 0 {
		return file.OriginalYear
	}
	if file.Year != 0 {
		return file.Year
	}
	return file.ReleaseYear
}

func localFallbackListeningHistory(candidate recommendations.Candidate) float64 {
	preference := math.Max(
		math.Max(boolLocalFallback(candidate.Starred), clampLocalFallback(float64(candidate.Rating)/5, 0, 1)),
		localFallbackTasteScore(candidate),
	)
	return clampLocalFallback(
		localFallbackHistoryWeight*normalizedLocalFallbackCount(candidate.PlayCount)+
			localFallbackPreferenceWeight*preference,
		0,
		1,
	)
}

func localFallbackTasteScore(candidate recommendations.Candidate) float64 {
	if candidate.TasteAffinity != 0 {
		return clampLocalFallback(candidate.TasteAffinity, 0, 1)
	}
	return clampLocalFallback(candidate.TasteDetails.Score, 0, 1)
}

func localFallbackFreshness(playDate *time.Time, now time.Time) float64 {
	if playDate == nil || now.IsZero() {
		return 1
	}
	age := now.Sub(playDate.UTC())
	if age < 0 {
		age = 0
	}
	return clampLocalFallback(1-math.Exp(-float64(age)/float64(localFallbackRecencyHalfLife)), 0, 1)
}

func localFallbackDiscoveryValue(candidate recommendations.Candidate, now time.Time) float64 {
	newLibraryValue := 0.0
	if !candidate.CreatedAt.IsZero() && !now.IsZero() {
		age := now.Sub(candidate.CreatedAt.UTC())
		if age < 0 {
			age = 0
		}
		newLibraryValue = math.Exp(-float64(age) / float64(localFallbackLibraryHalfLife))
	}
	return clampLocalFallback(
		0.7*(1-normalizedLocalFallbackCount(candidate.PlayCount))+0.3*newLibraryValue,
		0,
		1,
	)
}

func normalizedLocalFallbackCount(count int64) float64 {
	if count <= 0 {
		return 0
	}
	return math.Min(1, math.Log1p(float64(count))/math.Log1p(localFallbackCountSaturation))
}

func localFallbackHasAffinity(seed *model.MediaFile, candidate model.MediaFile) bool {
	if seed == nil {
		return false
	}
	return localFallbackSeedSimilarity(*seed, candidate) > 0 || localFallbackGenreOverlap(*seed, candidate) > 0
}

func isLocalFallbackSource(source string) bool {
	return source == "tasteFallback" || source == "exhaustiveFallback"
}

func normalizeLocalFallback(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func equalLocalFallback(left, right string) bool {
	left = normalizeLocalFallback(left)
	right = normalizeLocalFallback(right)
	return left != "" && left == right
}

func boolLocalFallback(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func clampLocalFallback(value, lower, upper float64) float64 {
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

func maxLocalFallback(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
