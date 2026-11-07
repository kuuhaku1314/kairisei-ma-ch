package cnbootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
)

func TestCNCardLoadModeReachesBusinessRequest(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		mode          int
		valid         bool
	}{
		{"session-only", "", 0, true},
		{"null-dto", "{}", 0, true},
		{"ordinary", `{"0":0}`, 0, true},
		{"alchemist", `{"0":2}`, 2, true},
		{"negative", `{"0":-1}`, 0, false},
		{"fraction", `{"0":2.5}`, 0, false},
		{"null", `{"0":null}`, 0, false},
		{"wrong-slot", `{"deck_info_type":2}`, 0, false},
		{"extra", `{"0":2,"1":1}`, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			business := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]int
				if err := json.Unmarshal(body, &fields); err != nil || len(fields) != 1 || fields["deck_info_type"] != tc.mode ||
					r.URL.Path != "/CardShow2" || r.Header.Get(accounthttp.AccountUserHeader) != "1000001" {
					t.Fatalf("card load request lost its mode, route or account: %s %v", body, err)
				}
				// Stop at the request boundary; response projection has its own
				// existing tests and must not turn this into a fake game response.
				w.WriteHeader(http.StatusTeapot)
			})
			r := httptest.NewRequest(http.MethodPost, "/CardShow2", strings.NewReader("test-session-00000000"+tc.payload))
			r = r.WithContext(context.WithValue(r.Context(), cnAuthenticatedUserKey{}, accountstore.PrimaryUserID))
			w := httptest.NewRecorder()
			cnBootstrapCardShow2(business)(w, r)
			if called != tc.valid || (tc.valid && w.Code != http.StatusTeapot) || (!tc.valid && w.Code != http.StatusBadRequest) {
				t.Fatalf("called=%v status=%d body=%s", called, w.Code, w.Body.String())
			}
		})
	}
}
