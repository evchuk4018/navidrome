package model

import "time"

// ExternalMusicSearch contains results returned by an external music catalog.
// These types intentionally do not depend on the web UI or on a provider SDK.
type ExternalMusicSearch struct {
	Results         []ExternalSearchHit `json:"results"`
	Partial         bool                `json:"partial,omitempty"`
	DegradedSources []string            `json:"degradedSources,omitempty"`

	Artists []ExternalArtist `json:"artists"`
	Albums  []ExternalAlbum  `json:"albums"`
	Songs   []ExternalTrack  `json:"songs"`
	Genres  []ExternalGenre  `json:"genres"`
}

// ExternalSearchHit is one item in the server-ranked mixed search stream.
// Exactly one entity pointer is populated for each hit.
type ExternalSearchHit struct {
	Kind   string          `json:"kind"`
	Artist *ExternalArtist `json:"artist,omitempty"`
	Album  *ExternalAlbum  `json:"album,omitempty"`
	Song   *ExternalTrack  `json:"song,omitempty"`
	Genre  *ExternalGenre  `json:"genre,omitempty"`
}

type ExternalArtist struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	SortName       string   `json:"sortName,omitempty"`
	Country        string   `json:"country,omitempty"`
	Disambiguation string   `json:"disambiguation,omitempty"`
	Type           string   `json:"type,omitempty"`
	ImageURL       string   `json:"imageUrl,omitempty"`
	Aliases        []string `json:"-"`
	ProviderScore  float64  `json:"-"`
	Popularity     float64  `json:"-"`
}

type ExternalAlbum struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	ArtistID      string   `json:"artistId,omitempty"`
	ArtistName    string   `json:"artistName,omitempty"`
	ReleaseDate   string   `json:"releaseDate,omitempty"`
	Year          int      `json:"year,omitempty"`
	Type          string   `json:"type,omitempty"`
	TrackCount    int      `json:"trackCount,omitempty"`
	ImageURL      string   `json:"imageUrl,omitempty"`
	ArtworkURLs   []string `json:"artworkUrls,omitempty"`
	ProviderScore float64  `json:"-"`
	Popularity    float64  `json:"-"`
}

type ExternalTrack struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	ArtistID      string   `json:"artistId,omitempty"`
	ArtistName    string   `json:"artistName,omitempty"`
	AlbumID       string   `json:"albumId,omitempty"`
	AlbumTitle    string   `json:"albumTitle,omitempty"`
	ReleaseDate   string   `json:"releaseDate,omitempty"`
	Year          int      `json:"year,omitempty"`
	Duration      int      `json:"duration,omitempty"`
	TrackNumber   int      `json:"trackNumber,omitempty"`
	DiscNumber    int      `json:"discNumber,omitempty"`
	Genre         string   `json:"genre,omitempty"`
	ImageURL      string   `json:"imageUrl,omitempty"`
	ArtworkURLs   []string `json:"artworkUrls,omitempty"`
	ISRCs         []string `json:"isrcs,omitempty"`
	Video         bool     `json:"video,omitempty"`
	Version       string   `json:"version,omitempty"`
	ProviderScore float64  `json:"-"`
	Popularity    float64  `json:"-"`
}

type ExternalGenre struct {
	Name          string  `json:"name"`
	ProviderScore float64 `json:"-"`
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
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	Origin      string `json:"-"`
	Priority    int    `json:"-"`
	RadioItemID string `json:"-"`
	Title       string `json:"-"`
	Artist      string `json:"-"`
	Album       string `json:"-"`
}

const (
	MusicDownloadSong  = "song"
	MusicDownloadAlbum = "album"

	MusicDownloadQueued  = "queued"
	MusicDownloadRunning = "running"
	MusicDownloadSuccess = "succeeded"
	MusicDownloadFailed  = "failed"

	MusicDownloadOriginManual = "manual"
	MusicDownloadOriginRadio  = "radio"
)

type MusicDownloadJob struct {
	ID          string     `json:"id" db:"id"`
	UserID      string     `json:"userId" db:"user_id"`
	Kind        string     `json:"kind" db:"kind"`
	SourceID    string     `json:"sourceId" db:"source_id"`
	Artist      string     `json:"artist,omitempty" db:"artist"`
	Album       string     `json:"album,omitempty" db:"album"`
	Title       string     `json:"title,omitempty" db:"title"`
	Status      string     `json:"status" db:"status"`
	Message     string     `json:"message,omitempty" db:"message"`
	Error       string     `json:"error,omitempty" db:"error"`
	OutputPath  string     `json:"outputPath,omitempty" db:"output_path"`
	Completed   int        `json:"completed" db:"completed"`
	Total       int        `json:"total" db:"total"`
	CreatedAt   time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time  `json:"updatedAt" db:"updated_at"`
	StartedAt   *time.Time `json:"startedAt,omitempty" db:"started_at"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty" db:"finished_at"`
	Origin      string     `json:"origin,omitempty" db:"origin"`
	Priority    int        `json:"priority,omitempty" db:"priority"`
	RadioItemID string     `json:"-" db:"radio_item_id"`
	MediaFileID string     `json:"mediaFileId,omitempty" db:"media_file_id"`
}

type MusicDownloadJobRepository interface {
	Create(*MusicDownloadJob) error
	Get(id string) (*MusicDownloadJob, error)
	GetForUser(id, userID string) (*MusicDownloadJob, error)
	GetAllForUser(userID string, limit int) ([]MusicDownloadJob, error)
	ClaimNext(origins ...string) (*MusicDownloadJob, error)
	Update(*MusicDownloadJob) error
	RequeueRunning() error
}
