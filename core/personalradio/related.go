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
	relatedArtist         = "related_artist"
	relatedArtistDownload = "related_artist_download"
	relatedLocalClose     = "related_local_close"
	relatedLocalFamily    = "related_local_family"
	relatedReplay         = "related_replay"
)

// These values describe a video category, not a musical style. Treating
// "Music" as an exact genre match makes every imported video appear related.
var uninformativeRelatedGenres = map[string]bool{
	"music": true, "people and blogs": true, "entertainment": true,
	"travel and events": true, "gaming": true, "howto and style": true,
	"film and animation": true, "education": true, "comedy": true,
	"news and politics": true, "science and technology": true,
	"sports": true, "autos and vehicles": true, "pets and animals": true,
	"nonprofits and activism": true, "shows": true, "movies": true,
	"trailers": true,
}

func relatedGenres(file model.MediaFile) map[string]bool {
	genres := genreSet(file)
	for genre := range genres {
		if uninformativeRelatedGenres[normalizeGenre(genre)] {
			delete(genres, genre)
		}
	}
	return genres
}

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
	genreMatch := genreAffinity(relatedGenres(*seed), relatedGenres(file))
	if localFallbackArtistRelationship(*seed, file) > 0 || genreMatch >= 2 {
		return 3
	}
	if genreMatch >= 1 {
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
	case relatedArtist:
		return 2
	case relatedLocalClose:
		return 3
	case relatedProviderBroad:
		return 4
	case relatedLocalFamily:
		return 5
	case relatedArtistDownload:
		return 6
	case relatedReplay:
		return 7
	default:
		return 8
	}
}

func relatedCandidateTier(candidate rankedRadioCandidate) int {
	if candidate.source == relatedReplay && candidate.replayEarlySkipped {
		return 8
	}
	if candidate.isDiscovery {
		return 6
	}
	return relatedSourceTier(candidate.source)
}

func relatedReplayOlder(left, right rankedRadioCandidate) bool {
	if !left.replayTime.Equal(right.replayTime) {
		if left.replayTime.IsZero() {
			return true
		}
		if right.replayTime.IsZero() {
			return false
		}
		return left.replayTime.Before(right.replayTime)
	}
	return left.replayPosition < right.replayPosition
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
		leftTier, rightTier := relatedCandidateTier(remaining[left]), relatedCandidateTier(remaining[right])
		if leftTier != rightTier {
			return leftTier < rightTier
		}
		if remaining[left].source == relatedReplay && remaining[right].source == relatedReplay &&
			(!remaining[left].replayTime.Equal(remaining[right].replayTime) ||
				remaining[left].replayPosition != remaining[right].replayPosition) {
			return relatedReplayOlder(remaining[left], remaining[right])
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
		bestTier := 9
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
			tier := relatedCandidateTier(candidate)
			score := candidateSelectionScore(candidate, artistCounts, albumCounts)
			prefer := tier < bestTier
			if tier == bestTier && best >= 0 {
				if candidate.source == relatedReplay && remaining[best].source == relatedReplay &&
					(!candidate.replayTime.Equal(remaining[best].replayTime) ||
						candidate.replayPosition != remaining[best].replayPosition) {
					prefer = relatedReplayOlder(candidate, remaining[best])
				} else {
					prefer = score > bestScore
				}
			}
			if prefer {
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
