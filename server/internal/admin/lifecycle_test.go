package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/objectstore"
)

func TestClosingAdminCancelsAndWaitsForInFlightImageRequest(t *testing.T) {
	reached := make(chan struct{})
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(reached)
		<-r.Context().Done()
	}))
	defer storage.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	done := make(chan struct{})
	job := &imageSyncJob{Mode: "check", State: "running", Total: 1, Results: map[string]imageSyncResult{}}
	state := &imageSyncState{job: job, cancel: cancel, done: done}
	api := &API{imageSync: state, homeBannerDir: t.TempDir()}
	credentials := objectstore.Credentials{EndpointURL: storage.URL, Bucket: "bucket", Region: "auto", AddressingStyle: "path", AccessKeyID: "test-access", SecretAccessKey: "test-secret"}
	go func() {
		defer close(done)
		api.runImageSync(ctx, job, []imageSyncImage{{Path: "home-banners/" + strings.Repeat("a", 64) + ".png", Local: true, Bytes: 100}}, "", credentials)
	}()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("storage request never started")
	}
	closed := make(chan struct{})
	go func() { _ = (&lifecycleHandler{images: state}).Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("admin close did not cancel and drain image sync")
	}
	if job := state.snapshot(); job.State != "cancelled" || job.Done != 1 {
		t.Fatalf("unfinished cancelled task: %+v", job)
	}
	// Closing an idle/already closed handler is safe and cannot reopen admission.
	if err := (&lifecycleHandler{images: state}).Close(); err != nil || !state.closed {
		t.Fatal("close not idempotent", err)
	}
}
