package nativeapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

func (api *Router) addQuickPickRoute(r chi.Router) {
	r.Get("/quick-pick", api.getQuickPick)
	r.Post("/quick-pick/impressions", api.recordQuickPickImpressions)
}

func (api *Router) recordQuickPickImpressions(w http.ResponseWriter, r *http.Request) {
	if api.quickPick == nil {
		http.Error(w, "Quick Pick is not configured", http.StatusNotImplemented)
		return
	}
	user, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var payload model.QuickPickImpressionRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if payload.ViewID == "" {
		http.Error(w, "viewId is required", http.StatusBadRequest)
		return
	}
	if err := api.quickPick.RecordImpressions(r.Context(), user.ID, payload.ViewID, payload.ItemKeys); err != nil {
		writeMusicError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *Router) getQuickPick(w http.ResponseWriter, r *http.Request) {
	if api.quickPick == nil {
		http.Error(w, "Quick Pick is not configured", http.StatusNotImplemented)
		return
	}
	user, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	response, err := api.quickPick.Get(r.Context(), user.ID)
	if err != nil {
		writeMusicError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeMusicJSON(w, response)
}

func (api *Router) recordPlaylistPlay(w http.ResponseWriter, r *http.Request) {
	if api.quickPick == nil {
		http.Error(w, "Quick Pick is not configured", http.StatusNotImplemented)
		return
	}
	user, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := api.quickPick.RecordPlaylistPlay(r.Context(), user.ID, chi.URLParam(r, "id")); err != nil {
		writeMusicError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
