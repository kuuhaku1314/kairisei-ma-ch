package cnbootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/testfixture"
)

func TestPastPublicationNativeProjection(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	storage := accounts.Database()
	if _, err := storage.WriteDocument(adminapi.PastBattlePublicationKey, 0, adminapi.TeamBattlePublication{Mode: "allowlist", GroupIDs: []int{3}}, "boss-policy"); err != nil {
		t.Fatal(err)
	}
	ops, err := adminapi.NewOperations(storage, nil)
	if err != nil {
		t.Fatal(err)
	}
	business := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/TeamBattlePastBossShow" {
			t.Fatal("wrong forwarding route")
		}
		_, _ = w.Write([]byte("{\"0\":0}\n{\"0\":[{\"0\":2,\"5\":1},{\"0\":3,\"5\":7}],\"1\":[{\"0\":42}]}\n{}"))
	})
	show := func(wantCount int) {
		t.Helper()
		r := httptest.NewRequest("POST", "/TeamBattlePastBossShow", strings.NewReader("test-session-00000000"))
		r = r.WithContext(context.WithValue(r.Context(), cnAuthenticatedUserKey{}, accountstore.PrimaryUserID))
		w := httptest.NewRecorder()
		cnBootstrapPastBossShow(business, ops.Runtime)(w, r)
		var result struct {
			Groups []struct {
				ID     int `json:"0"`
				Period int `json:"5"`
			} `json:"0"`
			Partners []map[string]int `json:"1"`
		}
		lines := strings.Split(w.Body.String(), "\n")
		if w.Code != 200 || len(lines) != 3 {
			t.Fatalf("bad native response: %s", w.Body.String())
		}
		if err := json.Unmarshal([]byte(lines[1]), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Groups) != wantCount || len(result.Partners) != 1 || result.Partners[0]["0"] != 42 {
			t.Fatalf("response lost catalog or partners: %s", w.Body.String())
		}
		if wantCount == 1 && (result.Groups[0].ID != 3 || result.Groups[0].Period != 7) {
			t.Fatal("archive identity or period changed")
		}
	}
	show(1)
	revision := 1
	if _, err := storage.WriteDocument(adminapi.PastBattlePublicationKey, revision, adminapi.TeamBattlePublication{Mode: "all", EndUnix: time.Now().Unix() - 1, ExpectedRevision: &revision}, "boss-policy"); err != nil {
		t.Fatal(err)
	}
	show(0)
}
