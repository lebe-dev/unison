package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRecoverHandlerAnswersAfterPanic checks that a panic of a handler becomes
// an error response.
func TestRecoverHandlerAnswersAfterPanic(t *testing.T) {
	server := newTestServer(t)
	handler := server.recoverHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusInternalServerError {
		t.Errorf("status is %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

// TestRecoverHandlerPassesTheResponse checks that a handler without a panic
// answers as it would without the wrapper.
func TestRecoverHandlerPassesTheResponse(t *testing.T) {
	server := newTestServer(t)
	handler := server.recoverHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusNoContent {
		t.Errorf("status is %d, want %d", response.Code, http.StatusNoContent)
	}
}
