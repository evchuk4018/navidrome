package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
	"github.com/navidrome/navidrome/server/events"
)

type playbackClaim struct {
	Intent        string                    `json:"intent"`
	RequestID     string                    `json:"requestId"`
	ExpectedEpoch int64                     `json:"expectedEpoch"`
	DeviceLabel   string                    `json:"deviceLabel"`
	Snapshot      *model.WebPlaybackSession `json:"snapshot"`
}

type playbackCheckpoint struct {
	Epoch           int64                     `json:"epoch"`
	Sequence        int64                     `json:"sequence"`
	Snapshot        *model.WebPlaybackSession `json:"snapshot,omitempty"`
	PositionMS      *int64                    `json:"positionMs,omitempty"`
	SelectedEntryID string                    `json:"selectedEntryId,omitempty"`
	State           string                    `json:"state,omitempty"`
}

func (api *Router) addWebPlaybackRoute(r chi.Router) {
	r.Route("/playback-session", func(r chi.Router) {
		r.Get("/", api.getWebPlayback)
		r.Post("/claim", api.claimWebPlayback)
		r.Patch("/", api.checkpointWebPlayback)
	})
}

func playbackActor(w http.ResponseWriter, r *http.Request) (model.User, string, bool) {
	user, ok := request.UserFrom(r.Context())
	client := r.Header.Get("X-ND-Client-Unique-Id")
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return user, "", false
	}
	if client == "" || len(client) > 100 {
		http.Error(w, "page instance ID required", http.StatusBadRequest)
		return user, "", false
	}
	return user, client, true
}

func decodePlayback(w http.ResponseWriter, r *http.Request, payload any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(payload); err != nil {
		http.Error(w, "invalid playback request", http.StatusBadRequest)
		return false
	}
	return true
}

func playbackError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, model.ErrPlaybackOwnership) {
		status = http.StatusConflict
	} else if errors.Is(err, model.ErrInvalidRequest) {
		status = http.StatusBadRequest
	}
	if status == http.StatusInternalServerError {
		http.Error(w, "unable to save playback session", status)
		return
	}
	http.Error(w, err.Error(), status)
}

func playbackRead(ctx context.Context, ds model.DataStore, userID string) (*model.WebPlaybackSession, error) {
	s, err := ds.WebPlaybackSession(ctx).Retrieve(userID)
	if errors.Is(err, model.ErrNotFound) {
		return nil, nil
	}
	return s, err
}

func playbackCopyFields(dst, src *model.WebPlaybackSession) {
	dst.Entries, dst.SelectedEntryID = src.Entries, src.SelectedEntryID
	dst.PositionMS, dst.State, dst.Mode = src.PositionMS, src.State, src.Mode
	dst.RadioSessionID = src.RadioSessionID
	dst.CurrentEntry = nil
}

// Resolve through user-scoped repositories. The saved display fields never
// authorize a stream, and missing entries retain their occurrence/order.
func (api *Router) hydrateWebPlayback(ctx context.Context, s *model.WebPlaybackSession, summary bool) {
	if s == nil {
		return
	}
	for i := range s.Entries {
		e := &s.Entries[i]
		if summary && e.EntryID != s.SelectedEntryID {
			continue
		}
		if e.RadioItemID != "" {
			item, err := api.ds.WebPlaybackSession(ctx).RadioItemForUser(s.UserID, e.RadioSessionID, e.RadioItemID)
			if err != nil {
				e.Unavailable = true
			} else {
				e.ID = item.MediaFileID
				e.Pending = item.Status == model.RadioItemDownloading
				e.Unavailable = item.Status == model.RadioItemFailed || (e.ID == "" && !e.Pending)
			}
		}
		switch e.Source {
		case "music":
			if e.Pending {
				break
			}
			song, err := api.ds.MediaFile(ctx).GetWithParticipants(e.ID)
			if err != nil || song.Missing {
				e.Unavailable = true
				break
			}
			e.Song, e.Title, e.Artist = song, song.Title, song.Artist
		case "radio":
			station, err := api.ds.Radio(ctx).Get(e.ID)
			if err != nil {
				e.Unavailable = true
			} else {
				e.Station, e.Title, e.Artist = station, station.Name, station.Name
			}
		}
		if e.EntryID == s.SelectedEntryID {
			current := *e
			s.CurrentEntry = &current
		}
	}
	if summary {
		s.Entries = nil
	}
}

