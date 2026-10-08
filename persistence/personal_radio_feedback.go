package persistence

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/navidrome/navidrome/db"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

const radioFeedbackBudget = 2 * time.Second

var radioFeedbackRetryDelays = [...]time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}

type radioFeedbackSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type radioFeedbackBusyError struct{ cause error }

func (e *radioFeedbackBusyError) Error() string   { return "feedback database is busy; retry shortly" }
func (e *radioFeedbackBusyError) Unwrap() []error { return []error{model.ErrNotAvailable, e.cause} }

func retryableRadioFeedbackError(err error) bool {
	code, extended, ok := db.ErrorCodes(err)
	return ok && (code == int(sqlite3.ErrBusy) || extended == int(sqlite3.ErrLockedSharedCache))
}

func (r *personalRadioRepository) RecordPlaybackFeedback(parent context.Context, userID, sessionID string, request model.PersonalRadioFeedbackRequest, now time.Time) (result *model.RadioPlaybackFeedbackResult, err error) {
	if len(request.EventID) > 255 {
		return nil, fmt.Errorf("%w: event ID exceeds 255 characters", model.ErrValidation)
	}
	ctx, cancel := context.WithTimeout(parent, radioFeedbackBudget)
	defer cancel()
	attempts := 0
	defer func() {
		if parent.Err() == nil && errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
			err = &radioFeedbackBusyError{cause: err}
		}
		if code, extended, ok := db.ErrorCodes(err); ok {
			log.Error(parent, "Personal radio feedback database operation failed", "sessionID", sessionID,
				"itemID", request.ItemID, "eventID", request.EventID, "attempts", attempts,
				"sqliteCode", code, "sqliteExtended", extended, "error", err)
		}
	}()
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var busyTimeout int
	if err = conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return nil, err
	}
	// Restore the connection's policy even when the request was cancelled. A
	// connection whose cleanup fails must never be returned to the shared pool.
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(parent), time.Second)
		defer cleanupCancel()
		if _, restoreErr := conn.ExecContext(cleanupCtx, fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeout)); restoreErr != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if err == nil {
				result, err = nil, fmt.Errorf("restore feedback connection: %w", restoreErr)
			}
		}
	}()
	if _, err = conn.ExecContext(ctx, "PRAGMA busy_timeout=250"); err != nil {
		return nil, err
	}
	for attempts = 1; attempts <= len(radioFeedbackRetryDelays)+1; attempts++ {
		result, err = r.recordPlaybackFeedbackAttempt(ctx, conn, userID, sessionID, request, now.UTC())
		if err == nil {
			if attempts > 1 {
				log.Info(parent, "Personal radio feedback recovered after database contention", "sessionID", sessionID,
					"itemID", request.ItemID, "eventID", request.EventID, "attempts", attempts)
			}
			return result, nil
		}
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		if !retryableRadioFeedbackError(err) {
			return nil, err
		}
		if attempts > len(radioFeedbackRetryDelays) || ctx.Err() != nil {
			return nil, &radioFeedbackBusyError{cause: err}
		}
		timer := time.NewTimer(radioFeedbackRetryDelays[attempts-1])
		select {
		case <-ctx.Done():
			timer.Stop()
			if parent.Err() != nil {
				return nil, parent.Err()
			}
			return nil, &radioFeedbackBusyError{cause: err}
		case <-timer.C:
		}
	}
	return nil, &radioFeedbackBusyError{cause: err}
}

