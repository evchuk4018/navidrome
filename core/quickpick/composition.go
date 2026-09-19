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

const (
	listenAgainSongQuota     = 4
	listenAgainPlaylistQuota = 2
	startRadioSongQuota      = 6
	listenAgainCooldown      = 6 * time.Hour
	startRadioCooldown       = 24 * time.Hour
)

func composeQuickPick(songs []songCandidate, playlists []playlistCandidate, options compositionOptions) composedQuickPick {
	if options.Limit <= 0 {
		options.Limit = listenAgainSongQuota + listenAgainPlaylistQuota + startRadioSongQuota
	}
	options.Limit = minQuickPick(options.Limit, listenAgainSongQuota+listenAgainPlaylistQuota+startRadioSongQuota)
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

	playlistSlots := minQuickPick(listenAgainPlaylistQuota, len(playlists))
	// Keep the old sparse-library behavior for callers that explicitly request
	// the legacy nine-tile layout. The normal service path uses twelve tiles and
	// always targets two Listen Again playlists before backfilling.
	if options.Limit <= 9 && uniqueSongCount(orderedSongs) < 6 {
		playlistSlots = minQuickPick(3, len(playlists))
	}
	playlistSlots = minQuickPick(playlistSlots, options.Limit)
	listenSongSlots := minQuickPick(listenAgainSongQuota, options.Limit-playlistSlots)
	radioSlots := minQuickPick(startRadioSongQuota, options.Limit-listenSongSlots-playlistSlots)

	selectedListenSongs := make([]songCandidate, 0, minQuickPick(listenSongSlots, len(orderedSongs)))
	selectedRadioSongs := make([]songCandidate, 0, minQuickPick(radioSlots, len(orderedSongs)))
	selectedIDs := make(map[string]struct{}, len(orderedSongs))
	artistCounts := map[string]int{}
	albumCounts := map[string]int{}
	addSong := func(candidate songCandidate, target *[]songCandidate) bool {
		identity := quickPickSongIdentity(candidate.song)
		if _, exists := selectedIDs[identity]; exists {
			return false
		}
		selectedIDs[identity] = struct{}{}
		if artist := normalizeQuickPickValue(candidate.song.Artist); artist != "" {
			artistCounts[artist]++
		}
		if album := normalizeQuickPickValue(candidate.song.Album); album != "" {
			albumCounts[album]++
		}
		*target = append(*target, candidate)
		return true
	}

	if listenSongSlots > 0 {
		for _, candidate := range orderedSongs {
			// The highest base-ranked playable song is the stable Listen Again
			// anchor. It is deliberately exempt from the exposure cooldown.
			if addSong(candidate, &selectedListenSongs) {
				break
			}
		}
	}

	selectBestSong := func(predicate func(songCandidate) bool, target *[]songCandidate, section string, cooldown time.Duration, enforceCooldown bool) bool {
		best := -1
		bestScore := -1e300
		for index, candidate := range orderedSongs {
			if predicate != nil && !predicate(candidate) {
				continue
			}
			if _, exists := selectedIDs[quickPickSongIdentity(candidate.song)]; exists {
				continue
			}
			if enforceCooldown && quickPickExposureCooling(options.Exposures, trackExposureKey(candidate.song.ID), options.Now, cooldown) {
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
		return addSong(orderedSongs[best], target)
	}

	listenCooldownEnabled := uniqueSongCount(orderedSongs) >= 1+2*listenSongSlots
	for len(selectedListenSongs) < listenSongSlots {
		before := len(selectedListenSongs)
		selectBestSong(func(candidate songCandidate) bool { return candidate.fromLiked }, &selectedListenSongs, model.QuickPickSectionListenAgain, listenAgainCooldown, listenCooldownEnabled)
		if len(selectedListenSongs) == before {
			break
		}
	}
	for len(selectedListenSongs) < listenSongSlots {
		before := len(selectedListenSongs)
		selectBestSong(func(candidate songCandidate) bool { return candidate.baseRank >= 5 && candidate.baseRank <= 20 }, &selectedListenSongs, model.QuickPickSectionListenAgain, listenAgainCooldown, listenCooldownEnabled)
		if len(selectedListenSongs) == before {
			break
		}
	}
	for len(selectedListenSongs) < listenSongSlots {
		before := len(selectedListenSongs)
		selectBestSong(func(candidate songCandidate) bool { return candidate.baseRank >= 20 && candidate.baseRank <= 60 }, &selectedListenSongs, model.QuickPickSectionListenAgain, listenAgainCooldown, listenCooldownEnabled)
		if len(selectedListenSongs) == before {
			break
		}
	}
	for len(selectedListenSongs) < listenSongSlots {
		if !selectBestSong(nil, &selectedListenSongs, model.QuickPickSectionListenAgain, listenAgainCooldown, listenCooldownEnabled) {
			break
		}
	}
	// If the cooldown left a quota short, relax it only after exhausting the
	// non-cooled alternatives. This keeps small libraries usable.
	for len(selectedListenSongs) < listenSongSlots {
		if !selectBestSong(nil, &selectedListenSongs, model.QuickPickSectionListenAgain, 0, false) {
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

	selectedPlaylists := make([]playlistCandidate, 0, playlistSlots)
	playlistCooldownEnabled := len(orderedPlaylists) >= 2*playlistSlots
	selectPlaylist := func(enforceCooldown bool) bool {
		for _, candidate := range orderedPlaylists {
			alreadySelected := false
			for _, selected := range selectedPlaylists {
				if selected.playlist.ID == candidate.playlist.ID {
					alreadySelected = true
					break
				}
			}
			if alreadySelected {
				continue
			}
			if enforceCooldown && quickPickExposureCooling(options.Exposures, playlistExposureKey(candidate.playlist.ID), options.Now, listenAgainCooldown) {
				continue
			}
			selectedPlaylists = append(selectedPlaylists, candidate)
			return true
		}
		return false
	}
	for len(selectedPlaylists) < playlistSlots {
		if !selectPlaylist(playlistCooldownEnabled) {
			break
		}
	}
	for len(selectedPlaylists) < playlistSlots {
		if !selectPlaylist(false) {
			break
		}
	}

	// Radio candidates are selected from the remaining local, playable pool.
	// They intentionally do not invoke similarity agents; the radio session can
	// do deeper discovery after the user starts playback.
	radioCooldownEnabled := uniqueSongCount(orderedSongs)-len(selectedListenSongs) >= 2*startRadioSongQuota
	for len(selectedRadioSongs) < radioSlots {
		if !selectBestSong(nil, &selectedRadioSongs, model.QuickPickSectionStartRadio, startRadioCooldown, radioCooldownEnabled) {
			break
		}
	}
	for len(selectedRadioSongs) < radioSlots {
		if !selectBestSong(nil, &selectedRadioSongs, model.QuickPickSectionStartRadio, 0, false) {
			break
		}
	}

	items := make([]model.QuickPickItem, 0, minQuickPick(options.Limit, len(selectedListenSongs)+len(selectedPlaylists)+len(selectedRadioSongs)))
	selectedTrackIDs := make(map[string]struct{}, len(selectedListenSongs)+len(selectedRadioSongs))
	seedSongs := make([]model.MediaFile, 0, len(selectedRadioSongs))
	appendSong := func(candidate songCandidate, section string) {
		if len(items) >= options.Limit {
			return
		}
		song := candidate.song
		itemScore := candidate.adjustedScore
		if len(items) == 0 {
			itemScore = candidate.baseScore
		}
		items = append(items, model.QuickPickItem{Kind: model.QuickPickSong, Song: &song, Section: section, Score: itemScore})
		if song.ID != "" {
			selectedTrackIDs[song.ID] = struct{}{}
		}
		if section == model.QuickPickSectionStartRadio {
			seedSongs = append(seedSongs, song)
		}
	}
	for _, candidate := range selectedListenSongs {
		appendSong(candidate, model.QuickPickSectionListenAgain)
	}
	for _, candidate := range selectedPlaylists {
		if len(items) >= options.Limit {
			break
		}
		playlist := candidate.playlist
		items = append(items, model.QuickPickItem{
			Kind:     model.QuickPickPlaylist,
			Playlist: &playlist,
			Section:  model.QuickPickSectionListenAgain,
			Score:    playlistSelectionScore(candidate, options),
		})
	}
	for _, candidate := range selectedRadioSongs {
		appendSong(candidate, model.QuickPickSectionStartRadio)
	}
	// Backfill either section when the preferred quotas cannot be met. This is
	// especially useful for libraries with fewer than ten playable songs or two
	// playlists, while keeping the 4/2/6 target for healthy pools.
	for len(items) < options.Limit {
		if !selectBestSong(nil, &selectedRadioSongs, model.QuickPickSectionStartRadio, startRadioCooldown, radioCooldownEnabled) {
			break
		}
		appendSong(selectedRadioSongs[len(selectedRadioSongs)-1], model.QuickPickSectionStartRadio)
	}
	for len(items) < options.Limit {
		if !selectPlaylist(false) {
			break
		}
		candidate := selectedPlaylists[len(selectedPlaylists)-1]
		playlist := candidate.playlist
		items = append(items, model.QuickPickItem{Kind: model.QuickPickPlaylist, Playlist: &playlist, Section: model.QuickPickSectionListenAgain, Score: playlistSelectionScore(candidate, options)})
	}

	return composedQuickPick{Items: items, SeedSongs: seedSongs, SelectedTrackIDs: selectedTrackIDs}
}

func quickPickExposureCooling(exposures map[string]model.QuickPickExposureMetric, key string, now time.Time, cooldown time.Duration) bool {
	if cooldown <= 0 || key == "" || now.IsZero() {
		return false
	}
	metric, ok := exposures[key]
	if !ok || metric.LastShownAt.IsZero() {
		return false
	}
	age := now.Sub(metric.LastShownAt.UTC())
	return age >= 0 && age < cooldown
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
