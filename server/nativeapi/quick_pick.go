package nativeapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model/request"
)

func (api *Router) addQuickPickRoute(r chi.Router) {
	r.Get("/quick-pick", api.getQuickPick)
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
