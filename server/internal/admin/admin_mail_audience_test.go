package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func TestAdminAllPlayerMailSnapshotAndResume(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	player, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-00000000a501")
	if err != nil {
		t.Fatal(err)
	}
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Metadata is sufficient for audience selection; no 12,000 save bodies are
	// needed. Delivery below uses a normally initialized real test player.
	for i := 0; i < 12000; i++ {
		id := 1100000 + i
		if _, err := tx.Exec(`INSERT INTO cn_local_account (user_id,login_uuid,session_key,created_utc,last_login_utc) VALUES (?,?,?,?,?)`, id, fmt.Sprint("audience-", id), fmt.Sprint("session-", id), "2026-10-18T00:00:00Z", "2026-10-18T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO cn_local_account (user_id,login_uuid,session_key,created_utc,last_login_utc) VALUES (?,?,?,?,?)`, accountstore.SystemPartnerUserIDBase+500, "audience-system", "session-system", "2026-10-18T00:00:00Z", "2026-10-18T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	ids, err := accounts.PlayerIDs(context.Background(), accountstore.PrimaryUserID, accountstore.SystemPartnerUserIDBase-1)
	if err != nil || len(ids) < 12000 || !slices.IsSorted(ids) {
		t.Fatalf("audience count=%d err=%v", len(ids), err)
	}
	for _, id := range ids {
		if id >= accountstore.SystemPartnerUserIDBase {
			t.Fatal("system partner entered audience")
		}
	}
	large, _ := json.Marshal(ids)
	if len(large) <= 64*1024 {
		t.Fatal("fixture must exceed the ordinary request body size")
	}
	operations, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := masterdata.LoadPlayerProgressionRuntimeMaster(filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin := &API{accounts: accounts, progression: policy, operations: operations, business: accounthttp.New(accounthttp.Config{}), catalogByKey: map[string]AdminCatalogEntry{"4:0": {Kind: "currency", RewardType: 4, Name: "金币"}}}
	call := func(handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "http://localhost/", bytes.NewReader(raw))
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Kairisei-Admin-Action", "apply")
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	batch := AdminMailBatch{ID: "all-players-test-1", Title: "全员测试", Message: "感谢游玩", AllPlayers: true, Rewards: []AdminMailRequest{{RewardType: 4, Quantity: 99}}}
	preview := call(admin.previewMailBatch, batch)
	if preview.Code != 200 {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	var result struct {
		Batch          AdminMailBatch `json:"batch"`
		Confirmation   AdminMailBatch `json:"confirmation"`
		RecipientCount int            `json:"recipient_count"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.RecipientCount != len(ids) || len(result.Batch.UserIDs) != 0 || !result.Batch.AllPlayers || result.Confirmation.RecipientRange == nil || result.Confirmation.RecipientRange.StartUID != ids[0] || result.Confirmation.RecipientRange.EndUID != ids[len(ids)-1] || result.Confirmation.RecipientRange.Count != len(ids) || preview.Body.Len() > 4096 {
		t.Fatalf("preview is not a compact full-audience snapshot: %s", preview.Body.String())
	}
	if doc, err := operations.storage.ReadDocument(adminBatchPrefix + batch.ID); err != nil || doc.Revision != 0 {
		t.Fatal("preview created a runnable batch")
	}
	manual := batch
	manual.AllPlayers, manual.UserIDs = false, ids[:500]
	manual.RecipientRange = nil
	if _, err := admin.validateMailBatch(&manual); err != nil {
		t.Fatalf("manual 500: %v", err)
	}
	manual.UserIDs = ids[:501]
	if _, err := admin.validateMailBatch(&manual); err == nil {
		t.Fatal("manual cap disappeared")
	}
	if w := call(admin.createMailBatch, batch); w.Code != 400 {
		t.Fatal("all-player create omitted fixed UID bounds")
	}
	bad := result.Confirmation
	bad.RecipientRange = &mailRecipientRange{StartUID: ids[0], EndUID: ids[len(ids)-1], Count: len(ids) + 1}
	if w := call(admin.createMailBatch, bad); w.Code != 409 {
		t.Fatal("changed recipient count accepted")
	}
	newPlayer, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-00000000a502")
	if err != nil {
		t.Fatal(err)
	}
	created := call(admin.createMailBatch, result.Confirmation)
	if created.Code != 200 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	stored, doc, err := admin.readMailBatch(batch.ID)
	if err != nil || !slices.Equal(stored.UserIDs, ids) || slices.Contains(stored.UserIDs, newPlayer.UserID) || newPlayer.UserID <= stored.RecipientRange.EndUID {
		t.Fatal("confirmation changed the preview audience or payload")
	}
	// The range is self-contained. Confirmation and retry need no process-local
	// preview state, including after a server restart.
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	admin = &API{accounts: accounts, progression: policy, operations: restarted, business: accounthttp.New(accounthttp.Config{}), catalogByKey: admin.catalogByKey}
	if w := call(admin.createMailBatch, result.Confirmation); w.Code != 200 {
		t.Fatalf("confirmation retry after restart: %d %s", w.Code, w.Body.String())
	}
	before, err := accounts.LoadState(player.UserID)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Post("/{batchID}/run", admin.runMailBatch)
	run := func(id int) *httptest.ResponseRecorder {
		return call(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = "/" + batch.ID + "/run"
			router.ServeHTTP(w, r)
		}, map[string]any{"user_ids": []int{id}})
	}
	if w := run(newPlayer.UserID); w.Code != 400 {
		t.Fatal("post-preview registration entered frozen audience")
	}
	for i := 0; i < 2; i++ {
		if w := run(player.UserID); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
			t.Fatalf("run: %d %s", w.Code, w.Body.String())
		}
	}
	after, err := accounts.LoadState(player.UserID)
	if err != nil || len(after.Engagement.Presents) != len(before.Engagement.Presents)+1 {
		t.Fatal("retry duplicated or lost mail")
	}
	seen, err := operations.storage.DeliveredUsers(adminBatchPrefix+batch.ID, doc.SHA256)
	if err != nil || len(seen) != 1 || !seen[player.UserID] {
		t.Fatal("large batch did not retain per-player receipt")
	}
	// A changed range under the same batch ID cannot alter a recorded audience.
	bad = result.Confirmation
	bad.RecipientRange = &mailRecipientRange{StartUID: ids[0], EndUID: newPlayer.UserID, Count: len(ids) + 1}
	if w := call(admin.createMailBatch, bad); w.Code != 409 {
		t.Fatal("changed range overwrote the original batch")
	}
	unchanged, _, err := admin.readMailBatch(batch.ID)
	if err != nil || !slices.Equal(unchanged.UserIDs, ids) {
		t.Fatal("rejected range changed the stored audience")
	}
	// Restart before the first confirmation also needs no temporary preview cache.
	batch = result.Confirmation
	batch.ID = "range-after-restart"
	if w := call(admin.createMailBatch, batch); w.Code != 200 {
		t.Fatalf("first confirmation after restart: %d %s", w.Code, w.Body.String())
	}
	bad = result.Confirmation
	bad.ID = "invalid-system-range"
	bad.RecipientRange = &mailRecipientRange{StartUID: ids[0], EndUID: accountstore.SystemPartnerUserIDBase, Count: len(ids)}
	if w := call(admin.createMailBatch, bad); w.Code != 400 {
		t.Fatal("system UID range accepted")
	}
}
