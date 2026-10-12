package model

import (
	"errors"
	"time"
)

var ErrPlaybackOwnership = errors.New("playback ownership changed")
var ErrInvalidRequest = errors.New("invalid playback request")

// WebPlaybackEntry identifies an occurrence, not just a track: duplicates retain
// their positions. Song and Unavailable are response-only hydration fields.
type WebPlaybackEntry struct {
	EntryID        string         `json:"entryId"`
	Source         string         `json:"source"`
	ID             string         `json:"id"`
	Title          string         `json:"title,omitempty"`
	Artist         string         `json:"artist,omitempty"`
	Video          *HomeTubeVideo `json:"video,omitempty"`
	RadioSessionID string         `json:"radioSessionId,omitempty"`
	RadioItemID    string         `json:"radioItemId,omitempty"`
	RadioItemType  string         `json:"radioItemType,omitempty"`
	Pending        bool           `json:"pending,omitempty"`
	Song           *MediaFile     `json:"song,omitempty"`
	Station        *Radio         `json:"station,omitempty"`
	Unavailable    bool           `json:"unavailable,omitempty"`
}

type WebPlaybackSession struct {
	UserID             string             `json:"-"`
	Entries            []WebPlaybackEntry `json:"entries,omitempty"`
	SelectedEntryID    string             `json:"selectedEntryId"`
	PositionMS         int64              `json:"positionMs"`
	State              string             `json:"state"`
	Mode               string             `json:"mode"`
	RadioSessionID     string             `json:"radioSessionId,omitempty"`
	OwnerClientID      string             `json:"ownerClientId"`
	DeviceLabel        string             `json:"deviceLabel"`
	Epoch              int64              `json:"epoch"`
	Sequence           int64              `json:"sequence"`
	Revision           int64              `json:"revision"`
	LastClaimRequestID string             `json:"lastClaimRequestId"`
	UpdatedAt          time.Time          `json:"updatedAt"`
	CurrentEntry       *WebPlaybackEntry  `json:"currentEntry,omitempty"`
}

type WebPlaybackSessionRepository interface {
	Retrieve(userID string) (*WebPlaybackSession, error)
	Store(*WebPlaybackSession) error
	RadioItemForUser(userID, sessionID, itemID string) (*PersonalRadioItem, error)
}

type WebPlaybackGuard struct {
	ClientID string
	Epoch    int64
}
