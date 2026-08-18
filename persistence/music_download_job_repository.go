package persistence

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/navidrome/navidrome/model"
)

type musicDownloadJobRepository struct {
	db *sql.DB
}

func NewMusicDownloadJobRepository(db *sql.DB) model.MusicDownloadJobRepository {
	return &musicDownloadJobRepository{db: db}
}

func (r *musicDownloadJobRepository) Create(job *model.MusicDownloadJob) error {
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	_, err := r.db.Exec(`
		insert into music_download_job
		(id, user_id, kind, source_id, artist, album, title, status, message, error,
		 output_path, completed, total, created_at, updated_at, started_at, finished_at,
		 origin, priority, radio_item_id, media_file_id)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.UserID, job.Kind, job.SourceID, job.Artist, job.Album, job.Title,
		job.Status, job.Message, job.Error, job.OutputPath, job.Completed, job.Total,
		job.CreatedAt, job.UpdatedAt, job.StartedAt, job.FinishedAt,
		job.Origin, job.Priority, job.RadioItemID, job.MediaFileID)
	return err
}

func (r *musicDownloadJobRepository) Get(id string) (*model.MusicDownloadJob, error) {
	row := r.db.QueryRow(musicDownloadJobSelect+" where id = ?", id)
	job, err := scanMusicDownloadJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}

func (r *musicDownloadJobRepository) GetForUser(id, userID string) (*model.MusicDownloadJob, error) {
	row := r.db.QueryRow(musicDownloadJobSelect+" where id = ? and user_id = ?", id, userID)
	job, err := scanMusicDownloadJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return job, nil
}

// GetAllForUser returns the download jobs for a user. A limit <= 0 returns every
// job for the user (used by admin seeding); otherwise at most the most recent
// `limit` jobs are returned.
func (r *musicDownloadJobRepository) GetAllForUser(userID string, limit int) ([]model.MusicDownloadJob, error) {
	query := musicDownloadJobSelect + " where user_id = ? order by created_at desc"
	args := []any{userID}
	if limit > 0 {
		query += " limit ?"
		args = append(args, limit)
	}
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]model.MusicDownloadJob, 0)
	for rows.Next() {
		job, err := scanMusicDownloadJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *musicDownloadJobRepository) ClaimNext(origins ...string) (*model.MusicDownloadJob, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	query := musicDownloadJobSelect + " where status = ?"
	args := []any{model.MusicDownloadQueued}
	if len(origins) > 0 {
		query += " and origin in (" + strings.TrimSuffix(strings.Repeat("?,", len(origins)), ",") + ")"
		for _, origin := range origins {
			args = append(args, origin)
		}
	}
	query += " order by priority desc, created_at asc limit 1"
	job, err := scanMusicDownloadJob(tx.QueryRow(query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	result, err := tx.Exec(`
		update music_download_job
		set status = ?, started_at = ?, updated_at = ?, message = ?, error = ''
		where id = ? and status = ?`,
		model.MusicDownloadRunning, now, now, "Downloading", job.ID, model.MusicDownloadQueued)
	if err != nil {
		return nil, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if updated != 1 {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	job.Status = model.MusicDownloadRunning
	job.StartedAt = &now
	job.UpdatedAt = now
	job.Message = "Downloading"
	job.Error = ""
	return job, nil
}

func (r *musicDownloadJobRepository) Update(job *model.MusicDownloadJob) error {
	job.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(`
		update music_download_job set
		user_id = ?, kind = ?, source_id = ?, artist = ?, album = ?, title = ?,
		status = ?, message = ?, error = ?, output_path = ?, completed = ?, total = ?,
		created_at = ?, updated_at = ?, started_at = ?, finished_at = ?, origin = ?,
		priority = ?, radio_item_id = ?, media_file_id = ?
		where id = ?`,
		job.UserID, job.Kind, job.SourceID, job.Artist, job.Album, job.Title,
		job.Status, job.Message, job.Error, job.OutputPath, job.Completed, job.Total,
		job.CreatedAt, job.UpdatedAt, job.StartedAt, job.FinishedAt, job.Origin,
		job.Priority, job.RadioItemID, job.MediaFileID, job.ID)
	return err
}

func (r *musicDownloadJobRepository) RequeueRunning() error {
	_, err := r.db.Exec(`
		update music_download_job
		set status = ?, started_at = null, updated_at = ?, message = ?, error = ''
		where status = ?`,
		model.MusicDownloadQueued, time.Now().UTC(), "Recovered after restart", model.MusicDownloadRunning)
	return err
}

const musicDownloadJobSelect = `
	select id, user_id, kind, source_id, artist, album, title, status, message, error,
		output_path, completed, total, created_at, updated_at, started_at, finished_at,
		origin, priority, radio_item_id, media_file_id
	from music_download_job`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMusicDownloadJob(row rowScanner) (*model.MusicDownloadJob, error) {
	job := &model.MusicDownloadJob{}
	var startedAt, finishedAt sql.NullTime
	err := row.Scan(
		&job.ID, &job.UserID, &job.Kind, &job.SourceID, &job.Artist, &job.Album, &job.Title,
		&job.Status, &job.Message, &job.Error, &job.OutputPath, &job.Completed, &job.Total,
		&job.CreatedAt, &job.UpdatedAt, &startedAt, &finishedAt,
		&job.Origin, &job.Priority, &job.RadioItemID, &job.MediaFileID,
	)
	if err != nil {
		return nil, err
	}
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		job.FinishedAt = &finishedAt.Time
	}
	return job, nil
}
