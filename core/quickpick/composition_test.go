package quickpick

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/navidrome/navidrome/model"
)

func testSongCandidate(id string, baseScore, adjustedScore float64, rank int, liked bool, artist, album string) songCandidate {
	return songCandidate{
		song:      model.MediaFile{ID: id, Artist: artist, Album: album},
		baseScore: baseScore, adjustedScore: adjustedScore, baseRank: rank, fromLiked: liked,
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

func compositionSongs(count int) []songCandidate {
	songs := make([]songCandidate, count)
	for i := range songs {
		id := fmt.Sprintf("song-%02d", i)
		songs[i] = testSongCandidate(id, float64(count-i), float64(count-i), i+1, i == 1, id, id)
	}
	return songs
}

func viewOf(items []model.QuickPickItem, clicks ...string) *model.QuickPickView {
	view := &model.QuickPickView{ViewID: "previous", ClickedItemKeys: clicks}
	for _, item := range items {
		view.ItemKeys = append(view.ItemKeys, trackExposureKey(item.Song.ID))
	}
	return view
}

func TestComposeQuickPickKeepsFamiliarRowAndChurnsEveryUnclickedDiscoverySong(t *testing.T) {
	songs := compositionSongs(30)
	first := composeQuickPick(songs, compositionOptions{})
	if len(first.Items) != 12 {
		t.Fatalf("got %d items, want 12", len(first.Items))
	}
	exposures := map[string]model.QuickPickExposureMetric{}
	for _, item := range first.Items {
		exposures[trackExposureKey(item.Song.ID)] = model.QuickPickExposureMetric{LastShownAt: time.Now()}
	}
	second := composeQuickPick(songs, compositionOptions{PreviousView: viewOf(first.Items), Exposures: exposures})
	if !reflect.DeepEqual(selectedSongIDs(first.Items[:3]), selectedSongIDs(second.Items[:3])) {
		t.Fatalf("familiar row changed: first=%v second=%v", selectedSongIDs(first.Items), selectedSongIDs(second.Items))
	}
	previous := map[string]bool{}
	for index, item := range first.Items {
		previous[item.Song.ID] = true
		want := model.QuickPickSectionStartRadio
		if index < 3 {
			want = model.QuickPickSectionListenAgain
		}
		if item.Section != want {
			t.Fatalf("item %d section = %q, want %q", index, item.Section, want)
		}
	}
	for _, item := range second.Items[3:] {
		if previous[item.Song.ID] {
			t.Fatalf("unclicked discovery song %q repeated with available alternatives", item.Song.ID)
		}
	}
}

func TestComposeQuickPickCarriesOnlyClicksFromThePreviousView(t *testing.T) {
	songs := compositionSongs(40)
	first := composeQuickPick(songs, compositionOptions{})
	clicked := first.Items[6].Song.ID
	second := composeQuickPick(songs, compositionOptions{PreviousView: viewOf(first.Items, trackExposureKey(clicked))})
	assertContains := func(items []model.QuickPickItem, want bool) {
		t.Helper()
		found := false
		seen := map[string]bool{}
		for _, item := range items {
			if seen[item.Song.ID] {
				t.Fatalf("duplicate song %q", item.Song.ID)
			}
			seen[item.Song.ID] = true
			found = found || item.Song.ID == clicked
		}
		if found != want {
			t.Fatalf("song %q present=%v, want %v: %v", clicked, found, want, selectedSongIDs(items))
		}
	}
	assertContains(second.Items, true)
	third := composeQuickPick(songs, compositionOptions{PreviousView: viewOf(second.Items, trackExposureKey(clicked))})
	assertContains(third.Items, true)
	fourth := composeQuickPick(songs, compositionOptions{PreviousView: viewOf(third.Items)})
	assertContains(fourth.Items, false)

	// Clicking a familiar song cannot create a duplicate discovery tile.
	withFavoriteClick := composeQuickPick(songs, compositionOptions{PreviousView: viewOf(first.Items, trackExposureKey(first.Items[0].Song.ID))})
	assertContains(withFavoriteClick.Items, false)
}

func TestComposeQuickPickExhaustsAlternativesThenBackfillsOldestDisplays(t *testing.T) {
	songs := compositionSongs(14)
	first := composeQuickPick(songs, compositionOptions{})
	exposures := map[string]model.QuickPickExposureMetric{}
	now := time.Now()
	for i, item := range first.Items {
		exposures[trackExposureKey(item.Song.ID)] = model.QuickPickExposureMetric{LastShownAt: now.Add(time.Duration(i) * time.Hour)}
	}
	second := composeQuickPick(songs, compositionOptions{PreviousView: viewOf(first.Items), Exposures: exposures})
	if len(second.Items) != 12 {
		t.Fatalf("got %d items, want 12", len(second.Items))
	}
	previous := map[string]bool{}
	for _, item := range first.Items {
		previous[item.Song.ID] = true
	}
	for _, item := range second.Items[3:5] {
		if previous[item.Song.ID] {
			t.Fatalf("repeat %q selected before both new alternatives", item.Song.ID)
		}
	}
	if second.Items[5].Song.ID != first.Items[3].Song.ID {
		t.Fatalf("first repeat = %q, want oldest %q", second.Items[5].Song.ID, first.Items[3].Song.ID)
	}
}

func TestComposeQuickPickSkipsMissingAndDuplicateSongsInSparseLibraries(t *testing.T) {
	songs := compositionSongs(2)
	songs = append(songs, songs[0], songCandidate{song: model.MediaFile{ID: "missing", Missing: true}}, songCandidate{})
	composed := composeQuickPick(songs, compositionOptions{})
	if len(composed.Items) != 2 {
		t.Fatalf("got %v, want two unique playable songs", selectedSongIDs(composed.Items))
	}
}

func TestExposureFatigueDecaysWithoutPermanentPunishment(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	recent := exposureFatigue(model.QuickPickExposureMetric{ShowCount: 1, LastShownAt: now}, now)
	old := exposureFatigue(model.QuickPickExposureMetric{ShowCount: 1, LastShownAt: now.Add(-72 * time.Hour)}, now)
	if !(recent > old && recent > .5 && old < .2) {
		t.Fatalf("fatigue recent=%v old=%v; want strong recent penalty with decay", recent, old)
	}
}
