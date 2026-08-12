package music

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

type Catalog interface {
	Search(context.Context, string) (model.ExternalMusicSearch, error)
	Artist(context.Context, string) (model.ExternalArtistDetails, error)
	Album(context.Context, string) (model.ExternalAlbumDetails, error)
	Recording(context.Context, string) (model.ExternalTrack, error)
}

type Downloader interface {
	Download(context.Context, model.ExternalTrack, string) (string, error)
}

type Tagger interface {
	Import(context.Context, []string, model.ExternalTrack, bool) error
}

type Service interface {
	Start(context.Context)
	Search(context.Context, string) (model.ExternalMusicSearch, error)
	Artist(context.Context, string) (model.ExternalArtistDetails, error)
	Album(context.Context, string) (model.ExternalAlbumDetails, error)
	CreateDownload(context.Context, string, model.ExternalDownloadRequest) (*model.MusicDownloadJob, error)
	GetDownload(context.Context, string, string) (*model.MusicDownloadJob, error)
	ListDownloads(context.Context, string, int) ([]model.MusicDownloadJob, error)
}

type service struct {
	catalog    Catalog
	downloader Downloader
	tagger     Tagger
	jobs       model.MusicDownloadJobRepository
	scanner    model.Scanner
	wake       chan struct{}
	startOnce  sync.Once
}

func New(catalog Catalog, downloader Downloader, tagger Tagger, jobs model.MusicDownloadJobRepository, scanner model.Scanner) Service {
	return &service{
		catalog:    catalog,
		downloader: downloader,
		tagger:     tagger,
		jobs:       jobs,
		scanner:    scanner,
		wake:       make(chan struct{}, 1),
	}
}

func (s *service) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.startOnce.Do(func() {
		if err := s.jobs.RequeueRunning(); err != nil {
			log.Error(ctx, "Unable to recover music download jobs", err)
		}
		go s.run(ctx)
	})
}

func (s *service) Search(ctx context.Context, query string) (model.ExternalMusicSearch, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 200 {
		return model.ExternalMusicSearch{}, fmt.Errorf("%w: search query must be between 1 and 200 characters", model.ErrValidation)
	}
	return s.catalog.Search(ctx, query)
}

func (s *service) Artist(ctx context.Context, artistID string) (model.ExternalArtistDetails, error) {
	if err := validateSourceID(artistID); err != nil {
		return model.ExternalArtistDetails{}, err
	}
	return s.catalog.Artist(ctx, artistID)
}

func (s *service) Album(ctx context.Context, albumID string) (model.ExternalAlbumDetails, error) {
	if err := validateSourceID(albumID); err != nil {
		return model.ExternalAlbumDetails{}, err
	}
	return s.catalog.Album(ctx, albumID)
}

func (s *service) CreateDownload(ctx context.Context, userID string, request model.ExternalDownloadRequest) (*model.MusicDownloadJob, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: user is required", model.ErrValidation)
	}
	if request.Kind != model.MusicDownloadSong && request.Kind != model.MusicDownloadAlbum {
		return nil, fmt.Errorf("%w: unsupported download kind", model.ErrValidation)
	}
	if err := validateSourceID(request.ID); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	job := &model.MusicDownloadJob{
		ID:        id.NewRandom(),
		UserID:    userID,
		Kind:      request.Kind,
		SourceID:  request.ID,
		Status:    model.MusicDownloadQueued,
		Message:   "Queued",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.jobs.Create(job); err != nil {
		return nil, err
	}
	s.signal()
	return job, nil
}

func (s *service) GetDownload(_ context.Context, userID, jobID string) (*model.MusicDownloadJob, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(jobID) == "" {
		return nil, model.ErrNotFound
	}
	return s.jobs.GetForUser(jobID, userID)
}

func (s *service) ListDownloads(_ context.Context, userID string, limit int) ([]model.MusicDownloadJob, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("%w: user is required", model.ErrValidation)
	}
	return s.jobs.GetAllForUser(userID, limit)
}

func (s *service) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *service) run(ctx context.Context) {
	for {
		job, err := s.jobs.ClaimNext()
		if err != nil {
			log.Error(ctx, "Unable to claim music download job", err)
			if !waitFor(ctx, s.wake, 5*time.Second) {
				return
			}
			continue
		}
		if job != nil {
			s.process(ctx, job)
			continue
		}
		if !waitFor(ctx, s.wake, 2*time.Second) {
			return
		}
	}
}

func waitFor(ctx context.Context, wake <-chan struct{}, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-timer.C:
		return true
	}
}

