package persistence

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestQuickPickMetricsParsesSQLiteTextTimestamps(t *testing.T) {
	database, err := sql.Open("sqlite3", "file:quick-pick-metrics-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	_, err = database.Exec(`
		create table playlist_play_history (
			user_id text not null,
			playlist_id text not null,
			played_at datetime not null
		);
		insert into playlist_play_history (user_id, playlist_id, played_at)
		values ('user', 'playlist', '2026-08-13 21:53:18.123456789+00:00');
	`)
	if err != nil {
		t.Fatal(err)
	}

	repository := &quickPickMetricsRepository{db: database}
	metrics, err := repository.PlaylistMetrics("user", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	lastPlayed := metrics["playlist"].LastPlayed
	if lastPlayed == nil {
		t.Fatal("expected last played timestamp")
	}
	want := time.Date(2026, 8, 13, 21, 53, 18, 123456789, time.UTC)
	if !lastPlayed.Equal(want) {
		t.Fatalf("got last played %s, want %s", lastPlayed, want)
	}
}

func newQuickPickExposureTestRepository(t *testing.T) *quickPickMetricsRepository {
	t.Helper()
	database, err := sql.Open("sqlite3", "file:quick-pick-exposure-test?mode=memory&cache=shared&_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.Exec(`
		create table quick_pick_exposure (
			user_id text not null,
			item_key text not null,
			show_count integer not null default 0,
			last_shown_at datetime not null,
			primary key (user_id, item_key)
		);
		create index quick_pick_exposure_user_last_shown
			on quick_pick_exposure (user_id, last_shown_at desc);`)
	if err != nil {
		t.Fatal(err)
	}
	return &quickPickMetricsRepository{db: database}
}

func TestQuickPickExposureMetricsRoundTripAndDeduplicate(t *testing.T) {
	repository := newQuickPickExposureTestRepository(t)
	first := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	if err := repository.RecordExposures("user-a", []string{"track:a", "track:a", "playlist:p"}, first); err != nil {
		t.Fatal(err)
	}
	if err := repository.RecordExposures("user-a", []string{"track:a"}, second); err != nil {
		t.Fatal(err)
	}

	metrics, err := repository.ExposureMetrics("user-a", []string{"track:a", "track:a", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 || metrics["track:a"].ShowCount != 2 {
		t.Fatalf("metrics = %#v, want only track:a with count 2", metrics)
	}
	if !metrics["track:a"].LastShownAt.Equal(second) {
		t.Fatalf("last shown = %s, want %s", metrics["track:a"].LastShownAt, second)
	}

	otherUser, err := repository.ExposureMetrics("user-b", []string{"track:a", "playlist:p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherUser) != 0 {
		t.Fatalf("other user metrics = %#v, want empty", otherUser)
	}
}

func TestQuickPickExposureMetricsEmptyKeysAreNoOps(t *testing.T) {
	repository := newQuickPickExposureTestRepository(t)
	if err := repository.RecordExposures("user", nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	metrics, err := repository.ExposureMetrics("user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 0 {
		t.Fatalf("metrics = %#v, want empty", metrics)
	}
}
