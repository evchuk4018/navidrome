package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/tests"
)

func TestHeartbeatWithBasePath(t *testing.T) {
	tests.Init(t, false)
	conf.Server.BasePath = "/navidrome"
	server := &Server{}
	server.initRoutes()
	server.mountRootRedirector()
	for _, endpoint := range []string{"/ping", "/navidrome/ping"} {
		t.Run(endpoint, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint, nil))
			if response.Code != http.StatusOK || response.Body.String() != "." {
				t.Fatalf("heartbeat: status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}
