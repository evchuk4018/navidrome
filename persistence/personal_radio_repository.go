package persistence

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/navidrome/navidrome/model"
)

type personalRadioRepository struct {
	db *sql.DB
}

func NewPersonalRadioRepository(db *sql.DB) model.PersonalRadioRepository {
	return &personalRadioRepository{db: db}
}

func (r *personalRadioRepository) CreateSession(session *model.PersonalRadioSession, items []model.PersonalRadioItem) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	session.Mode = model.NormalizeRadioMode(string(session.Mode))
	_, err = tx.Exec(`insert into personal_radio_session
		(id, user_id, seed_media_file_id, mode, status, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?)`, session.ID, session.UserID, session.SeedMediaFileID,
		session.Mode, session.Status, session.CreatedAt, session.UpdatedAt)
	if err != nil {
		return err
	}
	for i := range items {
		if err := insertRadioItem(tx, &items[i]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *personalRadioRepository) UpdateSession(session *model.PersonalRadioSession) error {
	if session.Mode == "" {
		session.Mode = model.RadioModeBalanced
	}
	session.Mode = model.NormalizeRadioMode(string(session.Mode))
	session.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(`update personal_radio_session set mode = ?, status = ?, updated_at = ? where id = ? and user_id = ?`,
		session.Mode, session.Status, session.UpdatedAt, session.ID, session.UserID)
	return err
}

func (r *personalRadioRepository) EndActiveSessions(userID, exceptID string) error {
	_, err := r.db.Exec(`update personal_radio_session set status = ?, updated_at = ?
		where user_id = ? and status = ? and id <> ?`, model.PersonalRadioEnded, time.Now().UTC(),
		userID, model.PersonalRadioActive, exceptID)
	return err
}

func (r *personalRadioRepository) GetSessionForUser(sessionID, userID string) (*model.PersonalRadioSession, error) {
	s := &model.PersonalRadioSession{}
	var mode string
	err := r.db.QueryRow(`select id, user_id, seed_media_file_id, mode, status, created_at, updated_at
		from personal_radio_session where id = ? and user_id = ?`, sessionID, userID).
		Scan(&s.ID, &s.UserID, &s.SeedMediaFileID, &mode, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	s.Mode = model.RadioMode(mode)
	return s, err
}

func (r *personalRadioRepository) GetItems(sessionID string) ([]model.PersonalRadioItem, error) {
	rows, err := r.db.Query(radioItemSelect+` where session_id = ? order by position`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.PersonalRadioItem
	for rows.Next() {
		item, err := scanRadioItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *personalRadioRepository) GetItemForUser(itemID, userID string) (*model.PersonalRadioItem, error) {
	item, err := scanRadioItem(r.db.QueryRow(radioItemSelect+`
		join personal_radio_session s on s.id = personal_radio_item.session_id
		where personal_radio_item.id = ? and s.user_id = ?`, itemID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	return item, err
}

func (r *personalRadioRepository) AppendItems(sessionID string, items []model.PersonalRadioItem) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i := range items {
		items[i].SessionID = sessionID
		if err := insertRadioItem(tx, &items[i]); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`update personal_radio_session set updated_at = ? where id = ?`, time.Now().UTC(), sessionID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func insertRadioItem(tx *sql.Tx, item *model.PersonalRadioItem) error {
	_, err := tx.Exec(`insert into personal_radio_item
		(id, session_id, position, item_type, status, media_file_id, recording_mbid,
		 download_job_id, playback_outcome, listened_ms, duration_ms, transition_source_item_id,
		 transition_source_key, last_feedback_at, created_at, updated_at)
		values (?, ?, ?, ?, ?, nullif(?, ''), ?, nullif(?, ''), ?, ?, ?, nullif(?, ''), ?, ?, ?, ?)`, item.ID, item.SessionID,
		item.Position, item.ItemType, item.Status, item.MediaFileID, item.RecordingMBID,
		item.DownloadJobID, item.PlaybackOutcome, item.ListenedMS, item.DurationMS,
		item.TransitionSourceItemID, item.TransitionSourceKey, item.LastFeedbackAt,
		item.CreatedAt, item.UpdatedAt)
	return err
}

func (r *personalRadioRepository) UpdateItem(item *model.PersonalRadioItem) error {
	item.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(`update personal_radio_item set status = ?, media_file_id = nullif(?, ''),
		recording_mbid = ?, download_job_id = nullif(?, ''), playback_outcome = ?, listened_ms = ?,
		duration_ms = ?, transition_source_item_id = nullif(?, ''), transition_source_key = ?,
		last_feedback_at = ?, updated_at = ? where id = ?`, item.Status, item.MediaFileID,
		item.RecordingMBID, item.DownloadJobID, item.PlaybackOutcome, item.ListenedMS,
		item.DurationMS, item.TransitionSourceItemID, item.TransitionSourceKey,
		item.LastFeedbackAt, item.UpdatedAt, item.ID)
	return err
}

func (r *personalRadioRepository) RecentSeedIDs(sessionID string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := r.db.Query(`select media_file_id from personal_radio_item
		where session_id = ? and media_file_id is not null and status = ?
		order by position desc limit ?`, sessionID, model.RadioItemPlayed, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *personalRadioRepository) GetRecentAcceptedItems(sessionID string, limit int) ([]model.PersonalRadioItem, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := r.db.Query(radioItemSelect+` where session_id = ? and
		playback_outcome in (?, ?, ?, ?) and last_feedback_at is not null
		order by last_feedback_at desc, id desc limit ?`, sessionID,
		model.RadioPlaybackAccepted, model.RadioPlaybackCompleted,
		model.RadioPlaybackLateSkip, model.RadioPlaybackKeep, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.PersonalRadioItem
	for rows.Next() {
		item, err := scanRadioItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *personalRadioRepository) RecordPlaybackFeedback(userID, sessionID string, request model.PersonalRadioFeedbackRequest, now time.Time) (*model.RadioPlaybackFeedbackResult, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	item, err := scanRadioItem(tx.QueryRow(radioItemSelect+`
		join personal_radio_session s on s.id = personal_radio_item.session_id
		where personal_radio_item.id = ? and personal_radio_item.session_id = ? and s.user_id = ?`,
		request.ItemID, sessionID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	now = now.UTC()
	item.Status = model.RadioItemPlayed
	item.ListenedMS = maxInt64(item.ListenedMS, request.ListenedMS)
	item.DurationMS = maxInt64(item.DurationMS, request.DurationMS)
	item.LastFeedbackAt = &now

	outcome, applied, delta := radioPlaybackOutcome(*item, request, item.ListenedMS, item.DurationMS)
	if applied {
		item.PlaybackOutcome = outcome
	}

	if request.Event == model.RadioFeedbackStarted && item.TransitionSourceKey == "" {
		targetKey := model.RadioTrackKey(item.RecordingMBID, item.MediaFileID)
		if targetKey != "" {
			anchor, anchorErr := recentAcceptedItemTx(tx, sessionID, item.ID)
			if anchorErr != nil && !errors.Is(anchorErr, model.ErrNotFound) {
				return nil, anchorErr
			}
			if anchor != nil {
				sourceKey := model.RadioTrackKey(anchor.RecordingMBID, anchor.MediaFileID)
				if sourceKey != "" {
					item.TransitionSourceItemID = anchor.ID
					item.TransitionSourceKey = sourceKey
					delta.attempts++
					delta.sourceMediaFileID = anchor.MediaFileID
					delta.targetMediaFileID = item.MediaFileID
				}
			}
		}
	}

	if err := updateRadioItemTx(tx, item); err != nil {
		return nil, err
	}
	if item.TransitionSourceKey != "" {
		delta.sourceKey = item.TransitionSourceKey
		delta.targetKey = model.RadioTrackKey(item.RecordingMBID, item.MediaFileID)
		if delta.hasCounts() {
			if err := upsertRadioTransitionTx(tx, userID, delta, now); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &model.RadioPlaybackFeedbackResult{Item: *item, Applied: applied}, nil
}

func (r *personalRadioRepository) GetTransitionsForTargets(userID, sourceKey string, targetKeys []string) (map[string]model.RadioTransitionFeedback, error) {
	result := map[string]model.RadioTransitionFeedback{}
	if strings.TrimSpace(sourceKey) == "" || len(targetKeys) == 0 {
		return result, nil
	}
	keys := uniqueStrings(targetKeys)
	if len(keys) == 0 {
		return result, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	args := make([]any, 0, len(keys)+2)
	args = append(args, userID, sourceKey)
	for _, key := range keys {
		args = append(args, key)
	}
	rows, err := r.db.Query(fmt.Sprintf(`select user_id, source_key, target_key,
		coalesce(source_media_file_id, ''), coalesce(target_media_file_id, ''), attempt_count,
		accepted_count, completed_count, early_skip_count, neutral_skip_count, keep_count,
		last_attempt_at, last_positive_at, last_negative_at, updated_at
		from radio_transition_feedback where user_id = ? and source_key = ? and target_key in (%s)`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		feedback, err := scanRadioTransition(rows)
		if err != nil {
			return nil, err
		}
		result[feedback.TargetKey] = *feedback
	}
	return result, rows.Err()
}

func (r *personalRadioRepository) GetTopTransitions(userID, sourceKey string, limit int) ([]model.RadioTransitionFeedback, error) {
	if strings.TrimSpace(sourceKey) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.Query(`select user_id, source_key, target_key,
		coalesce(source_media_file_id, ''), coalesce(target_media_file_id, ''), attempt_count,
		accepted_count, completed_count, early_skip_count, neutral_skip_count, keep_count,
		last_attempt_at, last_positive_at, last_negative_at, updated_at
		from radio_transition_feedback where user_id = ? and source_key = ?
		order by (accepted_count + 1.5 * completed_count + 2.0 * keep_count -
			1.5 * early_skip_count - 0.25 * neutral_skip_count) desc, updated_at desc limit ?`,
		userID, sourceKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []model.RadioTransitionFeedback
	for rows.Next() {
		feedback, err := scanRadioTransition(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *feedback)
	}
	return result, rows.Err()
}

func (r *personalRadioRepository) UpsertDiscovery(track *model.DiscoveryTrack) error {
	_, err := r.db.Exec(`insert into discovery_track
		(id, user_id, recording_mbid, media_file_id, state, play_starts, expires_at, created_at, updated_at)
		values (?, ?, ?, nullif(?, ''), ?, ?, ?, ?, ?)
		on conflict (user_id, recording_mbid) do update set
			media_file_id = coalesce(excluded.media_file_id, discovery_track.media_file_id),
			state = case when discovery_track.state = 'kept' then discovery_track.state else excluded.state end,
			expires_at = case when discovery_track.state = 'kept' then discovery_track.expires_at else excluded.expires_at end,
			updated_at = excluded.updated_at`,
		track.ID, track.UserID, track.RecordingMBID, track.MediaFileID, track.State, track.PlayStarts,
		track.ExpiresAt, track.CreatedAt, track.UpdatedAt)
	return err
}

func (r *personalRadioRepository) GetDiscoveryByMediaFile(userID, mediaFileID string) (*model.DiscoveryTrack, error) {
	return r.getDiscovery(`where user_id = ? and media_file_id = ?`, userID, mediaFileID)
}

func (r *personalRadioRepository) GetDiscoveryByRecording(userID, recordingMBID string) (*model.DiscoveryTrack, error) {
	return r.getDiscovery(`where user_id = ? and recording_mbid = ?`, userID, recordingMBID)
}

func (r *personalRadioRepository) getDiscovery(where string, args ...any) (*model.DiscoveryTrack, error) {
	track, err := scanDiscovery(r.db.QueryRow(discoverySelect+" "+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	return track, err
}

func (r *personalRadioRepository) ListExpiredDiscoveries(now time.Time, limit int) ([]model.DiscoveryTrack, error) {
	rows, err := r.db.Query(discoverySelect+` where state = ? or
		(state = ? and expires_at is not null and expires_at <= ?)
		order by expires_at limit ?`, model.DiscoveryDeletePending, model.DiscoveryTemporary, now.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tracks []model.DiscoveryTrack
	for rows.Next() {
		track, err := scanDiscovery(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, *track)
	}
	return tracks, rows.Err()
}

func (r *personalRadioRepository) UpdateDiscovery(track *model.DiscoveryTrack) error {
	track.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(`update discovery_track set media_file_id = nullif(?, ''), state = ?,
		play_starts = ?, expires_at = ?, updated_at = ? where id = ?`, track.MediaFileID, track.State,
		track.PlayStarts, track.ExpiresAt, track.UpdatedAt, track.ID)
	return err
}

func (r *personalRadioRepository) RecordFeedback(userID, recordingMBID, event string, now time.Time) error {
	positive, completed, neutral, early := 0, 0, 0, 0
	var earlyAt any
	switch event {
	case model.RadioFeedbackThresholdReached, model.RadioFeedbackKeep:
		positive = 1
	case model.RadioFeedbackCompleted:
		positive, completed = 1, 1
	case model.RadioFeedbackManualSkip:
		early, earlyAt = 1, now.UTC()
	default:
		neutral = 1
	}
	_, err := r.db.Exec(`insert into radio_track_feedback
		(user_id, recording_mbid, positive_count, completed_count, neutral_skip_count,
		 early_skip_count, last_early_skip_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)
		on conflict (user_id, recording_mbid) do update set
			positive_count = positive_count + excluded.positive_count,
			completed_count = completed_count + excluded.completed_count,
			neutral_skip_count = neutral_skip_count + excluded.neutral_skip_count,
			early_skip_count = early_skip_count + excluded.early_skip_count,
			last_early_skip_at = coalesce(excluded.last_early_skip_at, last_early_skip_at),
			updated_at = excluded.updated_at`, userID, recordingMBID, positive, completed, neutral,
		early, earlyAt, now.UTC())
	return err
}

func (r *personalRadioRepository) GetFeedback(userID string, recordingMBIDs []string) (map[string]model.RadioTrackFeedback, error) {
	result := map[string]model.RadioTrackFeedback{}
	if len(recordingMBIDs) == 0 {
		return result, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(recordingMBIDs)), ",")
	args := make([]any, 0, len(recordingMBIDs)+1)
	args = append(args, userID)
	for _, id := range recordingMBIDs {
		args = append(args, id)
	}
	rows, err := r.db.Query(fmt.Sprintf(`select user_id, recording_mbid, positive_count, completed_count,
		neutral_skip_count, early_skip_count, last_early_skip_at, updated_at
		from radio_track_feedback where user_id = ? and recording_mbid in (%s)`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f model.RadioTrackFeedback
		var earlyAt sql.NullTime
		if err := rows.Scan(&f.UserID, &f.RecordingMBID, &f.PositiveCount, &f.CompletedCount,
			&f.NeutralSkipCount, &f.EarlySkipCount, &earlyAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if earlyAt.Valid {
			f.LastEarlySkipAt = &earlyAt.Time
		}
		result[f.RecordingMBID] = f
	}
	return result, rows.Err()
}

func (r *personalRadioRepository) IsMediaFileProtected(mediaFileID string) (bool, error) {
	var protected int
	err := r.db.QueryRow(`select case when
		exists(select 1 from annotation where item_type = 'media_file' and item_id = ? and starred = 1)
		or exists(select 1 from playlist_tracks where media_file_id = ?)
		or exists(select 1 from discovery_track where media_file_id = ? and state = ?)
		then 1 else 0 end`, mediaFileID, mediaFileID, mediaFileID, model.DiscoveryKept).Scan(&protected)
	return protected == 1, err
}

const radioItemSelect = `select personal_radio_item.id, personal_radio_item.session_id,
	personal_radio_item.position, personal_radio_item.item_type, personal_radio_item.status,
	coalesce(personal_radio_item.media_file_id, ''), personal_radio_item.recording_mbid,
	coalesce(personal_radio_item.download_job_id, ''), personal_radio_item.playback_outcome,
	personal_radio_item.listened_ms, personal_radio_item.duration_ms,
	coalesce(personal_radio_item.transition_source_item_id, ''), personal_radio_item.transition_source_key,
	personal_radio_item.last_feedback_at, personal_radio_item.created_at,
	personal_radio_item.updated_at from personal_radio_item`

const discoverySelect = `select id, user_id, recording_mbid, coalesce(media_file_id, ''), state,
	play_starts, expires_at, created_at, updated_at from discovery_track`

type sqlScanner interface{ Scan(...any) error }

func scanRadioItem(row sqlScanner) (*model.PersonalRadioItem, error) {
	item := &model.PersonalRadioItem{}
	var lastFeedbackAt sql.NullTime
	err := row.Scan(&item.ID, &item.SessionID, &item.Position, &item.ItemType, &item.Status,
		&item.MediaFileID, &item.RecordingMBID, &item.DownloadJobID, &item.PlaybackOutcome,
		&item.ListenedMS, &item.DurationMS, &item.TransitionSourceItemID,
		&item.TransitionSourceKey, &lastFeedbackAt, &item.CreatedAt, &item.UpdatedAt)
	if lastFeedbackAt.Valid {
		item.LastFeedbackAt = &lastFeedbackAt.Time
	}
	return item, err
}

type radioTransitionDelta struct {
	sourceKey         string
	targetKey         string
	sourceMediaFileID string
	targetMediaFileID string
	attempts          int
	accepted          int
	completed         int
	earlySkip         int
	neutralSkip       int
	keep              int
}

func (d radioTransitionDelta) hasCounts() bool {
	return d.attempts != 0 || d.accepted != 0 || d.completed != 0 ||
		d.earlySkip != 0 || d.neutralSkip != 0 || d.keep != 0
}

func updateRadioItemTx(tx *sql.Tx, item *model.PersonalRadioItem) error {
	item.UpdatedAt = time.Now().UTC()
	_, err := tx.Exec(`update personal_radio_item set status = ?, media_file_id = nullif(?, ''),
		recording_mbid = ?, download_job_id = nullif(?, ''), playback_outcome = ?, listened_ms = ?,
		duration_ms = ?, transition_source_item_id = nullif(?, ''), transition_source_key = ?,
		last_feedback_at = ?, updated_at = ? where id = ?`, item.Status, item.MediaFileID,
		item.RecordingMBID, item.DownloadJobID, item.PlaybackOutcome, item.ListenedMS,
		item.DurationMS, item.TransitionSourceItemID, item.TransitionSourceKey,
		item.LastFeedbackAt, item.UpdatedAt, item.ID)
	return err
}

func recentAcceptedItemTx(tx *sql.Tx, sessionID, excludeItemID string) (*model.PersonalRadioItem, error) {
	item, err := scanRadioItem(tx.QueryRow(radioItemSelect+` where session_id = ? and id <> ? and
		playback_outcome in (?, ?, ?, ?) and last_feedback_at is not null
		order by last_feedback_at desc, id desc limit 1`, sessionID, excludeItemID,
		model.RadioPlaybackAccepted, model.RadioPlaybackCompleted,
		model.RadioPlaybackLateSkip, model.RadioPlaybackKeep))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	return item, err
}

func radioPlaybackOutcome(item model.PersonalRadioItem, request model.PersonalRadioFeedbackRequest, listenedMS, durationMS int64) (string, bool, radioTransitionDelta) {
	delta := radioTransitionDelta{}
	current := item.PlaybackOutcome
	switch request.Event {
	case model.RadioFeedbackStarted:
		if current == "" {
			return model.RadioPlaybackStarted, true, delta
		}
	case model.RadioFeedbackThresholdReached:
		if playbackOutcomeRank(current) < playbackOutcomeRank(model.RadioPlaybackAccepted) {
			delta.accepted = 1
			return model.RadioPlaybackAccepted, true, delta
		}
	case model.RadioFeedbackCompleted:
		if current != model.RadioPlaybackCompleted {
			delta.completed = 1
			return model.RadioPlaybackCompleted, true, delta
		}
	case model.RadioFeedbackManualSkip:
		if current == model.RadioPlaybackCompleted || current == model.RadioPlaybackKeep || current == model.RadioPlaybackLateSkip {
			return current, false, delta
		}
		thresholdMS := earlySkipThresholdMSForDuration(durationMS)
		if model.IsAcceptedRadioPlaybackOutcome(current) || listenedMS >= thresholdMS {
			delta.neutralSkip = 1
			return model.RadioPlaybackLateSkip, true, delta
		}
		if current != model.RadioPlaybackEarlySkip {
			delta.earlySkip = 1
			return model.RadioPlaybackEarlySkip, true, delta
		}
	case model.RadioFeedbackKeep:
		if current == model.RadioPlaybackCompleted || current == model.RadioPlaybackKeep {
			return current, false, delta
		}
		delta.keep = 1
		return model.RadioPlaybackKeep, true, delta
	}
	return current, false, delta
}

func playbackOutcomeRank(outcome string) int {
	switch outcome {
	case model.RadioPlaybackStarted:
		return 1
	case model.RadioPlaybackEarlySkip:
		return 2
	case model.RadioPlaybackAccepted:
		return 3
	case model.RadioPlaybackLateSkip, model.RadioPlaybackKeep:
		return 4
	case model.RadioPlaybackCompleted:
		return 5
	default:
		return 0
	}
}

func earlySkipThresholdMSForDuration(durationMS int64) int64 {
	if durationMS <= 0 {
		return 30000
	}
	return minInt64(30000, durationMS/5)
}

func upsertRadioTransitionTx(tx *sql.Tx, userID string, delta radioTransitionDelta, now time.Time) error {
	var lastAttemptAt, lastPositiveAt, lastNegativeAt any
	if delta.attempts > 0 {
		lastAttemptAt = now
	}
	if delta.accepted > 0 || delta.completed > 0 || delta.keep > 0 {
		lastPositiveAt = now
	}
	if delta.earlySkip > 0 || delta.neutralSkip > 0 {
		lastNegativeAt = now
	}
	_, err := tx.Exec(`insert into radio_transition_feedback
		(user_id, source_key, target_key, source_media_file_id, target_media_file_id,
		 attempt_count, accepted_count, completed_count, early_skip_count, neutral_skip_count,
		 keep_count, last_attempt_at, last_positive_at, last_negative_at, updated_at)
		values (?, ?, ?, nullif(?, ''), nullif(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict (user_id, source_key, target_key) do update set
		 source_media_file_id = coalesce(excluded.source_media_file_id, radio_transition_feedback.source_media_file_id),
		 target_media_file_id = coalesce(excluded.target_media_file_id, radio_transition_feedback.target_media_file_id),
		 attempt_count = radio_transition_feedback.attempt_count + excluded.attempt_count,
		 accepted_count = radio_transition_feedback.accepted_count + excluded.accepted_count,
		 completed_count = radio_transition_feedback.completed_count + excluded.completed_count,
		 early_skip_count = radio_transition_feedback.early_skip_count + excluded.early_skip_count,
		 neutral_skip_count = radio_transition_feedback.neutral_skip_count + excluded.neutral_skip_count,
		 keep_count = radio_transition_feedback.keep_count + excluded.keep_count,
		 last_attempt_at = coalesce(excluded.last_attempt_at, radio_transition_feedback.last_attempt_at),
		 last_positive_at = coalesce(excluded.last_positive_at, radio_transition_feedback.last_positive_at),
		 last_negative_at = coalesce(excluded.last_negative_at, radio_transition_feedback.last_negative_at),
		 updated_at = excluded.updated_at`, userID, delta.sourceKey, delta.targetKey,
		delta.sourceMediaFileID, delta.targetMediaFileID, delta.attempts, delta.accepted,
		delta.completed, delta.earlySkip, delta.neutralSkip, delta.keep, lastAttemptAt,
		lastPositiveAt, lastNegativeAt, now)
	return err
}

func scanRadioTransition(row sqlScanner) (*model.RadioTransitionFeedback, error) {
	feedback := &model.RadioTransitionFeedback{}
	var lastAttemptAt, lastPositiveAt, lastNegativeAt sql.NullTime
	err := row.Scan(&feedback.UserID, &feedback.SourceKey, &feedback.TargetKey,
		&feedback.SourceMediaFileID, &feedback.TargetMediaFileID, &feedback.AttemptCount,
		&feedback.AcceptedCount, &feedback.CompletedCount, &feedback.EarlySkipCount,
		&feedback.NeutralSkipCount, &feedback.KeepCount, &lastAttemptAt, &lastPositiveAt,
		&lastNegativeAt, &feedback.UpdatedAt)
	if lastAttemptAt.Valid {
		feedback.LastAttemptAt = &lastAttemptAt.Time
	}
	if lastPositiveAt.Valid {
		feedback.LastPositiveAt = &lastPositiveAt.Time
	}
	if lastNegativeAt.Valid {
		feedback.LastNegativeAt = &lastNegativeAt.Time
	}
	return feedback, err
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func scanDiscovery(row sqlScanner) (*model.DiscoveryTrack, error) {
	track := &model.DiscoveryTrack{}
	var expiry sql.NullTime
	err := row.Scan(&track.ID, &track.UserID, &track.RecordingMBID, &track.MediaFileID, &track.State,
		&track.PlayStarts, &expiry, &track.CreatedAt, &track.UpdatedAt)
	if expiry.Valid {
		track.ExpiresAt = &expiry.Time
	}
	return track, err
}

var _ model.PersonalRadioRepository = (*personalRadioRepository)(nil)
