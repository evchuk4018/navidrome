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
	SearchSongs(context.Context, string) ([]model.ExternalTrack, error)
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
		go s.run(ctx, model.MusicDownloadOriginManual)
		go s.run(ctx, model.MusicDownloadOriginRadio)
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
		ID:          id.NewRandom(),
		UserID:      userID,
		Kind:        request.Kind,
		SourceID:    request.ID,
		Title:       request.Title,
		Artist:      request.Artist,
		Album:       request.Album,
		Status:      model.MusicDownloadQueued,
		Message:     "Queued",
		CreatedAt:   now,
		UpdatedAt:   now,
		Origin:      request.Origin,
		Priority:    request.Priority,
		RadioItemID: request.RadioItemID,
	}
	if job.Origin == "" {
		job.Origin = model.MusicDownloadOriginManual
	}
	if err := s.jobs.Create(job); err != nil {
		return nil, err
	}
	log.Info(ctx, "Music download job queued",
		"jobID", job.ID,
		"userID", userID,
		"origin", job.Origin,
		"kind", job.Kind,
		"sourceID", job.SourceID,
		"priority", job.Priority,
		"radioItemID", job.RadioItemID)
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

func (s *service) run(ctx context.Context, origin string) {
	for {
		job, err := s.jobs.ClaimNext(origin)
		if err != nil {
			log.Error(ctx, "Unable to claim music download job", "origin", origin, "error", err)
			if !waitFor(ctx, s.wake, 5*time.Second) {
				return
			}
			continue
		}
		if job != nil {
			log.Debug(ctx, "Music download job claimed",
				"jobID", job.ID,
				"userID", job.UserID,
				"origin", job.Origin,
				"kind", job.Kind,
				"sourceID", job.SourceID,
				"radioItemID", job.RadioItemID)
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
	start := time.Now()
	log.Info(ctx, "Music download job started",
		"jobID", job.ID,
		"userID", job.UserID,
		"origin", job.Origin,
		"kind", job.Kind,
		"sourceID", job.SourceID,
		"radioItemID", job.RadioItemID)
	err := s.processDownload(ctx, job)
	if err == nil {
		now := time.Now().UTC()
		job.Status = model.MusicDownloadSuccess
		job.Message = "Added to library"
		job.Error = ""
		job.FinishedAt = &now
		if updateErr := s.jobs.Update(job); updateErr != nil {
			log.Error(ctx, "Unable to mark music download job successful", "jobID", job.ID, "error", updateErr)
		}
		log.Info(ctx, "Music download job succeeded",
			"jobID", job.ID,
			"userID", job.UserID,
			"origin", job.Origin,
			"sourceID", job.SourceID,
			"title", job.Title,
			"artist", job.Artist,
			"album", job.Album,
			"completed", job.Completed,
			"total", job.Total,
			"elapsed", time.Since(start))
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
		log.Error(ctx, "Unable to update music download job", "jobID", job.ID, "error", updateErr)
	}
	if !errors.Is(err, context.Canceled) {
		log.Error(ctx, "Music download job failed",
			"jobID", job.ID,
			"userID", job.UserID,
			"origin", job.Origin,
			"sourceID", job.SourceID,
			"title", job.Title,
			"artist", job.Artist,
			"album", job.Album,
			"message", job.Message,
			"jobError", job.Error,
			"elapsed", time.Since(start),
			"error", err)
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
		stageStart := time.Now()
		log.Debug(ctx, "Music download loading recording metadata",
			"jobID", job.ID,
			"sourceID", job.SourceID,
			"origin", job.Origin)
		track, err := s.catalog.Recording(ctx, job.SourceID)
		if err != nil {
			log.Warn(ctx, "Music download recording lookup failed; searching catalog instead",
				"jobID", job.ID,
				"sourceID", job.SourceID,
				"origin", job.Origin,
				"title", job.Title,
				"artist", job.Artist,
				"error", err)
			track, err = s.resolveRecordingBySearch(ctx, job)
			if err != nil {
				return fmt.Errorf("load recording: %w", err)
			}
			log.Info(ctx, "Music download recording resolved via catalog search",
				"jobID", job.ID,
				"sourceID", job.SourceID,
				"resolvedID", track.ID,
				"title", track.Title,
				"artist", track.ArtistName,
				"album", track.AlbumTitle)
		}
		setJobTrack(job, track)
		log.Debug(ctx, "Music download recording metadata loaded",
			"jobID", job.ID,
			"sourceID", job.SourceID,
			"title", track.Title,
			"artist", track.ArtistName,
			"album", track.AlbumTitle,
			"duration", track.Duration,
			"elapsed", time.Since(stageStart))
		job.Total = 1
		if err := s.jobs.Update(job); err != nil {
			return fmt.Errorf("update download metadata: %w", err)
		}
		stageStart = time.Now()
		log.Debug(ctx, "Music download provider started",
			"jobID", job.ID,
			"sourceID", job.SourceID,
			"title", track.Title,
			"artist", track.ArtistName)
		path, err := s.downloader.Download(ctx, track, tempDir)
		if err != nil {
			return fmt.Errorf("download recording: %w", err)
		}
		log.Debug(ctx, "Music download provider completed",
			"jobID", job.ID,
			"sourceID", job.SourceID,
			"path", path,
			"elapsed", time.Since(stageStart))
		stageStart = time.Now()
		log.Debug(ctx, "Music download importer started",
			"jobID", job.ID,
			"sourceID", job.SourceID,
			"title", track.Title,
			"artist", track.ArtistName,
			"album", track.AlbumTitle)
		if err := s.tagger.Import(ctx, []string{path}, track, true); err != nil {
			return fmt.Errorf("tag recording: %w", err)
		}
		log.Debug(ctx, "Music download importer completed",
			"jobID", job.ID,
			"sourceID", job.SourceID,
			"path", path,
			"elapsed", time.Since(stageStart))
		job.Completed = 1
		return s.scanLibrary(ctx)

	case model.MusicDownloadAlbum:
		stageStart := time.Now()
		log.Debug(ctx, "Music download loading album metadata",
			"jobID", job.ID,
			"sourceID", job.SourceID,
			"origin", job.Origin)
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
		log.Debug(ctx, "Music download album metadata loaded",
			"jobID", job.ID,
			"sourceID", job.SourceID,
			"artist", job.Artist,
			"album", job.Album,
			"trackCount", job.Total,
			"elapsed", time.Since(stageStart))
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
			stageStart = time.Now()
			log.Debug(ctx, "Music download album track provider started",
				"jobID", job.ID,
				"sourceID", job.SourceID,
				"trackIndex", i+1,
				"trackCount", job.Total,
				"title", track.Title,
				"artist", track.ArtistName)
			path, err := s.downloader.Download(ctx, track, tempDir)
			if err != nil {
				return fmt.Errorf("download track %q: %w", track.Title, err)
			}
			log.Debug(ctx, "Music download album track provider completed",
				"jobID", job.ID,
				"sourceID", job.SourceID,
				"trackIndex", i+1,
				"trackCount", job.Total,
				"title", track.Title,
				"path", path,
				"elapsed", time.Since(stageStart))
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
		log.Debug(ctx, "Music download scan skipped because scanner is unavailable")
		return nil
	}
	start := time.Now()
	log.Debug(ctx, "Music download library scan started")
	warnings, err := s.scanner.ScanAll(ctx, false)
	if err != nil {
		// The files are already safely imported. A concurrent scan can pick them up
		// later, so a scan conflict is not a failed download.
		log.Warn(ctx, "Music download completed but library scan could not start",
			"warningCount", len(warnings),
			"elapsed", time.Since(start),
			"error", err)
	} else {
		log.Info(ctx, "Music download library scan completed",
			"warningCount", len(warnings),
			"warnings", warnings,
			"elapsed", time.Since(start))
	}
	return nil
}

func setJobTrack(job *model.MusicDownloadJob, track model.ExternalTrack) {
	job.Artist = track.ArtistName
	job.Album = track.AlbumTitle
	job.Title = track.Title
}

// resolveRecordingBySearch looks a recording up by artist and title when the
// recorded source ID does not exist in the catalog. Last.fm recommendations
// sometimes carry MusicBrainz IDs that are no longer valid, so the search is
// the fallback that keeps the download flowing.
func (s *service) resolveRecordingBySearch(ctx context.Context, job *model.MusicDownloadJob) (model.ExternalTrack, error) {
	query := strings.TrimSpace(strings.TrimSpace(job.Artist) + " " + strings.TrimSpace(job.Title))
	if query == "" {
		return model.ExternalTrack{}, errors.New("recording metadata is empty; cannot search")
	}
	songs, err := s.catalog.SearchSongs(ctx, query)
	if err != nil {
		return model.ExternalTrack{}, fmt.Errorf("search recording: %w", err)
	}
	if len(songs) == 0 {
		return model.ExternalTrack{}, model.ErrNotFound
	}
	title := normalizeSearchTitle(job.Title)
	artist := normalizeSearchTitle(job.Artist)
	for _, song := range songs {
		if !searchTitleMatch(title, song.Title) {
			continue
		}
		if artist == "" || normalizeSearchTitle(song.ArtistName) == artist {
			return song, nil
		}
	}
	for _, song := range songs {
		if searchTitleMatch(title, song.Title) {
			return song, nil
		}
	}
	return model.ExternalTrack{}, model.ErrNotFound
}

func normalizeSearchTitle(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// searchTitleMatch accepts an exact normalized match or a fuzzy match where
// one title fully contains the other.
func searchTitleMatch(expected, actual string) bool {
	expected = normalizeSearchTitle(expected)
	actual = normalizeSearchTitle(actual)
	if expected == "" || actual == "" {
		return false
	}
	return expected == actual ||
		strings.Contains(expected, actual) ||
		strings.Contains(actual, expected)
}

func validateSourceID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("%w: invalid external music ID", model.ErrValidation)
	}
	return nil
}

var _ Service = (*service)(nil)
