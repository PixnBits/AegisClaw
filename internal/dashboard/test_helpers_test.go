package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// httptestRequest builds a request with an isolated rate-limit client key so
// dashboard tests do not share the global token bucket.
func httptestRequest(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("X-Forwarded-For", t.Name())
	return req
}