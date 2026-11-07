package cnbootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"kairisei.local/server/internal/accountstore"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/testfixture"
)

func TestGuestLoginCancelledRequestDoesNotCreateAccount(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() accountstore.CreationPolicy {
		return accountstore.CreationPolicy{DailyLimit: 10, VerificationBits: 0}
	}); err != nil {
		t.Fatal(err)
	}
	const uuid = "d5750000-0000-4000-8000-000000000099"
	profiles, err := loadPlatformDeliveries([]string{writePlatformFixture(t, t.TempDir(), "StandaloneWindows64", []byte("menu"))})
	if err != nil {
		t.Fatal(err)
	}
	login := platformClientLogin(accounts, "127.0.0.1", 26020, CDNConfig{}, "test", profiles)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/local/client/login", strings.NewReader(`{"uuid":"`+uuid+`","clver":"`+cnMinimumClientVersion+`","resource_platform":"StandaloneWindows64","unity_version":"6000.3.26f1"}`)).WithContext(ctx)
	login(httptest.NewRecorder(), r)
	if binding, err := accounts.AccountBinding(uuid); err != nil || binding.UserID != 0 {
		t.Fatalf("cancelled guest request created identity: %+v %v", binding, err)
	}
}

func TestGuestLoginCancellationWhileWaitingAndStableRetries(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() accountstore.CreationPolicy {
		return accountstore.CreationPolicy{DailyLimit: 10, VerificationBits: 0}
	}); err != nil {
		t.Fatal(err)
	}
	const uuid = "d5750000-0000-4000-8000-000000000098"
	profiles, err := loadPlatformDeliveries([]string{writePlatformFixture(t, t.TempDir(), "StandaloneWindows64", []byte("menu"))})
	if err != nil {
		t.Fatal(err)
	}
	login := platformClientLogin(accounts, "127.0.0.1", 26020, CDNConfig{}, "test", profiles)
	request := func() *http.Request {
		return httptest.NewRequest("POST", "/local/client/login", strings.NewReader(`{"uuid":"`+uuid+`","clver":"`+cnMinimumClientVersion+`","resource_platform":"StandaloneWindows64","unity_version":"6000.3.26f1"}`))
	}
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	waits := db.Stats().WaitCount
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { login(httptest.NewRecorder(), request().WithContext(ctx)); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for db.Stats().WaitCount == waits && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if db.Stats().WaitCount == waits {
		t.Fatal("login did not reach the blocked writer")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled login retained database waiter")
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if b, e := accounts.AccountBinding(uuid); e != nil || b.UserID != 0 {
		t.Fatal("cancelled waiting request created a guest", b, e)
	}
	var first int
	for i := 0; i < 24; i++ {
		w := httptest.NewRecorder()
		login(w, request())
		var result struct {
			UserID  int    `json:"userid"`
			Session string `json:"sess_key"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.UserID == 0 {
			t.Fatal("retry failed", w.Code, w.Body.String())
		}
		if i == 0 {
			first = result.UserID
		}
		if result.UserID != first {
			t.Fatal("stable UUID retry created another identity")
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cn_local_account WHERE login_uuid=?`, uuid).Scan(&count); err != nil || count != 1 || db.Stats().InUse != 0 {
		t.Fatal("retry retained accounts or SQL leases", count, err)
	}
	t.Log("cancelled database waiter released; 24 unbound guest retries retained exactly one UUID identity")
}

// Exercise failure paths against an isolated database; no listener or real
// accounts. Heap figures are evidence only, not a flaky process-RSS assertion.
func TestAccountGatewayFailuresDoNotRetainAccounts(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() accountstore.CreationPolicy {
		return accountstore.CreationPolicy{DailyLimit: 10, VerificationBits: 0}
	}); err != nil {
		t.Fatal(err)
	}
	const uuid = "d5750000-0000-4000-8000-000000000001"
	if _, err := accounts.BindInternalAccount(uuid, "retention_probe", "correct-password"); err != nil {
		t.Fatal(err)
	}
	gw := newCNAccountGateway(accounts)
	db, err := accounts.Database().OpenRead()
	if err != nil {
		t.Fatal(err)
	}
	counts := func() [4]int {
		t.Helper()
		var result [4]int
		if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM cn_local_account), (SELECT COUNT(*) FROM cn_account_snapshot), (SELECT COUNT(*) FROM cn_account_credentials), (SELECT COUNT(*) FROM cn_admin_audit)`).Scan(&result[0], &result[1], &result[2], &result[3]); err != nil {
			t.Fatal(err)
		}
		return result
	}
	want := counts()
	for batch := 0; batch < 3; batch++ {
		if batch > 0 {
			for i := 0; i < 24; i++ {
				path := "/local/account/login"
				body := `{"username":"retention_probe","password":"wrong-password"}`
				switch i % 4 {
				case 0:
					path = "/local/account/bind"
					body = fmt.Sprintf(`{"uuid":"d5750000-0000-4000-8000-%012d","username":"retention_probe","password":"new-password"}`, batch*100+i+2)
				case 2:
					body = `{"username":"no_such_account","password":"wrong-password"}`
				case 3:
					path = "/local/account/bind"
					body = `{"uuid":"invalid","username":"retention_probe","password":"new-password"}`
				}
				r := httptest.NewRequest("POST", path, strings.NewReader(body))
				r.Header.Set("X-Kairisei-Account", "1")
				r.RemoteAddr = fmt.Sprintf("127.0.0.%d:1234", i/6+1)
				w := httptest.NewRecorder()
				gw.ServeHTTP(w, r)
				if w.Code != 400 {
					t.Fatalf("failure path status=%d %s", w.Code, w.Body.String())
				}
			}
		}
		if got := counts(); got != want {
			t.Fatalf("failed request retained database state: %v -> %v", want, got)
		}
		if len(gw.workers) != 0 || len(gw.requests) > 4 || db.Stats().InUse != 0 {
			t.Fatal("failed request retained worker, address or database lease")
		}
		runtime.GC()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		t.Logf("after %d failures: heap_live=%d heap_objects=%d goroutines=%d address_buckets=%d database_rows=%v", batch*24, mem.HeapAlloc, mem.HeapObjects, runtime.NumGoroutine(), len(gw.requests), want)
	}
}

func TestAccountGatewayRequiresBoundedPrivateBody(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if err := accounts.AttachCreationPolicy(func() accountstore.CreationPolicy {
		return accountstore.CreationPolicy{DailyLimit: 10, VerificationBits: 0}
	}); err != nil {
		t.Fatal(err)
	}
	gateway := newCNAccountGateway(accounts)
	body := `{"uuid":"d4640000-0000-4000-8000-000000000003","username":"new_guest","password":"new-password"}`
	request := httptest.NewRequest("POST", "/local/account/bind", strings.NewReader(body))
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatal("unguarded browser bind accepted")
	}
	request = httptest.NewRequest("POST", "/local/account/bind", strings.NewReader(body))
	request.Header.Set("X-Kairisei-Account", "1")
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if strings.Contains(redactCNRequestBody(request.URL.Path, []byte(body)), "password") {
		t.Fatal("credential body recorded")
	}
	logPath := filepath.Join(t.TempDir(), "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	record := &recorder{path: logPath}
	request = httptest.NewRequest("POST", "/local/account/login", strings.NewReader(body))
	record.middleware(slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded, _ := io.ReadAll(r.Body)
		if string(forwarded) != body {
			t.Fatal("recording changed authentication input")
		}
	})).ServeHTTP(httptest.NewRecorder(), request)
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var entry capture
	if err := json.Unmarshal(logged, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.BodySHA != "" || entry.BodyBytes != 0 || strings.Contains(string(logged), "new-password") {
		t.Fatal("credential verifier leaked into request capture")
	}
	for i := 0; i < 15; i++ {
		request = httptest.NewRequest("POST", "/local/account/status", strings.NewReader(body))
		response = httptest.NewRecorder()
		gateway.ServeHTTP(response, request)
	}
	if response.Code != 429 {
		t.Fatal("account rate limit missing")
	}
}
