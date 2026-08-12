package model

import "time"

// ExternalMusicSearch contains results returned by an external music catalog.
// These types intentionally do not depend on the web UI or on a provider SDK.
type ExternalMusicSearch struct {
	Artists []ExternalArtist `json:"artists"`
	Albums  []ExternalAlbum  `json:"albums"`
	Songs   []ExternalTrack  `json:"songs"`
	Genres  []ExternalGenre  `json:"genres"`
}

type ExternalArtist struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SortName       string `json:"sortName,omitempty"`
	Country        string `json:"country,omitempty"`
	Disambiguation string `json:"disambiguation,omitempty"`
	Type           string `json:"type,omitempty"`
	ImageURL       string `json:"imageUrl,omitempty"`
}

type ExternalAlbum struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	ArtistID    string `json:"artistId,omitempty"`
	ArtistName  string `json:"artistName,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	Year        int    `json:"year,omitempty"`
	Type        string `json:"type,omitempty"`
	TrackCount  int    `json:"trackCount,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
}

type ExternalTrack struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	ArtistID    string `json:"artistId,omitempty"`
	ArtistName  string `json:"artistName,omitempty"`
	AlbumID     string `json:"albumId,omitempty"`
	AlbumTitle  string `json:"albumTitle,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	Year        int    `json:"year,omitempty"`
	Duration    int    `json:"duration,omitempty"`
	TrackNumber int    `json:"trackNumber,omitempty"`
	DiscNumber  int    `json:"discNumber,omitempty"`
	Genre       string `json:"genre,omitempty"`
	ImageURL    string `json:"imageUrl,omitempty"`
}

type ExternalGenre struct {
	Name string `json:"name"`
}

type ExternalArtistDetails struct {
	Artist ExternalArtist  `json:"artist"`
	Albums []ExternalAlbum `json:"albums"`
}

type ExternalAlbumDetails struct {
	Album  ExternalAlbum   `json:"album"`
	Tracks []ExternalTrack `json:"tracks"`
}

type ExternalDownloadRequest struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

const (
	MusicDownloadSong  = "song"
	MusicDownloadAlbum = "album"

	MusicDownloadQueued  = "queued"
	MusicDownloadRunning = "running"
	MusicDownloadSuccess = "succeeded"
	MusicDownloadFailed  = "failed"
)

type MusicDownloadJob struct {
	ID         string     `json:"id" db:"id"`
	UserID     string     `json:"userId" db:"user_id"`
	Kind       string     `json:"kind" db:"kind"`
	SourceID   string     `json:"sourceId" db:"source_id"`
	Artist     string     `json:"artist,omitempty" db:"artist"`
	Album      string     `json:"album,omitempty" db:"album"`
	Title      string     `json:"title,omitempty" db:"title"`
	Status     string     `json:"status" db:"status"`
	Message    string     `json:"message,omitempty" db:"message"`
	Error      string     `json:"error,omitempty" db:"error"`
	OutputPath string     `json:"outputPath,omitempty" db:"output_path"`
	Completed  int        `json:"completed" db:"completed"`
	Total      int        `json:"total" db:"total"`
	CreatedAt  time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt  time.Time  `json:"updatedAt" db:"updated_at"`
	StartedAt  *time.Time `json:"startedAt,omitempty" db:"started_at"`
	FinishedAt *time.Time `json:"finishedAt,omitempty" db:"finished_at"`
}

type MusicDownloadJobRepository interface {
	Create(*MusicDownloadJob) error
	Get(id string) (*MusicDownloadJob, error)
	GetForUser(id, userID string) (*MusicDownloadJob, error)
	GetAllForUser(userID string, limit int) ([]MusicDownloadJob, error)
	ClaimNext() (*MusicDownloadJob, error)
	Update(*MusicDownloadJob) error
	RequeueRunning() error
}
