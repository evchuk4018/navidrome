package quickpick

import (
	"sort"
	"strings"

	"github.com/navidrome/navidrome/model"
)

type compositionOptions struct {
	Limit        int
	Exposures    map[string]model.QuickPickExposureMetric
	PreviousView *model.QuickPickView
}

type composedQuickPick struct {
	Items            []model.QuickPickItem
	SeedSongs        []model.MediaFile
	SelectedTrackIDs map[string]struct{}
}

const (
	listenAgainSongQuota = 3
	startRadioSongQuota  = 9
)

func composeQuickPick(songs []songCandidate, options compositionOptions) composedQuickPick {
	if options.Limit <= 0 {
		options.Limit = listenAgainSongQuota + startRadioSongQuota
	}
	options.Limit = minQuickPick(options.Limit, listenAgainSongQuota+startRadioSongQuota)
	orderedSongs := make([]songCandidate, 0, len(songs))
	for _, candidate := range songs {
		if candidate.song.ID != "" && !candidate.song.Missing {
			orderedSongs = append(orderedSongs, candidate)
		}
	}
	sort.SliceStable(orderedSongs, func(left, right int) bool {
		if orderedSongs[left].baseRank != orderedSongs[right].baseRank {
			return orderedSongs[left].baseRank < orderedSongs[right].baseRank
		}
		if orderedSongs[left].baseScore != orderedSongs[right].baseScore {
			return orderedSongs[left].baseScore > orderedSongs[right].baseScore
		}
		return orderedSongs[left].song.ID < orderedSongs[right].song.ID
	})

	previousKeys := map[string]bool{}
	clickedKeys := map[string]bool{}
	if options.PreviousView != nil {
		for _, key := range options.PreviousView.ItemKeys {
			previousKeys[key] = true
		}
		for _, key := range options.PreviousView.ClickedItemKeys {
			clickedKeys[key] = previousKeys[key]
		}
	}

	listenSlots := minQuickPick(listenAgainSongQuota, options.Limit)
	discoverySlots := options.Limit - listenSlots
	familiar := make([]songCandidate, 0, listenSlots)
	discovery := make([]songCandidate, 0, discoverySlots)
	selectedIDs := make(map[string]struct{}, options.Limit)
	artistCounts := map[string]int{}
	albumCounts := map[string]int{}
	addSong := func(candidate songCandidate, target *[]songCandidate) bool {
		if _, exists := selectedIDs[candidate.song.ID]; exists {
			return false
		}
		selectedIDs[candidate.song.ID] = struct{}{}
		if artist := normalizeQuickPickValue(candidate.song.Artist); artist != "" {
			artistCounts[artist]++
		}
		if album := normalizeQuickPickValue(candidate.song.Album); album != "" {
			albumCounts[album]++
		}
		*target = append(*target, candidate)
		return true
	}
	selectSong := func(predicate func(songCandidate) bool, target *[]songCandidate, useBase, oldestFirst bool) bool {
		best := -1
		bestScore := 0.0
		for index, candidate := range orderedSongs {
			if predicate != nil && !predicate(candidate) {
				continue
			}
			if _, exists := selectedIDs[candidate.song.ID]; exists {
				continue
			}
			if useBase {
				candidate.adjustedScore = candidate.baseScore
			}
			score := songSelectionScore(candidate, artistCounts, albumCounts)
			if best >= 0 && oldestFirst {
				shown := options.Exposures[trackExposureKey(candidate.song.ID)].LastShownAt
				bestShown := options.Exposures[trackExposureKey(orderedSongs[best].song.ID)].LastShownAt
				if !shown.Equal(bestShown) {
					if shown.Before(bestShown) {
						best, bestScore = index, score
					}
					continue
				}
			}
			if best < 0 || score > bestScore {
				best, bestScore = index, score
			}
		}
		if best < 0 {
			return false
		}
		return addSong(orderedSongs[best], target)
	}

	// Keep the familiar favorite and exploration buckets, without imposing
	// discovery exposure penalties on the familiar row.
	if listenSlots > 0 && len(orderedSongs) > 0 {
		addSong(orderedSongs[0], &familiar)
	}
	for _, predicate := range []func(songCandidate) bool{
		func(candidate songCandidate) bool { return candidate.fromLiked },
		func(candidate songCandidate) bool { return candidate.baseRank >= 5 && candidate.baseRank <= 20 },
		func(candidate songCandidate) bool { return candidate.baseRank >= 20 && candidate.baseRank <= 60 },
		nil,
	} {
		for len(familiar) < listenSlots && selectSong(predicate, &familiar, true, false) {
		}
	}

	// Only clicks from the last displayed grid earn another visit. A carried
	// song must be clicked in its new view to earn the following visit as well.
	for len(discovery) < discoverySlots && selectSong(func(candidate songCandidate) bool {
		return clickedKeys[trackExposureKey(candidate.song.ID)]
	}, &discovery, false, false) {
	}
	for len(discovery) < discoverySlots && selectSong(func(candidate songCandidate) bool {
		return !previousKeys[trackExposureKey(candidate.song.ID)]
	}, &discovery, false, false) {
	}
	// Exhaust all alternatives before repeating unclicked songs. Small libraries
	// remain usable, with the oldest displays receiving repeat slots first.
	for len(discovery) < discoverySlots && selectSong(nil, &discovery, false, true) {
	}

	items := make([]model.QuickPickItem, 0, len(familiar)+len(discovery))
	seedSongs := make([]model.MediaFile, 0, len(discovery))
	appendSong := func(candidate songCandidate, section string) {
		song := candidate.song
		score := candidate.adjustedScore
		if section == model.QuickPickSectionListenAgain {
			score = candidate.baseScore
		} else {
			seedSongs = append(seedSongs, song)
		}
		items = append(items, model.QuickPickItem{Kind: model.QuickPickSong, Song: &song, Section: section, Score: score})
	}
	for _, candidate := range familiar {
		appendSong(candidate, model.QuickPickSectionListenAgain)
	}
	for _, candidate := range discovery {
		appendSong(candidate, model.QuickPickSectionStartRadio)
	}
	return composedQuickPick{Items: items, SeedSongs: seedSongs, SelectedTrackIDs: selectedIDs}
}

func songSelectionScore(candidate songCandidate, artistCounts, albumCounts map[string]int) float64 {
	score := candidate.adjustedScore
	if artist := normalizeQuickPickValue(candidate.song.Artist); artist != "" {
		if count := artistCounts[artist]; count > 0 {
			score -= .40 + .25*float64(count-1)
		}
	}
	if album := normalizeQuickPickValue(candidate.song.Album); album != "" {
		if count := albumCounts[album]; count > 0 {
			score -= .20 + .15*float64(count-1)
		}
	}
	return score
}

func normalizeQuickPickValue(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func minQuickPick(left, right int) int {
	if left < right {
		return left
	}
	return right
}
