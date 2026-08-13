package nativeapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

func (api *Router) addPersonalRadioRoute(r chi.Router) {
	r.Route("/personal-radio", func(r chi.Router) {
		r.Post("/sessions", api.createPersonalRadio)
		r.Get("/sessions/{id}", api.refillPersonalRadio)
		r.Post("/sessions/{id}/feedback", api.personalRadioFeedback)
	})
}

func (api *Router) createPersonalRadio(w http.ResponseWriter, r *http.Request) {
	if api.personalRadio == nil {
		http.Error(w, "personal radio is not configured", http.StatusNotImplemented)
		return
	}
	user, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var payload model.CreatePersonalRadioRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	response, err := api.personalRadio.Create(r.Context(), user.ID, payload.SeedMediaFileID)
	if err != nil {
		writeMusicError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeMusicJSON(w, response)
}

func (api *Router) refillPersonalRadio(w http.ResponseWriter, r *http.Request) {
	if api.personalRadio == nil {
		http.Error(w, "personal radio is not configured", http.StatusNotImplemented)
		return
	}
	user, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	response, err := api.personalRadio.Refill(r.Context(), user.ID, chi.URLParam(r, "id"))
	if err != nil {
		writeMusicError(w, r, err, http.StatusInternalServerError)
		return
	}
	writeMusicJSON(w, response)
}

func (api *Router) personalRadioFeedback(w http.ResponseWriter, r *http.Request) {
	if api.personalRadio == nil {
		http.Error(w, "personal radio is not configured", http.StatusNotImplemented)
		return
	}
	user, ok := request.UserFrom(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var payload model.PersonalRadioFeedbackRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := api.personalRadio.Feedback(r.Context(), user.ID, chi.URLParam(r, "id"), payload); err != nil {
		writeMusicError(w, r, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
