package quickpick

import (
	"sort"
	"strings"
	"time"

	"github.com/navidrome/navidrome/model"
)

type compositionOptions struct {
	Limit     int
	Now       time.Time
	Exposures map[string]model.QuickPickExposureMetric
}

type composedQuickPick struct {
	Items            []model.QuickPickItem
	SeedSongs        []model.MediaFile
	SelectedTrackIDs map[string]struct{}
}

func composeQuickPick(songs []songCandidate, playlists []playlistCandidate, options compositionOptions) composedQuickPick {
	if options.Limit <= 0 {
		options.Limit = 9
	}
	orderedSongs := append([]songCandidate(nil), songs...)
	sort.SliceStable(orderedSongs, func(left, right int) bool {
		if orderedSongs[left].baseRank != orderedSongs[right].baseRank {
			return orderedSongs[left].baseRank < orderedSongs[right].baseRank
		}
		if orderedSongs[left].baseScore != orderedSongs[right].baseScore {
			return orderedSongs[left].baseScore > orderedSongs[right].baseScore
		}
		return orderedSongs[left].song.ID < orderedSongs[right].song.ID
	})

	playlistSlots := minQuickPick(2, len(playlists))
	if uniqueSongCount(orderedSongs) < 6 {
		playlistSlots = minQuickPick(3, len(playlists))
	}
	playlistSlots = minQuickPick(playlistSlots, options.Limit)
	songSlots := options.Limit - playlistSlots

	selectedSongs := make([]songCandidate, 0, minQuickPick(songSlots, len(orderedSongs)))
	selectedIDs := make(map[string]struct{}, len(orderedSongs))
	artistCounts := map[string]int{}
	albumCounts := map[string]int{}
	addSong := func(candidate songCandidate) bool {
		identity := quickPickSongIdentity(candidate.song)
		if _, exists := selectedIDs[identity]; exists {
			return false
		}
		selectedIDs[identity] = struct{}{}
		selectedSongs = append(selectedSongs, candidate)
		if artist := normalizeQuickPickValue(candidate.song.Artist); artist != "" {
			artistCounts[artist]++
		}
		if album := normalizeQuickPickValue(candidate.song.Album); album != "" {
			albumCounts[album]++
		}
		return true
	}

	if songSlots > 0 {
		for _, candidate := range orderedSongs {
			if addSong(candidate) {
				break
			}
		}
	}

	selectBest := func(predicate func(songCandidate) bool) bool {
		best := -1
		bestScore := -1e300
		for index, candidate := range orderedSongs {
			if predicate != nil && !predicate(candidate) {
				continue
			}
			if _, exists := selectedIDs[quickPickSongIdentity(candidate.song)]; exists {
				continue
			}
			score := songSelectionScore(candidate, artistCounts, albumCounts)
			if best < 0 || score > bestScore || (score == bestScore && candidate.baseRank < orderedSongs[best].baseRank) {
				best = index
				bestScore = score
			}
		}
		if best < 0 {
			return false
		}
		return addSong(orderedSongs[best])
	}

	if len(selectedSongs) < songSlots {
		selectBest(func(candidate songCandidate) bool { return candidate.fromLiked })
	}
	if len(selectedSongs) < songSlots {
		selectBest(func(candidate songCandidate) bool { return candidate.baseRank >= 5 && candidate.baseRank <= 20 })
	}
	if len(selectedSongs) < songSlots {
		selectBest(func(candidate songCandidate) bool { return candidate.baseRank >= 20 && candidate.baseRank <= 60 })
	}
	for len(selectedSongs) < songSlots {
		if !selectBest(nil) {
			break
		}
	}

	orderedPlaylists := append([]playlistCandidate(nil), playlists...)
	sort.SliceStable(orderedPlaylists, func(left, right int) bool {
		leftScore := playlistSelectionScore(orderedPlaylists[left], options)
		rightScore := playlistSelectionScore(orderedPlaylists[right], options)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return orderedPlaylists[left].playlist.ID < orderedPlaylists[right].playlist.ID
	})

	items := make([]model.QuickPickItem, 0, minQuickPick(options.Limit, len(selectedSongs)+playlistSlots))
	selectedTrackIDs := make(map[string]struct{}, len(selectedSongs))
	seedSongs := make([]model.MediaFile, 0, minQuickPick(recommendationSeedCount, len(selectedSongs)))
	for _, candidate := range selectedSongs {
		if len(items) >= options.Limit {
			break
		}
		song := candidate.song
		itemScore := candidate.adjustedScore
		if len(items) == 0 {
			itemScore = candidate.baseScore
		}
		items = append(items, model.QuickPickItem{Kind: model.QuickPickSong, Song: &song, Score: itemScore})
		if song.ID != "" {
			selectedTrackIDs[song.ID] = struct{}{}
		}
		if len(seedSongs) < recommendationSeedCount {
			seedSongs = append(seedSongs, song)
		}
	}
	for index := 0; index < playlistSlots && len(items) < options.Limit && index < len(orderedPlaylists); index++ {
		playlist := orderedPlaylists[index].playlist
		items = append(items, model.QuickPickItem{
			Kind:     model.QuickPickPlaylist,
			Playlist: &playlist,
			Score:    playlistSelectionScore(orderedPlaylists[index], options),
		})
	}

	return composedQuickPick{Items: items, SeedSongs: seedSongs, SelectedTrackIDs: selectedTrackIDs}
}

func uniqueSongCount(songs []songCandidate) int {
	seen := make(map[string]struct{}, len(songs))
	for _, candidate := range songs {
		seen[quickPickSongIdentity(candidate.song)] = struct{}{}
	}
	return len(seen)
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

func playlistSelectionScore(candidate playlistCandidate, options compositionOptions) float64 {
	if options.Exposures != nil {
		if metric, ok := options.Exposures[playlistExposureKey(candidate.playlist.ID)]; ok {
			return candidate.normalizedScore - .70*exposureFatigue(metric, options.Now)
		}
	}
	if candidate.hasAdjustedScore {
		return candidate.adjustedScore
	}
	if candidate.normalizedScore != 0 {
		return candidate.normalizedScore
	}
	return candidate.baseScore
}

func quickPickSongIdentity(song model.MediaFile) string {
	if song.ID != "" {
		return song.ID
	}
	if song.Path != "" {
		return "path:" + song.Path
	}
	return "title:" + normalizeQuickPickValue(song.Title) + "|artist:" + normalizeQuickPickValue(song.Artist)
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
