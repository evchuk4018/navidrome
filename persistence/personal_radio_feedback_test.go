package persistence

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/navidrome/navidrome/model"
)

func feedbackTestRepository(t *testing.T) (*personalRadioRepository, *sql.DB) {
	t.Helper()
	_, testFile, _, _ := runtime.Caller(0)
	database, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "radio.db")+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=3000")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.Exec(`
		create table user (id text primary key);
		insert into user values ('user');
		create table media_file (id text primary key);
		insert into media_file values ('seed'), ('seed-file'), ('next-file'), ('discovery-file');
		create table music_download_job (id text primary key);
		create table playlist (id text primary key);`); err != nil {
		t.Fatal(err)
	}
	for _, migration := range []string{"20260813010000_add_quick_pick_personal_radio.sql", "20260827010000_add_personal_radio_context.sql", "20261007220000_add_radio_feedback_events.sql"} {
		source, err := os.ReadFile(filepath.Join(filepath.Dir(testFile), "..", "db", "migrations", migration))
		if err != nil {
			t.Fatal(err)
		}
		up := strings.Split(string(source), "-- +goose Down")[0]
		up = strings.Split(up, "alter table music_download_job")[0]
		if _, err = database.Exec(up); err != nil {
			t.Fatalf("migration %s: %v", migration, err)
		}
	}
	repo := &personalRadioRepository{db: database}
	now := time.Now().UTC()
	session := &model.PersonalRadioSession{ID: "session", UserID: "user", SeedMediaFileID: "seed", Status: model.PersonalRadioActive, CreatedAt: now, UpdatedAt: now}
	items := []model.PersonalRadioItem{
		{ID: "seed", Position: 0, ItemType: model.RadioItemSeed, Status: model.RadioItemReady, MediaFileID: "seed-file", RecordingMBID: "seed-mbid", CreatedAt: now, UpdatedAt: now},
		{ID: "next", Position: 1, ItemType: model.RadioItemLibrary, Status: model.RadioItemReady, MediaFileID: "next-file", RecordingMBID: "next-mbid", CreatedAt: now, UpdatedAt: now},
		{ID: "discovery", Position: 2, ItemType: model.RadioItemDiscovery, Status: model.RadioItemReady, MediaFileID: "discovery-file", RecordingMBID: "discovery-mbid", CreatedAt: now, UpdatedAt: now},
	}
	for i := range items {
		items[i].SessionID = session.ID
	}
	if err := repo.CreateSession(session, items); err != nil {
		t.Fatal(err)
	}
	expires := now.Add(time.Hour)
	if err := repo.UpsertDiscovery(&model.DiscoveryTrack{ID: "discovery-track", UserID: "user", RecordingMBID: "discovery-mbid", MediaFileID: "discovery-file", State: model.DiscoveryTemporary, ExpiresAt: &expires, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return repo, database
}

func recordTestFeedback(t *testing.T, repo *personalRadioRepository, item, event, eventID string) *model.RadioPlaybackFeedbackResult {
	t.Helper()
	result, err := repo.RecordPlaybackFeedback(context.Background(), "user", "session", model.PersonalRadioFeedbackRequest{ItemID: item, Event: event, EventID: eventID, DurationMS: 100000}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRadioFeedbackReceiptsAndAtomicCounters(t *testing.T) {
	repo, database := feedbackTestRepository(t)
	recordTestFeedback(t, repo, "seed", model.RadioFeedbackCompleted, "seed-complete")
	recordTestFeedback(t, repo, "next", model.RadioFeedbackStarted, "next-start")
	recordTestFeedback(t, repo, "next", model.RadioFeedbackCompleted, "next-complete")
	before, _ := repo.GetItemForUser("next", "user")
	if result := recordTestFeedback(t, repo, "next", model.RadioFeedbackCompleted, "next-complete"); !result.Duplicate || result.Applied {
		t.Fatalf("duplicate = %#v", result)
	}
	after, _ := repo.GetItemForUser("next", "user")
	if !after.LastFeedbackAt.Equal(*before.LastFeedbackAt) {
		t.Fatal("duplicate changed feedback ordering")
	}
	feedback, err := repo.GetFeedback("user", []string{"next-mbid"})
	if err != nil || feedback["next-mbid"].CompletedCount != 1 || feedback["next-mbid"].PositiveCount != 1 {
		t.Fatalf("feedback = %#v, %v", feedback, err)
	}
	transitions, err := repo.GetTransitionsForTargets("user", "mbid:seed-mbid", []string{"mbid:next-mbid"})
	if err != nil || transitions["mbid:next-mbid"].AttemptCount != 1 || transitions["mbid:next-mbid"].CompletedCount != 1 {
		t.Fatalf("transitions = %#v, %v", transitions, err)
	}
	_, err = repo.RecordPlaybackFeedback(context.Background(), "user", "session", model.PersonalRadioFeedbackRequest{ItemID: "next", Event: model.RadioFeedbackManualSkip, EventID: "next-complete", DurationMS: 100000}, time.Now())
	if !errors.Is(err, model.ErrFeedbackEventConflict) {
		t.Fatalf("conflict = %v", err)
	}
	_, err = repo.RecordPlaybackFeedback(context.Background(), "another-user", "session", model.PersonalRadioFeedbackRequest{ItemID: "next", Event: model.RadioFeedbackStarted, EventID: "next-start"}, time.Now())
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("ownership = %v", err)
	}
	if _, err := database.Exec("delete from personal_radio_session where id = 'session'"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow("select count(*) from radio_feedback_event").Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade count = %d, %v", count, err)
	}
}

func TestRadioFeedbackDiscoveryRetriesAndLegacyReplays(t *testing.T) {
	repo, _ := feedbackTestRepository(t)
	recordTestFeedback(t, repo, "discovery", model.RadioFeedbackStarted, "start-1")
	recordTestFeedback(t, repo, "discovery", model.RadioFeedbackStarted, "start-1")
	track, _ := repo.GetDiscoveryByRecording("user", "discovery-mbid")
	if track.PlayStarts != 1 || track.State != model.DiscoveryTemporary {
		t.Fatalf("duplicate start: %#v", track)
	}
	recordTestFeedback(t, repo, "discovery", model.RadioFeedbackStarted, "start-2")
	recordTestFeedback(t, repo, "discovery", model.RadioFeedbackStarted, "start-2")
	track, _ = repo.GetDiscoveryByRecording("user", "discovery-mbid")
	if track.PlayStarts != 2 || track.State != model.DiscoveryKept || track.ExpiresAt != nil {
		t.Fatalf("replay: %#v", track)
	}
	recordTestFeedback(t, repo, "discovery", model.RadioFeedbackStarted, "")
	recordTestFeedback(t, repo, "discovery", model.RadioFeedbackStarted, "")
	track, _ = repo.GetDiscoveryByRecording("user", "discovery-mbid")
	if track.PlayStarts != 4 {
		t.Fatalf("legacy starts = %d", track.PlayStarts)
	}
}

func TestRadioFeedbackRollsBackEveryWriteOnFailure(t *testing.T) {
	for _, table := range []string{"radio_track_feedback", "discovery_track", "radio_feedback_event"} {
		t.Run(table, func(t *testing.T) {
			repo, database := feedbackTestRepository(t)
			recordTestFeedback(t, repo, "seed", model.RadioFeedbackCompleted, "anchor")
			recordTestFeedback(t, repo, "discovery", model.RadioFeedbackStarted, "start")
			operation := "insert"
			if table == "discovery_track" {
				operation = "update"
			}
			if _, err := database.Exec("create trigger reject_feedback before " + operation + " on " + table + " begin select raise(abort, 'injected feedback failure'); end"); err != nil {
				t.Fatal(err)
			}
			_, err := repo.RecordPlaybackFeedback(context.Background(), "user", "session", model.PersonalRadioFeedbackRequest{ItemID: "discovery", Event: model.RadioFeedbackCompleted, EventID: "failed"}, time.Now())
			if err == nil {
				t.Fatal("expected injected failure")
			}
			item, _ := repo.GetItemForUser("discovery", "user")
			track, _ := repo.GetDiscoveryByRecording("user", "discovery-mbid")
			feedback, _ := repo.GetFeedback("user", []string{"discovery-mbid"})
			var count int
			_ = database.QueryRow("select count(*) from radio_feedback_event").Scan(&count)
			transitions, transitionErr := repo.GetTransitionsForTargets("user", "mbid:seed-mbid", []string{"mbid:discovery-mbid"})
			if transitionErr != nil || transitions["mbid:discovery-mbid"].AttemptCount != 1 || transitions["mbid:discovery-mbid"].CompletedCount != 0 {
				t.Fatalf("partial transition update: %#v, %v", transitions, transitionErr)
			}
			if item.PlaybackOutcome != model.RadioPlaybackStarted || track.State != model.DiscoveryTemporary || len(feedback) != 0 || count != 2 {
				t.Fatalf("partial commit: item=%#v track=%#v feedback=%#v receipts=%d", item, track, feedback, count)
			}
			if _, err := database.Exec("drop trigger reject_feedback"); err != nil {
				t.Fatal(err)
			}
			recordTestFeedback(t, repo, "discovery", model.RadioFeedbackCompleted, "failed")
		})
	}
}

func TestRadioFeedbackConcurrentDuplicates(t *testing.T) {
	repo, _ := feedbackTestRepository(t)
	var wg sync.WaitGroup
	errorsOut := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.RecordPlaybackFeedback(context.Background(), "user", "session", model.PersonalRadioFeedbackRequest{ItemID: "discovery", Event: model.RadioFeedbackStarted, EventID: "same-start"}, time.Now())
			errorsOut <- err
		}()
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	track, _ := repo.GetDiscoveryByRecording("user", "discovery-mbid")
	if track.PlayStarts != 1 {
		t.Fatalf("starts = %d", track.PlayStarts)
	}
}

func TestRadioFeedbackContentionAndConnectionCleanup(t *testing.T) {
	for _, mode := range []string{"clears", "persists", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			repo, database := feedbackTestRepository(t)
			writer, err := database.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			if _, err := writer.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
				t.Fatal(err)
			}
			defer writer.ExecContext(context.Background(), "ROLLBACK")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := repo.RecordPlaybackFeedback(ctx, "user", "session", model.PersonalRadioFeedbackRequest{ItemID: "seed", Event: model.RadioFeedbackCompleted, EventID: "contended"}, time.Now())
				finished <- err
			}()
			start := time.Now()
			if mode == "clears" {
				time.Sleep(300 * time.Millisecond)
				if _, err := writer.ExecContext(context.Background(), "COMMIT"); err != nil {
					t.Fatal(err)
				}
			} else if mode == "cancelled" {
				deadline := time.Now().Add(time.Second)
				for database.Stats().InUse < 2 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if database.Stats().InUse < 2 {
					t.Fatal("feedback did not reserve its connection")
				}
				cancel()
			}
			err = <-finished
			if mode == "clears" && err != nil {
				t.Fatal(err)
			}
			if mode == "persists" && !errors.Is(err, model.ErrNotAvailable) {
				t.Fatalf("busy = %v", err)
			}
			if mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel = %v", err)
			}
			if time.Since(start) > radioFeedbackBudget+500*time.Millisecond {
				t.Fatal("feedback exceeded retry budget")
			}
			conn, err := database.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var timeout int
			if err := conn.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 3000 {
				t.Fatalf("busy timeout leaked: %d, %v", timeout, err)
			}
			if _, err := writer.ExecContext(context.Background(), "ROLLBACK"); err != nil && mode != "clears" {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
				t.Fatalf("transaction leaked: %v", err)
			}
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		})
	}
}

func TestRadioFeedbackRetryClassifier(t *testing.T) {
	for _, test := range []struct {
		err   error
		retry bool
	}{
		{sqlite3.Error{Code: sqlite3.ErrBusy, ExtendedCode: sqlite3.ErrBusySnapshot}, true},
		{sqlite3.Error{Code: sqlite3.ErrLocked, ExtendedCode: sqlite3.ErrLockedSharedCache}, true},
		{sqlite3.Error{Code: sqlite3.ErrLocked}, false},
		{sqlite3.Error{Code: sqlite3.ErrConstraint}, false},
	} {
		if retryableRadioFeedbackError(test.err) != test.retry {
			t.Fatalf("classification: %v", test.err)
		}
	}
}

func TestRadioFeedbackDeletionScheduledOnlyForCommittedEvent(t *testing.T) {
	repo, _ := feedbackTestRepository(t)
	first := recordTestFeedback(t, repo, "discovery", model.RadioFeedbackManualSkip, "skip")
	if first.DiscoveryToDelete == nil || first.DiscoveryToDelete.State != model.DiscoveryDeletePending {
		t.Fatalf("cleanup = %#v", first)
	}
	duplicate := recordTestFeedback(t, repo, "discovery", model.RadioFeedbackManualSkip, "skip")
	if duplicate.DiscoveryToDelete != nil {
		t.Fatal("duplicate scheduled cleanup")
	}
}
