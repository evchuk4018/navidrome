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
	_, err = tx.Exec(`insert into personal_radio_session
		(id, user_id, seed_media_file_id, status, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?)`, session.ID, session.UserID, session.SeedMediaFileID,
		session.Status, session.CreatedAt, session.UpdatedAt)
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

func (r *personalRadioRepository) EndActiveSessions(userID, exceptID string) error {
	_, err := r.db.Exec(`update personal_radio_session set status = ?, updated_at = ?
		where user_id = ? and status = ? and id <> ?`, model.PersonalRadioEnded, time.Now().UTC(),
		userID, model.PersonalRadioActive, exceptID)
	return err
}

func (r *personalRadioRepository) GetSessionForUser(sessionID, userID string) (*model.PersonalRadioSession, error) {
	s := &model.PersonalRadioSession{}
	err := r.db.QueryRow(`select id, user_id, seed_media_file_id, status, created_at, updated_at
		from personal_radio_session where id = ? and user_id = ?`, sessionID, userID).
		Scan(&s.ID, &s.UserID, &s.SeedMediaFileID, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
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
		 download_job_id, created_at, updated_at)
		values (?, ?, ?, ?, ?, nullif(?, ''), ?, nullif(?, ''), ?, ?)`, item.ID, item.SessionID,
		item.Position, item.ItemType, item.Status, item.MediaFileID, item.RecordingMBID,
		item.DownloadJobID, item.CreatedAt, item.UpdatedAt)
	return err
}

func (r *personalRadioRepository) UpdateItem(item *model.PersonalRadioItem) error {
	item.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(`update personal_radio_item set status = ?, media_file_id = nullif(?, ''),
		recording_mbid = ?, download_job_id = nullif(?, ''), updated_at = ? where id = ?`,
		item.Status, item.MediaFileID, item.RecordingMBID, item.DownloadJobID, item.UpdatedAt, item.ID)
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
	coalesce(personal_radio_item.download_job_id, ''), personal_radio_item.created_at,
	personal_radio_item.updated_at from personal_radio_item`

const discoverySelect = `select id, user_id, recording_mbid, coalesce(media_file_id, ''), state,
	play_starts, expires_at, created_at, updated_at from discovery_track`

type sqlScanner interface{ Scan(...any) error }

func scanRadioItem(row sqlScanner) (*model.PersonalRadioItem, error) {
	item := &model.PersonalRadioItem{}
	err := row.Scan(&item.ID, &item.SessionID, &item.Position, &item.ItemType, &item.Status,
		&item.MediaFileID, &item.RecordingMBID, &item.DownloadJobID, &item.CreatedAt, &item.UpdatedAt)
	return item, err
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