func (api *Router) validateWebPlayback(ctx context.Context, ds model.DataStore, userID string, s, previous *model.WebPlaybackSession) error {
	bad := func(message string) error { return fmt.Errorf("%w: %s", model.ErrInvalidRequest, message) }
	if s == nil || len(s.Entries) > 5000 || s.PositionMS < 0 || s.PositionMS > 7*24*60*60*1000 {
		return bad("invalid queue or position")
	}
	if s.State != "playing" && s.State != "paused" && s.State != "stopped" {
		return bad("invalid playback state")
	}
	switch s.Mode {
	case "order", "orderLoop", "singleLoop", "shufflePlay":
	default:
		return bad("invalid playback mode")
	}
	seen := map[string]bool{}
	old := map[string]model.WebPlaybackEntry{}
	if previous != nil {
		for _, e := range previous.Entries {
			old[e.EntryID] = e
		}
	}
	found := s.SelectedEntryID == "" && len(s.Entries) == 0
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.EntryID == "" || len(e.EntryID) > 100 || seen[e.EntryID] || len(e.ID) > 255 ||
			len(e.Title) > 1000 || len(e.Artist) > 1000 {
			return bad("invalid queue entry")
		}
		seen[e.EntryID] = true
		found = found || e.EntryID == s.SelectedEntryID
		e.Song, e.Station, e.Unavailable = nil, nil, false
		known := old[e.EntryID].Source == e.Source && old[e.EntryID].ID == e.ID
		if e.RadioItemID != "" {
			item, err := ds.WebPlaybackSession(ctx).RadioItemForUser(userID, e.RadioSessionID, e.RadioItemID)
			if err != nil {
				return bad("radio item unavailable")
			}
			if e.Source != "music" {
				return bad("invalid radio source")
			}
			e.ID, e.Pending, e.RadioItemType = item.MediaFileID, item.Status == model.RadioItemDownloading, item.ItemType
			if s.RadioSessionID != e.RadioSessionID {
				return bad("radio session mismatch")
			}
		} else if e.Pending || e.RadioSessionID != "" {
			return bad("invalid pending entry")
		}
		switch e.Source {
		case "music":
			e.Video = nil
			if !e.Pending {
				song, err := ds.MediaFile(ctx).GetWithParticipants(e.ID)
				if err != nil || song.Missing {
					if !known {
						return bad("music entry unavailable")
					}
				} else {
					e.Title, e.Artist = song.Title, song.Artist
				}
			}
		case "hometube":
			if e.ID == "" || e.Video == nil || e.Video.ID != e.ID || len(e.Video.Title) > 1000 ||
				len(e.Video.ChannelName) > 1000 || len(e.Video.ThumbnailURL) > 2048 {
				return bad("invalid video entry")
			}
			e.Title, e.Artist = e.Video.Title, e.Video.ChannelName
		case "radio":
			e.Video = nil
			station, err := ds.Radio(ctx).Get(e.ID)
			if err != nil {
				if !known {
					return bad("station unavailable")
				}
			} else {
				e.Title, e.Artist = station.Name, station.Name
			}
		default:
			return bad("unsupported playback source")
		}
	}
	if !found {
		return bad("selected entry not in queue")
	}
	if len(s.Entries) == 0 {
		s.PositionMS, s.State, s.RadioSessionID = 0, "stopped", ""
	}
	return nil
}

func (api *Router) getWebPlayback(w http.ResponseWriter, r *http.Request) {
	user, _, ok := playbackActor(w, r)
	if !ok {
		return
	}
	s, err := playbackRead(r.Context(), api.ds, user.ID)
	if err != nil {
		playbackError(w, err)
		return
	}
	api.hydrateWebPlayback(r.Context(), s, r.URL.Query().Get("summary") == "true")
	w.Header().Set("Cache-Control", "no-store")
	writeMusicJSON(w, map[string]any{"session": s})
}

