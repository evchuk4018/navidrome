package nativeapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

func (api *Router) addMusicRoute(r chi.Router) {
	r.Route("/music", func(r chi.Router) {
		r.Get("/search", api.searchExternalMusic)
		r.Get("/artist/{id}", api.getExternalArtist)
		r.Get("/album/{id}", api.getExternalAlbum)
		r.Post("/downloads", api.createMusicDownload)
		r.Get("/downloads", api.listMusicDownloads)
		r.Get("/downloads/{id}", api.getMusicDownload)
	})
}

func (api *Router) searchExternalMusic(w http.ResponseWriter, r *http.Request) {
	if api.music == nil {
		http.Error(w, "external music search is not configured", http.StatusNotImplemented)
		return
	}
	result, err := api.music.Search(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeMusicError(w, r, err, http.StatusBadGateway)
		return
	}
	writeMusicJSON(w, result)
}

func (api *Router) getExternalArtist(w http.ResponseWriter, r *http.Request) {
	if api.music == nil {
		http.Error(w, "external music search is not configured", http.StatusNotImplemented)
		return
	}
	artist, err := api.music.Artist(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeMusicError(w, r, err, http.StatusBadGateway)
		return
	}
	writeMusicJSON(w, artist)
}

func (api *Router) getExternalAlbum(w http.ResponseWriter, r *http.Request) {
	if api.music == nil {
		http.Error(w, "external music search is not configured", http.StatusNotImplemented)
		return
	}
	album, err := api.music.Album(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeMusicError(w, r, err, http.StatusBadGateway)
		return
	}
	writeMusicJSON(w, album)
}

func (api *Router) createMusicDownload(w http.ResponseWriter, r *http.Request) {
	if api.music == nil {
		http.Error(w, "music downloads are not configured", http.StatusNotImplemented)
		return
	}
	user, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var download model.ExternalDownloadRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	if err := decoder.Decode(&download); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	job, err := api.music.CreateDownload(r.Context(), user.ID, download)
	if err != nil {
		writeMusicError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	writeMusicJSON(w, job)
}

func (api *Router) listMusicDownloads(w http.ResponseWriter, r *http.Request) {
	if api.music == nil {
		http.Error(w, "music downloads are not configured", http.StatusNotImplemented)
		return
	}
	user, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			http.Error(w, "limit must be between 1 and 100", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	jobs, err := api.music.ListDownloads(r.Context(), user.ID, limit)
	if err != nil {
		writeMusicError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeMusicJSON(w, jobs)
}

func (api *Router) getMusicDownload(w http.ResponseWriter, r *http.Request) {
	if api.music == nil {
		http.Error(w, "music downloads are not configured", http.StatusNotImplemented)
		return
	}
	user, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	job, err := api.music.GetDownload(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeMusicError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeMusicJSON(w, job)
}

func writeMusicJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Error("Error encoding external music response", err)
	}
}

func writeMusicError(w http.ResponseWriter, r *http.Request, err error, fallbackStatus int) {
	status := fallbackStatus
	switch {
	case errors.Is(err, model.ErrValidation):
		status = http.StatusBadRequest
	case errors.Is(err, model.ErrNotFound):
		status = http.StatusNotFound
	default:
		log.Error(r.Context(), "External music request failed", err)
	}
	http.Error(w, err.Error(), status)
}
