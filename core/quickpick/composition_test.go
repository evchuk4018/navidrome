package quickpick

import (
	"testing"
	"time"

	"github.com/navidrome/navidrome/model"
)

func testSongCandidate(id string, baseScore, adjustedScore float64, rank int, liked bool, artist, album string) songCandidate {
	return songCandidate{
		song: model.MediaFile{
			ID:     id,
			Artist: artist,
			Album:  album,
		},
		baseScore:     baseScore,
		adjustedScore: adjustedScore,
		baseRank:      rank,
		fromLiked:     liked,
	}
}

func selectedSongIDs(items []model.QuickPickItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.Kind == model.QuickPickSong && item.Song != nil {
			ids = append(ids, item.Song.ID)
		}
	}
	return ids
}

func TestComposeQuickPickKeepsAnchorAndUsesExplorationBuckets(t *testing.T) {
	songs := []songCandidate{
		testSongCandidate("anchor", 10, 10, 1, false, "Anchor Artist", "Anchor Album"),
		testSongCandidate("top-exposed", 9, 1, 2, false, "Top Artist", "Top Album"),
		testSongCandidate("near", 7, 7, 8, false, "Near Artist", "Near Album"),
		testSongCandidate("deep", 6, 6, 30, false, "Deep Artist", "Deep Album"),
		testSongCandidate("liked", 5, 5, 45, true, "Liked Artist", "Liked Album"),
		testSongCandidate("fill-a", 4, 4, 3, false, "Fill A", "Fill A"),
		testSongCandidate("fill-b", 3, 3, 4, false, "Fill B", "Fill B"),
	}

	composed := composeQuickPick(songs, nil, compositionOptions{Limit: 7, Now: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)})
	ids := selectedSongIDs(composed.Items)
	if len(ids) != 7 {
		t.Fatalf("selected %d songs, want 7: %v", len(ids), ids)
	}
	if ids[0] != "anchor" {
		t.Fatalf("anchor = %q, want anchor", ids[0])
	}
	for _, want := range []string{"liked", "near", "deep"} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("selected songs %v do not contain %q", ids, want)
		}
	}
	if ids[1] == "top-exposed" {
		t.Fatalf("recently exposed non-anchor should not be selected ahead of alternatives: %v", ids)
	}
}

func TestComposeQuickPickDeduplicatesAndRelaxesBuckets(t *testing.T) {
	songs := []songCandidate{
		testSongCandidate("same", 10, 10, 1, false, "Artist", "Album"),
		testSongCandidate("same", 9, 9, 2, true, "Artist", "Album"),
		testSongCandidate("other", 8, 8, 3, false, "Other", "Other"),
	}
	composed := composeQuickPick(songs, nil, compositionOptions{Limit: 9})
	ids := selectedSongIDs(composed.Items)
	if len(ids) != 2 {
		t.Fatalf("selected IDs = %v, want both unique songs", ids)
	}
	if ids[0] != "same" || ids[1] != "other" {
		t.Fatalf("selected IDs = %v, want [same other]", ids)
	}
}

func TestComposeQuickPickPlaylistQuotaDoesNotUseSongScores(t *testing.T) {
	playlists := []playlistCandidate{
		{playlist: model.Playlist{ID: "p1"}, normalizedScore: 1, adjustedScore: 1, hasAdjustedScore: true},
		{playlist: model.Playlist{ID: "p2"}, normalizedScore: .9, adjustedScore: .9, hasAdjustedScore: true},
		{playlist: model.Playlist{ID: "p3"}, normalizedScore: .8, adjustedScore: .8, hasAdjustedScore: true},
	}
	sevenSongs := []songCandidate{
		testSongCandidate("s1", 100, 100, 1, false, "a", "a"),
		testSongCandidate("s2", 1, 1, 2, false, "b", "b"),
		testSongCandidate("s3", 1, 1, 3, false, "c", "c"),
		testSongCandidate("s4", 1, 1, 4, false, "d", "d"),
		testSongCandidate("s5", 1, 1, 5, false, "e", "e"),
		testSongCandidate("s6", 1, 1, 6, false, "f", "f"),
		testSongCandidate("s7", 1, 1, 7, false, "g", "g"),
	}
	composed := composeQuickPick(sevenSongs, playlists, compositionOptions{Limit: 9})
	playlistCount := 0
	for _, item := range composed.Items {
		if item.Kind == model.QuickPickPlaylist {
			playlistCount++
		}
	}
	if playlistCount != 2 {
		t.Fatalf("playlist count = %d, want 2", playlistCount)
	}

	fiveSongs := sevenSongs[:5]
	composed = composeQuickPick(fiveSongs, playlists, compositionOptions{Limit: 9})
	playlistCount = 0
	for _, item := range composed.Items {
		if item.Kind == model.QuickPickPlaylist {
			playlistCount++
		}
	}
	if playlistCount != 3 {
		t.Fatalf("playlist count with sparse songs = %d, want 3", playlistCount)
	}
}

func TestExposureFatigueDecaysWithoutPermanentPunishment(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	recent := exposureFatigue(model.QuickPickExposureMetric{ShowCount: 1, LastShownAt: now}, now)
	old := exposureFatigue(model.QuickPickExposureMetric{ShowCount: 1, LastShownAt: now.Add(-72 * time.Hour)}, now)
	if !(recent > old && recent > .5 && old < .2) {
		t.Fatalf("fatigue recent=%v old=%v; want strong recent penalty with decay", recent, old)
	}
}

