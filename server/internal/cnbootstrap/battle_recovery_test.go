package cnbootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/accounthttp"
)

func TestCNBattleRecoveryRouteRequiresSessionAndReachesAccountAPI(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(log, nil, 0600); err != nil {
		t.Fatal(err)
	}
	testHandler := newTestHandler(t, root, log).(testSessionHandler)
	for _, tc := range []struct {
		name, body string
		status     int
		success    bool
	}{
		{"lookup", testHandler.sessionKey + `{"abandoned_roomids":[]}`, 200, true},
		{"idempotent departure", testHandler.sessionKey + `{"abandoned_roomids":[123]}`, 200, true},
		{"missing session", `{"abandoned_roomids":[]}`, 401, false},
		{"forged session", `unregistered-session-key{"abandoned_roomids":[]}`, 200, false},
		{"foreign account field", testHandler.sessionKey + `{"abandoned_roomids":[],"userid":123}`, 400, false},
		{"invalid departure", testHandler.sessionKey + `{"abandoned_roomids":[-1]}`, 400, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/BattleRecoveryShow", strings.NewReader(tc.body))
			r.Header.Set(accounthttp.AccountUserHeader, "9999999") // cannot choose another account with a header
			w := httptest.NewRecorder()
			testHandler.Handler.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("route status %d: %s", w.Code, w.Body.String())
			}
			if tc.success {
				parts := strings.Split(w.Body.String(), "\n")
				if len(parts) < 3 {
					t.Fatal("not the account protocol envelope")
				}
				var common struct {
					Code int `json:"res_code"`
				}
				var body map[string]json.RawMessage
				if json.Unmarshal([]byte(parts[0]), &common) != nil || common.Code != 0 || json.Unmarshal([]byte(parts[1]), &body) != nil ||
					string(body["recovery"]) != "null" || body["battlesv"] == nil || body["user"] != nil {
					t.Fatalf("outer adapter did not reach the recovery API: %s", w.Body.String())
				}
			} else if tc.status == 200 && !strings.Contains(w.Body.String(), `"res_code":-3208`) {
				t.Fatal("forged session entered recovery API")
			}
		})
	}
}
