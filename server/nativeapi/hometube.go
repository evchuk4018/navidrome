package nativeapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
)

func (api *Router) addHomeTubeRoute(r chi.Router) {
	r.Route("/hometubeVideo/{id}", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			video, err := api.playlists.GetVideo(r.Context(), chi.URLParam(r, "id"))
			if err != nil {
				http.Error(w, "video not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(video)
		})
		r.Put("/", func(w http.ResponseWriter, r *http.Request) {
			var video model.HomeTubeVideo
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&video); err != nil {
				http.Error(w, "invalid video", http.StatusBadRequest)
				return
			}
			video.ID = chi.URLParam(r, "id")
			if err := api.playlists.SaveVideo(r.Context(), &video); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			result, err := api.playlists.GetVideo(r.Context(), video.ID)
			if err != nil {
				http.Error(w, "unable to load video", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
		})
		r.Put("/favorite", func(w http.ResponseWriter, r *http.Request) {
			var payload struct {
				Starred bool `json:"starred"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&payload); err != nil {
				http.Error(w, "invalid favorite", http.StatusBadRequest)
				return
			}
			if err := api.playlists.SetVideoFavorite(r.Context(), chi.URLParam(r, "id"), payload.Starred); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(payload)
		})
	})
}