func (s *service) process(ctx context.Context, job *model.MusicDownloadJob) {
	err := s.processDownload(ctx, job)
	if err == nil {
		now := time.Now().UTC()
		job.Status = model.MusicDownloadSuccess
		job.Message = "Added to library"
		job.Error = ""
		job.FinishedAt = &now
		if updateErr := s.jobs.Update(job); updateErr != nil {
			log.Error(ctx, "Unable to mark music download job successful", "jobID", job.ID, updateErr)
		}
		return
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		job.Status = model.MusicDownloadQueued
		job.Message = "Interrupted; will retry"
		job.Error = ""
		job.StartedAt = nil
	} else {
		now := time.Now().UTC()
		job.Status = model.MusicDownloadFailed
		job.Message = "Download failed"
		job.Error = err.Error()
		job.FinishedAt = &now
	}
	if updateErr := s.jobs.Update(job); updateErr != nil {
		log.Error(ctx, "Unable to update music download job", "jobID", job.ID, updateErr)
	}
	if !errors.Is(err, context.Canceled) {
		log.Error(ctx, "Music download job failed", "jobID", job.ID, err)
	}
}

func (s *service) processDownload(ctx context.Context, job *model.MusicDownloadJob) error {
	if s.catalog == nil || s.downloader == nil || s.tagger == nil {
		return fmt.Errorf("music download provider is not configured")
	}

	cacheDir := conf.Server.CacheFolder.String()
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("create download cache: %w", err)
	}
	tempDir, err := os.MkdirTemp(cacheDir, "music-download-")
	if err != nil {
		return fmt.Errorf("create download workspace: %w", err)
	}
	defer os.RemoveAll(tempDir) // The directory is an isolated, generated workspace.

	switch job.Kind {
	case model.MusicDownloadSong:
		track, err := s.catalog.Recording(ctx, job.SourceID)
		if err != nil {
			return fmt.Errorf("load recording: %w", err)
		}
		setJobTrack(job, track)
		job.Total = 1
		if err := s.jobs.Update(job); err != nil {
			return fmt.Errorf("update download metadata: %w", err)
		}
		path, err := s.downloader.Download(ctx, track, tempDir)
		if err != nil {
			return fmt.Errorf("download recording: %w", err)
		}
		if err := s.tagger.Import(ctx, []string{path}, track, true); err != nil {
			return fmt.Errorf("tag recording: %w", err)
		}
		job.Completed = 1
		return s.scanLibrary(ctx)

	case model.MusicDownloadAlbum:
		album, err := s.catalog.Album(ctx, job.SourceID)
		if err != nil {
			return fmt.Errorf("load album: %w", err)
		}
		if len(album.Tracks) == 0 {
			return fmt.Errorf("album contains no tracks")
		}
		job.Artist = album.Album.ArtistName
		job.Album = album.Album.Title
		job.Total = len(album.Tracks)
		if err := s.jobs.Update(job); err != nil {
			return fmt.Errorf("update album metadata: %w", err)
		}

		files := make([]string, 0, len(album.Tracks))
		for i, track := range album.Tracks {
			if track.ArtistName == "" {
				track.ArtistName = album.Album.ArtistName
			}
			if track.AlbumTitle == "" {
				track.AlbumTitle = album.Album.Title
			}
			if track.AlbumID == "" {
				track.AlbumID = album.Album.ID
			}
			path, err := s.downloader.Download(ctx, track, tempDir)
			if err != nil {
				return fmt.Errorf("download track %q: %w", track.Title, err)
			}
			files = append(files, path)
			job.Completed = i + 1
			job.Message = fmt.Sprintf("Downloaded %d of %d tracks", job.Completed, job.Total)
			if err := s.jobs.Update(job); err != nil {
				return fmt.Errorf("update album progress: %w", err)
			}
		}
		metadata := model.ExternalTrack{
			ArtistName: album.Album.ArtistName,
			AlbumID:    album.Album.ID,
			AlbumTitle: album.Album.Title,
			Year:       album.Album.Year,
		}
		if err := s.tagger.Import(ctx, files, metadata, false); err != nil {
			return fmt.Errorf("tag album: %w", err)
		}
		return s.scanLibrary(ctx)
	default:
		return fmt.Errorf("unsupported download kind %q", job.Kind)
	}
}

func (s *service) scanLibrary(ctx context.Context) error {
	if s.scanner == nil {
		return nil
	}
	if _, err := s.scanner.ScanAll(ctx, false); err != nil {
		// The files are already safely imported. A concurrent scan can pick them up
		// later, so a scan conflict is not a failed download.
		log.Warn(ctx, "Music download completed but library scan could not start", err)
	}
	return nil
}

func setJobTrack(job *model.MusicDownloadJob, track model.ExternalTrack) {
	job.Artist = track.ArtistName
	job.Album = track.AlbumTitle
	job.Title = track.Title
}

func validateSourceID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("%w: invalid external music ID", model.ErrValidation)
	}
	return nil
}

var _ Service = (*service)(nil)
