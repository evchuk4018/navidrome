package personalradio

import (
	"math"
	"sort"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/model"
)

const (
	relatedProviderStrong = "related_provider_strong"
	relatedProviderBroad  = "related_provider_broad"
	relatedLocalClose     = "related_local_close"
	relatedLocalFamily    = "related_local_family"
)

// The provider's own ordering is used only when it supplies no usable score.
// Its lower-ranked unscored tail is never eligible for an automatic download.
func relatedProviderTier(song agents.Song, index int) (int, float64) {
	var confidence float64
	for _, score := range song.SimilarityScores {
		if !math.IsNaN(score.NormalizedScore) && !math.IsInf(score.NormalizedScore, 0) {
			confidence = max(confidence, score.NormalizedScore)
		}
	}
	if confidence >= 0.5 {
		return 1, min(confidence, 1)
	}
	if confidence >= 0.3 {
		return 2, confidence
	}
	if len(song.SimilarityScores) > 0 || index >= 20 {
		return 0, 0
	}
	if index < 10 {
		return 1, 0.5 - float64(index)*0.01
	}
	return 2, 0.3
}

func relatedProviderSource(tier int) string {
	switch tier {
	case 1:
		return relatedProviderStrong
	case 2:
		return relatedProviderBroad
	default:
		return ""
	}
}

func relatedLocalTier(seed *model.MediaFile, file model.MediaFile) int {
	if seed == nil {
		return 0
	}
	if localFallbackArtistRelationship(*seed, file) > 0 || genreAffinity(genreSet(*seed), genreSet(file)) >= 2 {
		return 3
	}
	if genreAffinity(genreSet(*seed), genreSet(file)) >= 1 {
		return 4
	}
	return 0
}

func relatedLocalSource(tier int) string {
	if tier == 3 {
		return relatedLocalClose
	}
	return relatedLocalFamily
}

func relatedSourceTier(source string) int {
	switch source {
	case relatedProviderStrong:
		return 1
	case relatedLocalClose:
		return 2
	case relatedProviderBroad:
		return 3
	case relatedLocalFamily:
		return 4
	default:
		return 5
	}
}

// Related radio treats discovery as a ceiling, not a quota. A discovery must
// fit within 35% of the actual active queue, including the playing seed.
func composeRelatedRadioCandidates(candidates []rankedRadioCandidate, options radioCompositionOptions) []rankedRadioCandidate {
	if options.Slots <= 0 || len(candidates) == 0 {
		return nil
	}
	if options.ReadyLibraryFloor <= 0 {
		options.ReadyLibraryFloor = 2
	}
	remaining := append([]rankedRadioCandidate(nil), candidates...)
	sort.SliceStable(remaining, func(left, right int) bool {
		leftTier, rightTier := relatedSourceTier(remaining[left].source), relatedSourceTier(remaining[right].source)
		if leftTier != rightTier {
			return leftTier < rightTier
		}
		if remaining[left].ranked.Score != remaining[right].ranked.Score {
			return remaining[left].ranked.Score > remaining[right].ranked.Score
		}
		return remaining[left].candidate.Key < remaining[right].candidate.Key
	})

	activeTotal, activeDiscovery, readyLibrary := 0, 0, 0
	if options.SeedActive {
		activeTotal++
	}
	artistCounts, albumCounts := map[string]int{}, map[string]int{}
	for _, item := range options.Active {
		if item.ItemType == model.RadioItemSeed || item.Status == model.RadioItemFailed || item.Status == model.RadioItemPlayed {
			continue
		}
		activeTotal++
		if item.ItemType == model.RadioItemDiscovery {
			activeDiscovery++
		}
		if item.ItemType == model.RadioItemLibrary && item.Status == model.RadioItemReady {
			readyLibrary++
		}
		if item.Song != nil {
			artistCounts[normalizeCompositionValue(item.Song.Artist)]++
			albumCounts[normalizeCompositionValue(item.Song.Album)]++
		}
	}

	selected := make([]rankedRadioCandidate, 0, options.Slots)
	selectedDiscovery := 0
	for len(selected) < options.Slots && len(remaining) > 0 {
		best := -1
		bestTier := 6
		bestScore := math.Inf(-1)
		for index, candidate := range remaining {
			if candidate.isDiscovery {
				queueAfter := activeTotal + len(selected) + 1
				maxDiscovery := int(math.Floor(0.35 * float64(queueAfter)))
				if activeDiscovery+selectedDiscovery+1 > maxDiscovery {
					continue
				}
				if options.HasDownloading && readyLibrary < options.ReadyLibraryFloor {
					continue
				}
			}
			tier := relatedSourceTier(candidate.source)
			score := candidateSelectionScore(candidate, artistCounts, albumCounts)
			if tier < bestTier || (tier == bestTier && score > bestScore) {
				best, bestTier, bestScore = index, tier, score
			}
		}
		if best < 0 {
			break
		}
		candidate := remaining[best]
		remaining = append(remaining[:best], remaining[best+1:]...)
		selected = append(selected, candidate)
		if candidate.isDiscovery {
			selectedDiscovery++
		} else {
			readyLibrary++
		}
		if artist := normalizeCompositionValue(candidate.candidate.MediaFile.Artist); artist != "" {
			artistCounts[artist]++
		}
		if album := normalizeCompositionValue(candidate.candidate.MediaFile.Album); album != "" {
			albumCounts[album]++
		}
	}
	return selected
}