func (api *Router) claimWebPlayback(w http.ResponseWriter, r *http.Request) {
	user, client, ok := playbackActor(w, r)
	if !ok {
		return
	}
	var payload playbackClaim
	if !decodePlayback(w, r, &payload) {
		return
	}
	if payload.RequestID == "" || len(payload.RequestID) > 100 || len(payload.DeviceLabel) > 100 || payload.ExpectedEpoch < 0 {
		http.Error(w, "invalid claim", http.StatusBadRequest)
		return
	}
	var s *model.WebPlaybackSession
	err := api.ds.WithTxImmediate(func(tx model.DataStore) error {
		var err error
		s, err = playbackRead(r.Context(), tx, user.ID)
		if err != nil {
			return err
		}
		if s == nil {
			s = &model.WebPlaybackSession{UserID: user.ID}
		}
		if s.LastClaimRequestID == payload.RequestID && s.OwnerClientID == client {
			return nil
		}
		if s.Epoch != payload.ExpectedEpoch {
			return model.ErrPlaybackOwnership
		}
		switch payload.Intent {
		case "play":
			if err := api.validateWebPlayback(r.Context(), tx, user.ID, payload.Snapshot, s); err != nil {
				return err
			}
			playbackCopyFields(s, payload.Snapshot)
		case "resume":
			if len(s.Entries) == 0 {
				return fmt.Errorf("%w: no saved queue", model.ErrInvalidRequest)
			}
		default:
			return fmt.Errorf("%w: invalid claim intent", model.ErrInvalidRequest)
		}
		s.OwnerClientID, s.DeviceLabel = client, strings.TrimSpace(payload.DeviceLabel)
		s.Epoch++
		s.Revision++
		s.Sequence = 0
		s.LastClaimRequestID, s.UpdatedAt = payload.RequestID, time.Now().UTC()
		return tx.WebPlaybackSession(r.Context()).Store(s)
	}, "claim web playback")
	if err != nil {
		playbackError(w, err)
		return
	}
	api.publishWebPlayback(r.Context(), s)
	api.hydrateWebPlayback(r.Context(), s, false)
	writeMusicJSON(w, map[string]any{"session": s})
}

func (api *Router) checkpointWebPlayback(w http.ResponseWriter, r *http.Request) {
	user, client, ok := playbackActor(w, r)
	if !ok {
		return
	}
	var payload playbackCheckpoint
	if !decodePlayback(w, r, &payload) {
		return
	}
	if payload.Sequence <= 0 || payload.Epoch <= 0 {
		http.Error(w, "invalid checkpoint", http.StatusBadRequest)
		return
	}
	var s *model.WebPlaybackSession
	structural := false
	err := api.ds.WithTxImmediate(func(tx model.DataStore) error {
		var err error
		s, err = playbackRead(r.Context(), tx, user.ID)
		if err != nil {
			return err
		}
		if s == nil || s.OwnerClientID != client || s.Epoch != payload.Epoch {
			return model.ErrPlaybackOwnership
		}
		if payload.Sequence <= s.Sequence {
			return nil
		}
		if payload.Snapshot != nil {
			if err := api.validateWebPlayback(r.Context(), tx, user.ID, payload.Snapshot, s); err != nil {
				return err
			}
			playbackCopyFields(s, payload.Snapshot)
			structural = true
		} else {
			if payload.SelectedEntryID != s.SelectedEntryID {
				return fmt.Errorf("%w: checkpoint item changed", model.ErrInvalidRequest)
			}
			if payload.PositionMS != nil {
				if *payload.PositionMS < 0 || *payload.PositionMS > 7*24*60*60*1000 {
					return fmt.Errorf("%w: invalid position", model.ErrInvalidRequest)
				}
				s.PositionMS = *payload.PositionMS
			}
			if payload.State != "playing" && payload.State != "paused" && payload.State != "stopped" {
				return fmt.Errorf("%w: invalid state", model.ErrInvalidRequest)
			}
			s.State = payload.State
		}
		s.Sequence = payload.Sequence
		s.Revision++
		s.UpdatedAt = time.Now().UTC()
		return tx.WebPlaybackSession(r.Context()).Store(s)
	}, "checkpoint web playback")
	if err != nil {
		playbackError(w, err)
		return
	}
	if structural {
		api.publishWebPlayback(r.Context(), s)
	}
	api.hydrateWebPlayback(r.Context(), s, true)
	writeMusicJSON(w, map[string]any{"session": s})
}

func (api *Router) publishWebPlayback(ctx context.Context, s *model.WebPlaybackSession) {
	events.GetBroker().SendMessage(ctx, &events.PlaybackSessionChanged{UserID: s.UserID,
		Epoch: s.Epoch, Revision: s.Revision, OwnerClientID: s.OwnerClientID})
}