func (r *personalRadioRepository) recordPlaybackFeedbackAttempt(ctx context.Context, conn *sql.Conn, userID, sessionID string, request model.PersonalRadioFeedbackRequest, now time.Time) (result *model.RadioPlaybackFeedbackResult, err error) {
	committed := false
	begun := false
	defer func() {
		if committed {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		// BEGIN can race cancellation, so attempt rollback even if BEGIN reported
		// an error. "No transaction active" is expected when no lock was obtained.
		_, rollbackErr := conn.ExecContext(cleanupCtx, "ROLLBACK")
		if rollbackErr != nil && begun {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			err = fmt.Errorf("rollback feedback transaction: %w", rollbackErr)
		}
	}()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	begun = true
	result, err = r.applyPlaybackFeedback(ctx, conn, userID, sessionID, request, now)
	if err != nil {
		return nil, err
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

func (r *personalRadioRepository) applyPlaybackFeedback(ctx context.Context, conn radioFeedbackSQL, userID, sessionID string, request model.PersonalRadioFeedbackRequest, now time.Time) (*model.RadioPlaybackFeedbackResult, error) {
	item, err := scanRadioItem(conn.QueryRowContext(ctx, radioItemSelect+`
		join personal_radio_session s on s.id = personal_radio_item.session_id
		where personal_radio_item.id = ? and personal_radio_item.session_id = ? and s.user_id = ?`, request.ItemID, sessionID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if request.EventID != "" {
		var previous model.PersonalRadioFeedbackRequest
		var previousSession string
		err = conn.QueryRowContext(ctx, `select session_id, item_id, event, listened_ms, duration_ms
			from radio_feedback_event where user_id = ? and event_id = ?`, userID, request.EventID).
			Scan(&previousSession, &previous.ItemID, &previous.Event, &previous.ListenedMS, &previous.DurationMS)
		if err == nil {
			previous.EventID = request.EventID
			if previousSession != sessionID || previous != request {
				return nil, model.ErrFeedbackEventConflict
			}
			return &model.RadioPlaybackFeedbackResult{Item: *item, Duplicate: true}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	item.Status = model.RadioItemPlayed
	item.ListenedMS = maxInt64(item.ListenedMS, request.ListenedMS)
	item.DurationMS = maxInt64(item.DurationMS, request.DurationMS)
	item.LastFeedbackAt = &now
	outcome, applied, delta := radioPlaybackOutcome(*item, request, item.ListenedMS, item.DurationMS)
	if applied {
		item.PlaybackOutcome = outcome
	}
	if request.Event == model.RadioFeedbackStarted && item.TransitionSourceKey == "" {
		if targetKey := model.RadioTrackKey(item.RecordingMBID, item.MediaFileID); targetKey != "" {
			anchor, anchorErr := recentAcceptedItemTx(ctx, conn, sessionID, item.ID)
			if anchorErr != nil && !errors.Is(anchorErr, model.ErrNotFound) {
				return nil, anchorErr
			}
			if anchor != nil {
				if sourceKey := model.RadioTrackKey(anchor.RecordingMBID, anchor.MediaFileID); sourceKey != "" {
					item.TransitionSourceItemID, item.TransitionSourceKey = anchor.ID, sourceKey
					delta.attempts++
					delta.sourceMediaFileID, delta.targetMediaFileID = anchor.MediaFileID, item.MediaFileID
				}
			}
		}
	}
	if err := updateRadioItemTx(ctx, conn, item); err != nil {
		return nil, err
	}
	if item.TransitionSourceKey != "" && delta.hasCounts() {
		delta.sourceKey, delta.targetKey = item.TransitionSourceKey, model.RadioTrackKey(item.RecordingMBID, item.MediaFileID)
		if err := upsertRadioTransitionTx(ctx, conn, userID, delta, now); err != nil {
			return nil, err
		}
	}
	result := &model.RadioPlaybackFeedbackResult{Item: *item, Applied: applied}
	mbid := model.NormalizeRecordingMBID(item.RecordingMBID)
	if mbid != "" && applied {
		if event := radioRecordingFeedbackEvent(item.PlaybackOutcome); event != "" {
			if err := recordRadioTrackFeedback(ctx, conn, userID, mbid, event, now); err != nil {
				return nil, err
			}
		}
	}
	if item.ItemType == model.RadioItemDiscovery && mbid != "" {
		discovery, err := scanDiscovery(conn.QueryRowContext(ctx, discoverySelect+` where user_id = ? and recording_mbid = ?`, userID, mbid))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		update := true
		switch request.Event {
		case model.RadioFeedbackStarted:
			discovery.PlayStarts++
			if discovery.PlayStarts > 1 {
				discovery.State, discovery.ExpiresAt = model.DiscoveryKept, nil
				if err := recordRadioTrackFeedback(ctx, conn, userID, mbid, model.RadioFeedbackKeep, now); err != nil {
					return nil, err
				}
			}
		case model.RadioFeedbackThresholdReached, model.RadioFeedbackCompleted, model.RadioFeedbackKeep:
			update = applied
			if applied {
				discovery.State, discovery.ExpiresAt = model.DiscoveryKept, nil
			}
		case model.RadioFeedbackManualSkip:
			update = applied
			if applied {
				if item.PlaybackOutcome == model.RadioPlaybackEarlySkip {
					discovery.State = model.DiscoveryDeletePending
					result.DiscoveryToDelete = discovery
				} else {
					discovery.State, discovery.ExpiresAt = model.DiscoveryKept, nil
				}
			}
		}
		if update {
			_, err := conn.ExecContext(ctx, `update discovery_track set media_file_id = nullif(?, ''), state = ?,
				play_starts = ?, expires_at = ?, updated_at = ? where id = ?`, discovery.MediaFileID,
				discovery.State, discovery.PlayStarts, discovery.ExpiresAt, now, discovery.ID)
			if err != nil {
				return nil, err
			}
		}
	}
	if request.EventID != "" {
		_, err = conn.ExecContext(ctx, `insert into radio_feedback_event
			(user_id, event_id, session_id, item_id, event, listened_ms, duration_ms, created_at)
			values (?, ?, ?, ?, ?, ?, ?, ?)`, userID, request.EventID, sessionID, request.ItemID,
			request.Event, request.ListenedMS, request.DurationMS, now)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func radioRecordingFeedbackEvent(outcome string) string {
	switch outcome {
	case model.RadioPlaybackAccepted:
		return model.RadioFeedbackThresholdReached
	case model.RadioPlaybackCompleted:
		return model.RadioFeedbackCompleted
	case model.RadioPlaybackEarlySkip:
		return model.RadioFeedbackManualSkip
	case model.RadioPlaybackLateSkip:
		return "neutral"
	case model.RadioPlaybackKeep:
		return model.RadioFeedbackKeep
	default:
		return ""
	}
}
