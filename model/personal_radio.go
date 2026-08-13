package model

import "time"

const (
	PersonalRadioActive         = "active"
	PersonalRadioEnded          = "ended"
	RadioItemSeed               = "seed"
	RadioItemLibrary            = "library"
	RadioItemDiscovery          = "discovery"
	RadioItemReady              = "ready"
	RadioItemDownloading        = "downloading"
	RadioItemFailed             = "failed"
	RadioItemPlayed             = "played"
	RadioPlanningSelecting      = "selecting"
	RadioPlanningDownloading    = "downloading"
	RadioPlanningWaitingForScan = "waiting_for_scan"
	RadioPlanningReady          = "ready"
	RadioPlanningNoDiscovery    = "no_discovery"
	DiscoveryTemporary          = "temporary"
	DiscoveryKept               = "kept"
	DiscoveryDeletePending      = "delete_pending"
	DiscoveryDeleted            = "deleted"
)

type PersonalRadioSession struct {
	ID              string    `json:"id"`
	UserID          string    `json:"-"`
	SeedMediaFileID string    `json:"seedMediaFileId"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type PersonalRadioItem struct {
	ID            string     `json:"id"`
	SessionID     string     `json:"-"`
	Position      int        `json:"position"`
	ItemType      string     `json:"type"`
	Status        string     `json:"status"`
	MediaFileID   string     `json:"mediaFileId,omitempty"`
	RecordingMBID string     `json:"recordingMbid,omitempty"`
	DownloadJobID string     `json:"-"`
	Song          *MediaFile `json:"song,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type PersonalRadioSessionResponse struct {
	Session        PersonalRadioSession `json:"session"`
	Items          []PersonalRadioItem  `json:"items"`
	Pending        bool                 `json:"pending"`
	PlanningStatus string               `json:"planningStatus"`
}

type CreatePersonalRadioRequest struct {
	SeedMediaFileID string `json:"seedMediaFileId"`
}

type RefillPersonalRadioRequest struct {
	CurrentItemID string   `json:"currentItemId,omitempty"`
	QueuedItemIDs []string `json:"queuedItemIds,omitempty"`
}

const (
	RadioFeedbackStarted          = "started"
	RadioFeedbackThresholdReached = "threshold_reached"
	RadioFeedbackCompleted        = "completed"
	RadioFeedbackManualSkip       = "manual_skip"
	RadioFeedbackKeep             = "keep"
)

type PersonalRadioFeedbackRequest struct {
	ItemID     string `json:"itemId"`
	Event      string `json:"event"`
	ListenedMS int64  `json:"listenedMs"`
	DurationMS int64  `json:"durationMs"`
}

type DiscoveryTrack struct {
	ID            string
	UserID        string
	RecordingMBID string
	MediaFileID   string
	State         string
	PlayStarts    int
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type RadioTrackFeedback struct {
	UserID           string
	RecordingMBID    string
	PositiveCount    int
	CompletedCount   int
	NeutralSkipCount int
	EarlySkipCount   int
	LastEarlySkipAt  *time.Time
	UpdatedAt        time.Time
}

type PersonalRadioRepository interface {
	CreateSession(*PersonalRadioSession, []PersonalRadioItem) error
	EndActiveSessions(userID string, exceptID string) error
	GetSessionForUser(sessionID, userID string) (*PersonalRadioSession, error)
	GetItems(sessionID string) ([]PersonalRadioItem, error)
	GetItemForUser(itemID, userID string) (*PersonalRadioItem, error)
	AppendItems(sessionID string, items []PersonalRadioItem) error
	UpdateItem(*PersonalRadioItem) error
	RecentSeedIDs(sessionID string, limit int) ([]string, error)
	UpsertDiscovery(*DiscoveryTrack) error
	GetDiscoveryByMediaFile(userID, mediaFileID string) (*DiscoveryTrack, error)
	GetDiscoveryByRecording(userID, recordingMBID string) (*DiscoveryTrack, error)
	ListExpiredDiscoveries(now time.Time, limit int) ([]DiscoveryTrack, error)
	UpdateDiscovery(*DiscoveryTrack) error
	RecordFeedback(userID, recordingMBID, event string, now time.Time) error
	GetFeedback(userID string, recordingMBIDs []string) (map[string]RadioTrackFeedback, error)
	IsMediaFileProtected(mediaFileID string) (bool, error)
}
