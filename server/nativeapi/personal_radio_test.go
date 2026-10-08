package nativeapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/navidrome/navidrome/core/personalradio"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

type feedbackRouteService struct {
	personalradio.Service
	err       error
	payload   model.PersonalRadioFeedbackRequest
	sessionID string
}

func (s *feedbackRouteService) Feedback(_ context.Context, _, sessionID string, payload model.PersonalRadioFeedbackRequest) error {
	s.payload, s.sessionID = payload, sessionID
	return s.err
}

func TestRadioFeedbackHTTPResults(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"success or duplicate", nil, 204},
		{"contention exhausted", model.ErrNotAvailable, 503},
		{"event collision", model.ErrFeedbackEventConflict, 409},
		{"missing or unowned", model.ErrNotFound, 404},
		{"validation", model.ErrValidation, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &feedbackRouteService{err: test.err}
			api := &Router{personalRadio: service}
			router := chi.NewRouter()
			api.addPersonalRadioRoute(router)
			req := httptest.NewRequest(http.MethodPost, "/personal-radio/sessions/session/feedback", strings.NewReader(`{"eventId":"event","itemId":"item","event":"completed"}`))
			req = req.WithContext(request.WithUser(req.Context(), model.User{ID: "user"}))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			if test.status == 503 && response.Header().Get("Retry-After") != "1" {
				t.Fatal("missing Retry-After")
			}
			if service.payload.EventID != "event" || service.sessionID != "session" {
				t.Fatalf("request lost: %#v", service)
			}
		})
	}
}
