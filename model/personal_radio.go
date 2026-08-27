package model

import (
	"strings"
	"time"
)

const (
	PersonalRadioActive         = "active"
	PersonalRadioEnded          = "ended"
	RadioItemSeed               = "seed"
	RadioItemLibrary            = "library"
	RadioItemDiscovery          = "discovery"
	RadioItemReady              = "ready"
	RadioItemHeld               = "held"
	RadioItemDownloading        = "downloading"
	RadioItemFailed             = "failed"
	RadioItemPlayed             = "played"
	RadioPlaybackStarted        = "started"
	RadioPlaybackAccepted       = "accepted"
	RadioPlaybackCompleted      = "completed"
	RadioPlaybackEarlySkip      = "early_skip"
	RadioPlaybackLateSkip       = "late_skip"
	RadioPlaybackKeep           = "keep"
	RadioModeFamiliar           = "familiar"
	RadioModeBalanced           = "balanced"
	RadioModeDiscover           = "discover"
	RadioPlanningSelecting      = "selecting"
	RadioPlanningDownloading    = "downloading"
	RadioPlanningWaitingForScan = "waiting_for_scan"
	RadioPlanningRetrying       = "retrying"
	RadioPlanningReady          = "ready"
	RadioPlanningNoDiscovery    = "no_discovery"
	DiscoveryTemporary          = "temporary"
	DiscoveryKept               = "kept"
	DiscoveryDeletePending      = "delete_pending"
	DiscoveryDeleted            = "deleted"
)

// RadioMode is the small, persisted tuning domain for Personal Radio.
type RadioMode string

type RadioTuning struct {
	Mode RadioMode `json:"mode"`
}

type PersonalRadioSession struct {
	ID              string    `json:"id"`
	UserID          string    `json:"-"`
	SeedMediaFileID string    `json:"seedMediaFileId"`
	Mode            RadioMode `json:"mode"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type PersonalRadioItem struct {
	ID                     string     `json:"id"`
	SessionID              string     `json:"-"`
	Position               int        `json:"position"`
	ItemType               string     `json:"type"`
	Status                 string     `json:"status"`
	MediaFileID            string     `json:"mediaFileId,omitempty"`
	RecordingMBID          string     `json:"recordingMbid,omitempty"`
	DownloadJobID          string     `json:"-"`
	PlaybackOutcome        string     `json:"playbackOutcome,omitempty"`
	ListenedMS             int64      `json:"listenedMs,omitempty"`
	DurationMS             int64      `json:"durationMs,omitempty"`
	TransitionSourceItemID string     `json:"-"`
	TransitionSourceKey    string     `json:"-"`
	LastFeedbackAt         *time.Time `json:"-"`
	Song                   *MediaFile `json:"song,omitempty"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

type PersonalRadioSessionResponse struct {
	Session        PersonalRadioSession `json:"session"`
	Items          []PersonalRadioItem  `json:"items"`
	Pending        bool                 `json:"pending"`
	PlanningStatus string               `json:"planningStatus"`
}

type CreatePersonalRadioRequest struct {
	SeedMediaFileID string    `json:"seedMediaFileId"`
	Mode            RadioMode `json:"mode,omitempty"`
}

type RefillPersonalRadioRequest struct {
	CurrentItemID string    `json:"currentItemId,omitempty"`
	QueuedItemIDs []string  `json:"queuedItemIds,omitempty"`
	Mode          RadioMode `json:"mode,omitempty"`
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

// RadioTransitionFeedback aggregates playback outcomes for a contextual
// source-to-target relationship. The stable keys are canonical; media-file
// IDs are only denormalized lookup hints for candidate injection.
type RadioTransitionFeedback struct {
	UserID            string
	SourceKey         string
	TargetKey         string
	SourceMediaFileID string
	TargetMediaFileID string
	AttemptCount      int
	AcceptedCount     int
	CompletedCount    int
	EarlySkipCount    int
	NeutralSkipCount  int
	KeepCount         int
	LastAttemptAt     *time.Time
	LastPositiveAt    *time.Time
	LastNegativeAt    *time.Time
	UpdatedAt         time.Time
}

type RadioPlaybackFeedbackResult struct {
	Item    PersonalRadioItem
	Applied bool
}

// NormalizeRecordingMBID is the one canonical normalization used for radio
// identity and feedback lookup.
func NormalizeRecordingMBID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// RadioTrackKey returns a stable identity that survives a track moving between
// a temporary discovery file and a library file.
func RadioTrackKey(recordingMBID, mediaFileID string) string {
	if mbid := NormalizeRecordingMBID(recordingMBID); mbid != "" {
		return "mbid:" + mbid
	}
	if mediaFileID = strings.TrimSpace(mediaFileID); mediaFileID != "" {
		return "media:" + mediaFileID
	}
	return ""
}

func IsAcceptedRadioPlaybackOutcome(outcome string) bool {
	switch outcome {
	case RadioPlaybackAccepted, RadioPlaybackCompleted, RadioPlaybackLateSkip, RadioPlaybackKeep:
		return true
	default:
		return false
	}
}

// NormalizeRadioMode keeps persisted and request-provided mode values within
// the small set understood by the composer. Unknown or empty values use the
// balanced default so older clients remain compatible.
func NormalizeRadioMode(value string) RadioMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case RadioModeFamiliar:
		return RadioModeFamiliar
	case RadioModeDiscover:
		return RadioModeDiscover
	default:
		return RadioModeBalanced
	}
}

type PersonalRadioRepository interface {
	CreateSession(*PersonalRadioSession, []PersonalRadioItem) error
	UpdateSession(*PersonalRadioSession) error
	EndActiveSessions(userID string, exceptID string) error
	GetSessionForUser(sessionID, userID string) (*PersonalRadioSession, error)
	GetItems(sessionID string) ([]PersonalRadioItem, error)
	GetItemForUser(itemID, userID string) (*PersonalRadioItem, error)
	AppendItems(sessionID string, items []PersonalRadioItem) error
	UpdateItem(*PersonalRadioItem) error
	RecentSeedIDs(sessionID string, limit int) ([]string, error)
	GetRecentAcceptedItems(sessionID string, limit int) ([]PersonalRadioItem, error)
	RecordPlaybackFeedback(userID, sessionID string, request PersonalRadioFeedbackRequest, now time.Time) (*RadioPlaybackFeedbackResult, error)
	GetTransitionsForTargets(userID, sourceKey string, targetKeys []string) (map[string]RadioTransitionFeedback, error)
	GetTopTransitions(userID, sourceKey string, limit int) ([]RadioTransitionFeedback, error)
	UpsertDiscovery(*DiscoveryTrack) error
	GetDiscoveryByMediaFile(userID, mediaFileID string) (*DiscoveryTrack, error)
	GetDiscoveryByRecording(userID, recordingMBID string) (*DiscoveryTrack, error)
	ListExpiredDiscoveries(now time.Time, limit int) ([]DiscoveryTrack, error)
	UpdateDiscovery(*DiscoveryTrack) error
	RecordFeedback(userID, recordingMBID, event string, now time.Time) error
	GetFeedback(userID string, recordingMBIDs []string) (map[string]RadioTrackFeedback, error)
	IsMediaFileProtected(mediaFileID string) (bool, error)
}
