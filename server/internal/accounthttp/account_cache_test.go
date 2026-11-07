package accounthttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
)

func TestAccountCacheReloadsDurableStateAfterUnsuccessfulMutation(t *testing.T) {
	for _, failure := range []string{"persist", "validation", "panic", "protocol"} {
		t.Run(failure, func(t *testing.T) {
			durable, builds := 100, 0
			build := func(int) (http.Handler, error) {
				builds++
				cached := durable
				outcome := &cacheOutcome{retain: true}
				outcome.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/fail" {
						cached -= 10
						switch failure {
						case "protocol":
							outcome.retain = false
							fmt.Fprint(w, `{"res_code":-1}`)
						case "panic":
							panic("failed mutation")
						case "persist":
							http.Error(w, "database unavailable", http.StatusInternalServerError)
						default:
							http.Error(w, "invalid final item", http.StatusBadRequest)
						}
						return
					}
					if r.URL.Path == "/commit" {
						cached--
						durable = cached
					}
					fmt.Fprint(w, cached)
				})
				return outcome, nil
			}
			router := New(Config{Build: build, IdleLimit: idleAccountCacheLimit})
			func() {
				defer func() {
					if caught := recover(); (caught != nil) != (failure == "panic") {
						t.Fatalf("unexpected panic: %v", caught)
					}
				}()
				router.ServeHTTP(httptest.NewRecorder(), primaryAccountRequest(http.MethodPost, "/fail", nil))
			}()
			for _, path := range []string{"/commit", "/read"} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, primaryAccountRequest(http.MethodPost, path, nil))
				if response.Code != http.StatusOK || response.Body.String() != "99" || durable != 99 {
					t.Fatalf("%s retained failed mutation: status=%d body=%s durable=%d", path, response.Code, response.Body.String(), durable)
				}
			}
			if builds != 2 {
				t.Fatalf("built %d handlers; want one reload, then successful cache reuse", builds)
			}
			snapshot := router.ActivitySnapshot()
			wantHTTPErrors := int64(1)
			if failure == "protocol" {
				wantHTTPErrors = 0
			}
			if snapshot.Requests != 3 || snapshot.Errors != wantHTTPErrors || len(snapshot.Players) != 1 {
				t.Fatalf("activity request/unique player counters: %+v", snapshot)
			}
			old := snapshot.Players[0]
			old.LastRequest = time.Now().Add(-ActivityWindow)
			router.activity[old.UserID] = old
			if len(router.ActivitySnapshot().Players) != 0 {
				t.Fatal("inactive player remained in online estimate")
			}
		})
	}
}

func TestAccountCacheEvictionPreservesActiveRequests(t *testing.T) {
	router := New(Config{IdleLimit: idleAccountCacheLimit, Build: func(int) (http.Handler, error) {
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil
	}})
	t.Cleanup(func() {
		for id := range router.handlers {
			router.Invalidate(id)
		}
	})
	firstID := accountstore.PrimaryUserID
	lock := router.AccountLock(firstID)
	active, err := router.acquireHandler(firstID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= idleAccountCacheLimit+12; i++ {
		id := firstID + i
		entry, err := router.acquireHandler(id)
		if err != nil {
			t.Fatal(err)
		}
		router.releaseHandler(id, entry, false)
	}
	if len(router.handlers) != idleAccountCacheLimit+1 || router.handlers[firstID] != active || router.handlers[firstID+1] != nil {
		t.Fatal("cache did not bound idle entries while retaining the active request")
	}
	active.lastUsed = time.Now().Add(-2 * idleAccountCacheTTL)
	router.expireHandler(firstID, active)
	if router.handlers[firstID] != active {
		t.Fatal("expiry evicted active request")
	}
	router.releaseHandler(firstID, active, false)
	router.expireHandler(firstID, active)
	if router.handlers[firstID] != active {
		t.Fatal("old timer evicted refreshed entry")
	}
	active.lastUsed = time.Now().Add(-2 * idleAccountCacheTTL)
	router.expireHandler(firstID, active)
	if router.handlers[firstID] != nil {
		t.Fatal("idle entry did not expire")
	}
	replacement, err := router.acquireHandler(firstID)
	if err != nil {
		t.Fatal(err)
	}
	router.releaseHandler(firstID, replacement, false)
	router.expireHandler(firstID, active)
	if router.handlers[firstID] != replacement || router.AccountLock(firstID) != lock {
		t.Fatal("old timer or eviction changed replacement identity/transaction lock")
	}
}

func TestConfiguredAccountCacheLimit(t *testing.T) {
	for _, value := range []string{"", "0", "2", "32", "128", "-1", "bad", "4097"} {
		t.Setenv("KAIRI_ACCOUNT_CACHE_LIMIT", value)
		limit, err := ConfiguredAccountCacheLimit()
		invalid := value == "-1" || value == "bad" || value == "4097"
		if (err != nil) != invalid {
			t.Fatalf("%q: %v", value, err)
		}
		if value == "" && limit != 32 {
			t.Fatalf("default %d", limit)
		}
	}
}

type cacheOutcome struct {
	http.Handler
	retain bool
}

func (h *cacheOutcome) RetainAccountCache() bool { return h.retain }

func TestCancelledAccountRequestDoesNotEnterAfterWaiting(t *testing.T) {
	built := false
	router := New(Config{Build: func(int) (http.Handler, error) {
		built = true
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil
	}})
	lock := router.AccountLock(accountstore.PrimaryUserID)
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		router.ServeHTTP(httptest.NewRecorder(), primaryAccountRequest(http.MethodPost, "/mutate", nil).WithContext(ctx))
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled request still waits for account")
	}
	if built {
		t.Fatal("cancelled request constructed account")
	}
}
