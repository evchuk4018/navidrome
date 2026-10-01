package nativeapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

type quickPickRouteService struct {
	impressions []model.QuickPickImpressionRequest
	clicks      []model.QuickPickClickRequest
	clickUser   string
	clickErr    error
}

func (s *quickPickRouteService) Get(context.Context, string) (*model.QuickPickResponse, error) {
	return &model.QuickPickResponse{}, nil
}
func (s *quickPickRouteService) RecordPlaylistPlay(context.Context, string, string) error {
	return nil
}
func (s *quickPickRouteService) RecordImpressions(_ context.Context, _ string, viewID string, itemKeys []string) error {
	s.impressions = append(s.impressions, model.QuickPickImpressionRequest{ViewID: viewID, ItemKeys: itemKeys})
	return nil
}
func (s *quickPickRouteService) RecordClick(_ context.Context, userID, viewID, itemKey string) error {
	s.clickUser = userID
	s.clicks = append(s.clicks, model.QuickPickClickRequest{ViewID: viewID, ItemKey: itemKey})
	return s.clickErr
}

func TestRecordQuickPickImpressionsRequiresViewID(t *testing.T) {
	service := &quickPickRouteService{}
	api := &Router{quickPick: service}
	req := httptest.NewRequest(http.MethodPost, "/quick-pick/impressions", strings.NewReader(`{"itemKeys":["track:a"]}`))
	req = req.WithContext(request.WithUser(req.Context(), model.User{ID: "user"}))
	response := httptest.NewRecorder()
	api.recordQuickPickImpressions(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if len(service.impressions) != 0 {
		t.Fatalf("impressions = %#v, want none", service.impressions)
	}
}

func TestRecordQuickPickImpressionsPassesViewAndVisibleItems(t *testing.T) {
	service := &quickPickRouteService{}
	api := &Router{quickPick: service}
	req := httptest.NewRequest(http.MethodPost, "/quick-pick/impressions", strings.NewReader(`{"viewId":"view-1","itemKeys":["track:a","playlist:p"]}`))
	req = req.WithContext(request.WithUser(req.Context(), model.User{ID: "user"}))
	response := httptest.NewRecorder()
	api.recordQuickPickImpressions(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
	if len(service.impressions) != 1 || service.impressions[0].ViewID != "view-1" || len(service.impressions[0].ItemKeys) != 2 {
		t.Fatalf("captured impressions = %#v", service.impressions)
	}
}

func TestRecordQuickPickClickValidatesRequestAndUser(t *testing.T) {
	for _, test := range []struct {
		name   string
		body   string
		user   bool
		err    error
		status int
	}{
		{name: "valid", body: `{"viewId":"view-1","itemKey":"track:a"}`, user: true, status: http.StatusNoContent},
		{name: "no user", body: `{"viewId":"view-1","itemKey":"track:a"}`, status: http.StatusUnauthorized},
		{name: "missing view", body: `{"itemKey":"track:a"}`, user: true, status: http.StatusBadRequest},
		{name: "missing key", body: `{"viewId":"view-1"}`, user: true, status: http.StatusBadRequest},
		{name: "blank view", body: `{"viewId":" ","itemKey":"track:a"}`, user: true, status: http.StatusBadRequest},
		{name: "malformed", body: `{`, user: true, status: http.StatusBadRequest},
		{name: "unknown impression", body: `{"viewId":"view-1","itemKey":"track:a"}`, user: true, err: model.ErrValidation, status: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &quickPickRouteService{clickErr: test.err}
			api := &Router{quickPick: service}
			req := httptest.NewRequest(http.MethodPost, "/quick-pick/clicks", strings.NewReader(test.body))
			if test.user {
				req = req.WithContext(request.WithUser(req.Context(), model.User{ID: "user"}))
			}
			response := httptest.NewRecorder()
			api.recordQuickPickClick(response, req)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
			if test.status == http.StatusNoContent && (len(service.clicks) != 1 || service.clickUser != "user" || service.clicks[0].ItemKey != "track:a") {
				t.Fatalf("recorded click = %#v for user %q", service.clicks, service.clickUser)
			}
		})
	}
}
