package personalradio

import (
	"math"
	"sort"
	"strings"

	"github.com/navidrome/navidrome/model"
)

func discoveryRatioForRadioMode(mode string) float64 {
	switch model.NormalizeRadioMode(mode) {
	case model.RadioModeFamiliar:
		return 0.15
	case model.RadioModeDiscover:
		return 0.70
	default:
		return 0.35
	}
}

type radioCompositionOptions struct {
	Mode              string
	Slots             int
	Active            []model.PersonalRadioItem
	HasDownloading    bool
	ReadyLibraryFloor int
}

// composeRadioCandidates selects one ranked stream into the next queue batch.
// The target discovery quota is calculated from the active client queue, then
// a greedy score/diversity pass fills the batch. Quotas are relaxed only when
// the available stream cannot satisfy them.
func composeRadioCandidates(candidates []rankedRadioCandidate, options radioCompositionOptions) []rankedRadioCandidate {
	if options.Slots <= 0 || len(candidates) == 0 {
		return nil
	}
	if options.ReadyLibraryFloor <= 0 {
		options.ReadyLibraryFloor = 2
	}
	ordered := append([]rankedRadioCandidate(nil), candidates...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].ranked.Score != ordered[right].ranked.Score {
			return ordered[left].ranked.Score > ordered[right].ranked.Score
		}
		return ordered[left].candidate.Key < ordered[right].candidate.Key
	})

	activeDiscovery, activeTotal, readyLibrary := 0, 0, 0
	artistCounts := map[string]int{}
	albumCounts := map[string]int{}
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
			artist := normalizeCompositionValue(item.Song.Artist)
			album := normalizeCompositionValue(item.Song.Album)
			if artist != "" {
				artistCounts[artist]++
			}
			if album != "" {
				albumCounts[album]++
			}
		}
	}
	desiredDiscovery := int(math.Round(discoveryRatioForRadioMode(options.Mode) * float64(activeTotal+options.Slots)))
	discoveryNeeded := maxIntForComposition(0, desiredDiscovery-activeDiscovery)
	if discoveryNeeded > options.Slots {
		discoveryNeeded = options.Slots
	}

	selected := make([]rankedRadioCandidate, 0, options.Slots)
	remaining := append([]rankedRadioCandidate(nil), ordered...)
	discoveries, locals := 0, 0
	for len(selected) < options.Slots && len(remaining) > 0 {
		best := -1
		bestScore := math.Inf(-1)
		for index, candidate := range remaining {
			isDiscovery := candidate.isDiscovery
			if isDiscovery && discoveries >= discoveryNeeded && hasRadioCandidateOfType(remaining, false) {
				continue
			}
			if !isDiscovery && locals >= options.Slots-discoveryNeeded && hasRadioCandidateOfType(remaining, true) {
				continue
			}
			if isDiscovery && options.HasDownloading && readyLibrary < options.ReadyLibraryFloor && hasRadioCandidateOfType(remaining, false) {
				continue
			}
			score := candidateSelectionScore(candidate, artistCounts, albumCounts)
			if score > bestScore {
				best, bestScore = index, score
			}
		}
		if best < 0 {
			// There was no candidate satisfying the quota/buffer guard. Fill
			// from the remaining ranked stream rather than returning a short
			// queue when one side of the pool is exhausted.
			for index, candidate := range remaining {
				score := candidateSelectionScore(candidate, artistCounts, albumCounts)
				if score > bestScore {
					best, bestScore = index, score
				}
			}
		}
		if best < 0 {
			break
		}
		candidate := remaining[best]
		remaining = append(remaining[:best], remaining[best+1:]...)
		selected = append(selected, candidate)
		if candidate.isDiscovery {
			discoveries++
		} else {
			locals++
			if candidate.candidate.MediaFile.ID != "" {
				readyLibrary++
			}
		}
		artist := normalizeCompositionValue(candidate.candidate.MediaFile.Artist)
		album := normalizeCompositionValue(candidate.candidate.MediaFile.Album)
		if artist != "" {
			artistCounts[artist]++
		}
		if album != "" {
			albumCounts[album]++
		}
	}
	return selected
}

func candidateSelectionScore(candidate rankedRadioCandidate, artistCounts, albumCounts map[string]int) float64 {
	score := candidate.ranked.Score
	artist := normalizeCompositionValue(candidate.candidate.MediaFile.Artist)
	album := normalizeCompositionValue(candidate.candidate.MediaFile.Album)
	transition := candidate.ranked.Breakdown.TransitionAffinity
	penaltyScale := 1.0
	if transition > 0.75 {
		penaltyScale = 0.25
	}
	if count := artistCounts[artist]; artist != "" && count > 0 {
		score -= penaltyScale * (0.85 + 0.65*float64(count-1))
	}
	if count := albumCounts[album]; album != "" && count > 0 {
		score -= penaltyScale * (0.45 + 0.35*float64(count-1))
	}
	return score
}

func hasRadioCandidateOfType(candidates []rankedRadioCandidate, discovery bool) bool {
	for _, candidate := range candidates {
		if candidate.isDiscovery == discovery {
			return true
		}
	}
	return false
}

func normalizeCompositionValue(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func maxIntForComposition(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func radioCompositionTypeCounts(items []model.PersonalRadioItem) (known, discovery int) {
	for _, item := range items {
		if item.ItemType == model.RadioItemDiscovery {
			discovery++
		} else if item.ItemType == model.RadioItemLibrary {
			known++
		}
	}
	return known, discovery
}

func radioCompositionTypeCountsFromCandidates(items []rankedRadioCandidate) (known, discovery int) {
	for _, item := range items {
		if item.isDiscovery {
			discovery++
		} else {
			known++
		}
	}
	return known, discovery
}

func readyLibraryBufferCount(items []model.PersonalRadioItem) int {
	count := 0
	for _, item := range items {
		if item.ItemType == model.RadioItemLibrary && item.Status == model.RadioItemReady {
			count++
		}
	}
	return count
}