func TestComposeQuickPickUsesListenAgainAndStartRadioQuotas(t *testing.T) {
	songs := make([]songCandidate, 0, 20)
	for i := 0; i < 20; i++ {
		songs = append(songs, testSongCandidate(
			string(rune('a'+i)),
			float64(100-i), float64(100-i), i+1, i == 1,
			"Artist "+string(rune('a'+i)), "Album "+string(rune('a'+i)),
		))
	}
	playlists := []playlistCandidate{
		{playlist: model.Playlist{ID: "p1"}, normalizedScore: 1, adjustedScore: 1, hasAdjustedScore: true},
		{playlist: model.Playlist{ID: "p2"}, normalizedScore: .9, adjustedScore: .9, hasAdjustedScore: true},
		{playlist: model.Playlist{ID: "p3"}, normalizedScore: .8, adjustedScore: .8, hasAdjustedScore: true},
	}
	composed := composeQuickPick(songs, playlists, compositionOptions{Limit: 12, Now: time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)})
	if len(composed.Items) != 12 {
		t.Fatalf("got %d items, want 12: %#v", len(composed.Items), composed.Items)
	}
	listenAgain, startRadio := 0, 0
	for _, item := range composed.Items {
		switch item.Section {
		case model.QuickPickSectionListenAgain:
			listenAgain++
		case model.QuickPickSectionStartRadio:
			startRadio++
		default:
			t.Fatalf("item %q has no recognized section", item.Kind)
		}
	}
	if listenAgain != 6 || startRadio != 6 {
		t.Fatalf("sections = listen_again:%d start_radio:%d, want 6/6", listenAgain, startRadio)
	}
	if composed.Items[0].Song == nil || composed.Items[0].Song.ID != "a" {
		t.Fatalf("stable anchor = %#v, want song a", composed.Items[0])
	}
	seenRadio := map[string]bool{}
	for _, item := range composed.Items {
		if item.Section != model.QuickPickSectionStartRadio || item.Song == nil {
			continue
		}
		if seenRadio[item.Song.ID] {
			t.Fatalf("duplicate start-radio song %q", item.Song.ID)
		}
		seenRadio[item.Song.ID] = true
	}
	if len(seenRadio) != 6 {
		t.Fatalf("start-radio songs = %d, want 6", len(seenRadio))
	}
}

func TestComposeQuickPickCooldownsNonAnchorItemsWhenAlternativesExist(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	songs := make([]songCandidate, 0, 24)
	exposures := map[string]model.QuickPickExposureMetric{}
	for i := 0; i < 24; i++ {
		id := string(rune('a' + i))
		songs = append(songs, testSongCandidate(id, float64(100-i), float64(100-i), i+1, false, id, id))
		if i > 0 && i < 12 {
			exposures[trackExposureKey(id)] = model.QuickPickExposureMetric{ItemKey: trackExposureKey(id), ShowCount: 1, LastShownAt: now.Add(-time.Hour)}
		}
	}
	composed := composeQuickPick(songs, nil, compositionOptions{Limit: 12, Now: now, Exposures: exposures})
	if composed.Items[0].Song == nil || composed.Items[0].Song.ID != "a" {
		t.Fatalf("anchor = %#v, want a", composed.Items[0])
	}
	for _, item := range composed.Items[1:] {
		if item.Song == nil {
			continue
		}
		if metric, ok := exposures[trackExposureKey(item.Song.ID)]; ok && item.Section == model.QuickPickSectionListenAgain {
			t.Fatalf("cooled listen-again song %q was selected: metric=%#v", item.Song.ID, metric)
		}
	}
}

func TestComposeQuickPickAppliesTwentyFourHourCooldownToRadioSeeds(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	songs := make([]songCandidate, 0, 20)
	exposures := map[string]model.QuickPickExposureMetric{}
	for i := 0; i < 20; i++ {
		id := string(rune('a' + i))
		songs = append(songs, testSongCandidate(id, float64(100-i), float64(100-i), i+1, false, id, id))
		if i >= 1 && i <= 12 {
			exposures[trackExposureKey(id)] = model.QuickPickExposureMetric{ItemKey: trackExposureKey(id), ShowCount: 1, LastShownAt: now.Add(-12 * time.Hour)}
		}
	}
	composed := composeQuickPick(songs, nil, compositionOptions{Limit: 12, Now: now, Exposures: exposures})
	for _, item := range composed.Items {
		if item.Section != model.QuickPickSectionStartRadio || item.Song == nil {
			continue
		}
		if _, exposed := exposures[trackExposureKey(item.Song.ID)]; exposed {
			t.Fatalf("radio seed %q was selected within 24h cooldown", item.Song.ID)
		}
	}
}

func TestComposeQuickPickBackfillsSparseSections(t *testing.T) {
	songs := []songCandidate{
		testSongCandidate("a", 10, 10, 1, false, "a", "a"),
		testSongCandidate("b", 9, 9, 2, false, "b", "b"),
	}
	playlists := make([]playlistCandidate, 0, 12)
	for i := 0; i < 12; i++ {
		id := "p" + string(rune('a'+i))
		playlists = append(playlists, playlistCandidate{playlist: model.Playlist{ID: id}, normalizedScore: float64(12 - i), adjustedScore: float64(12 - i), hasAdjustedScore: true})
	}
	composed := composeQuickPick(songs, playlists, compositionOptions{Limit: 12})
	if len(composed.Items) != 12 {
		t.Fatalf("sparse composition length = %d, want 12", len(composed.Items))
	}
	playlistCount := 0
	for _, item := range composed.Items {
		if item.Kind == model.QuickPickPlaylist {
			playlistCount++
		}
	}
	if playlistCount != 10 {
		t.Fatalf("sparse composition playlist count = %d, want 10 backfilled playlists", playlistCount)
	}
}
