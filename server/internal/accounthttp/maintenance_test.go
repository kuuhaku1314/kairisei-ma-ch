package accounthttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
)

func TestMaintenanceWaitsForAccountWritesAndInvalidatesCache(t *testing.T) {
	builds := 0
	r := New(Config{IdleLimit: 32, Build: func(int) (http.Handler, error) {
		builds++
		return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil
	}})
	r.ServeHTTP(httptest.NewRecorder(), primaryAccountRequest(http.MethodPost, "/", nil))
	lock := r.AccountLock(accountstore.PrimaryUserID)
	lock.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if err := r.WithAccountMaintenance(ctx, func() error { t.Error("entered while an account was writing"); return nil }); err == nil {
		t.Fatal("wait not cancelled")
	}
	lock.Unlock()
	if err := r.WithAccountMaintenance(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	r.ServeHTTP(httptest.NewRecorder(), primaryAccountRequest(http.MethodPost, "/", nil))
	if builds != 2 {
		t.Fatal("cached pre-cleanup account was reused", builds)
	}
	r.InvalidateAll()
}
