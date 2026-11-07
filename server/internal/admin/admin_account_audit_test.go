package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func TestAdminAccountMutationAndAuditCommitTogether(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if _, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-00000000a445"); err != nil {
		t.Fatal(err)
	}
	other, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-00000000a446")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := masterdata.LoadPlayerProgressionRuntimeMaster(filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin := &API{accounts: accounts, business: accounthttp.New(accounthttp.Config{}), progression: policy,
		catalogByKey: map[string]AdminCatalogEntry{"4:0": {Kind: "gold", Name: "金币"}}}
	router := chi.NewRouter()
	router.Post("/{userID}/mail", admin.sendAccountMail)
	router.Post("/{userID}/grant", admin.grantAccountResources)
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []int{accountstore.PrimaryUserID, other.UserID} {
		for _, operation := range []string{"mail", "grant"} {
			t.Run(fmt.Sprintf("%d/%s", userID, operation), func(t *testing.T) {
				before, err := admin.loadAccountState(userID)
				if err != nil {
					t.Fatal(err)
				}
				snapshot := func() string {
					t.Helper()
					query := "SELECT revision, payload_sha256 FROM cn_account_snapshot WHERE user_id = ?"
					arguments := []any{userID}
					if userID == accountstore.PrimaryUserID {
						query, arguments = "SELECT revision, payload_sha256 FROM cn_save_snapshot WHERE singleton = 1", nil
					}
					var revision, projectionRevision int
					var digest, projectionDigest string
					if err := db.QueryRow(query, arguments...).Scan(&revision, &digest); err != nil {
						t.Fatal(err)
					}
					if err := db.QueryRow("SELECT snapshot_revision, payload_sha256 FROM cn_account_projection WHERE user_id=?", userID).Scan(&projectionRevision, &projectionDigest); err != nil {
						t.Fatal(err)
					}
					return fmt.Sprint(revision, digest, projectionRevision, projectionDigest)
				}
				body := map[string]any{"gold": 7, "idempotency_key": "grant-atomic-d466"}
				if operation == "mail" {
					body = map[string]any{"idempotency_key": "audit-atomic-446", "title": "赠礼", "message": "奖励", "reward_type": 4, "quantity": 7}
				}
				content, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				call := func() *httptest.ResponseRecorder {
					request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("http://localhost/%d/%s", userID, operation), bytes.NewReader(content))
					request.RemoteAddr = "127.0.0.1:12345"
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("X-Kairisei-Admin-Action", "apply")
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					return response
				}
				original := snapshot()
				if _, err := db.Exec("CREATE TRIGGER reject_admin_audit BEFORE INSERT ON cn_admin_audit BEGIN SELECT RAISE(ABORT, 'test audit failure'); END"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _, _ = db.Exec("DROP TRIGGER IF EXISTS reject_admin_audit") })
				failed := call()
				if failed.Code != http.StatusInternalServerError || snapshot() != original {
					t.Fatalf("failed audit changed account/projection: status=%d body=%s", failed.Code, failed.Body.String())
				}
				if _, err := db.Exec("DROP TRIGGER reject_admin_audit"); err != nil {
					t.Fatal(err)
				}
				if response := call(); response.Code != http.StatusOK {
					t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
				}
				{
					// Lost responses and parallel retries keep the same present and audit.
					var wg sync.WaitGroup
					responses := make([]*httptest.ResponseRecorder, 2)
					for index := range responses {
						wg.Add(1)
						go func() { defer wg.Done(); responses[index] = call() }()
					}
					wg.Wait()
					for _, response := range responses {
						if response.Code != http.StatusOK || operation == "mail" && !bytes.Contains(response.Body.Bytes(), []byte(`"duplicate":true`)) {
							t.Fatalf("mail retry=%s", response.Body.String())
						}
					}
				}
				after, err := accounts.LoadState(userID)
				if err != nil {
					t.Fatal(err)
				}
				if (operation == "mail" && len(after.Engagement.Presents) != len(before.Engagement.Presents)+1) || (operation == "grant" && after.User.Gold != before.User.Gold+7) {
					t.Fatal("accepted mutation was not saved exactly once")
				}
				var audits int
				if err := db.QueryRow("SELECT count(*) FROM cn_admin_audit WHERE operation=? AND target=?", "account-"+operation, fmt.Sprint(userID)).Scan(&audits); err != nil || audits != 1 {
					t.Fatalf("audits=%d err=%v", audits, err)
				}
			})
		}
	}
	hour := time.Now().UTC().Add(time.Hour)
	for _, c := range []struct {
		filter accountstore.AuditFilter
		want   int
	}{
		{accountstore.AuditFilter{Operation: "account-grant", After: hour.Add(-2 * time.Hour).Format(time.RFC3339), Before: hour.Format(time.RFC3339)}, 2},
		{accountstore.AuditFilter{After: hour.Format(time.RFC3339)}, 0},
	} {
		if records, total, err := accounts.QueryAuditRecords(c.filter, 50, 0); err != nil || total != c.want || len(records) != c.want {
			t.Fatalf("audit range %+v: total=%d err=%v", c.filter, total, err)
		}
	}
}
