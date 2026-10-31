package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityMiddlewareProtectsStateChanges(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := securityMiddleware("http://127.0.0.1:8750/", "test-token", next)

	tests := []struct {
		name   string
		host   string
		origin string
		token  string
		want   int
	}{
		{name: "valid request", host: "127.0.0.1:8750", origin: "http://127.0.0.1:8750/", token: "test-token", want: http.StatusNoContent},
		{name: "wrong host", host: "localhost:8750", origin: "http://127.0.0.1:8750/", token: "test-token", want: http.StatusNotFound},
		{name: "missing origin", host: "127.0.0.1:8750", token: "test-token", want: http.StatusForbidden},
		{name: "wrong token", host: "127.0.0.1:8750", origin: "http://127.0.0.1:8750/", token: "wrong", want: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8750/api/action", nil)
			request.Host = test.host
			request.Header.Set("Origin", test.origin)
			request.Header.Set("X-Ytaudio-Token", test.token)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}
