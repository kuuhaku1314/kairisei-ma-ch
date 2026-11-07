package cnbootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/cpk"
)

type resourceDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *resourceDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

func TestDownloadsExtendWriteDeadlineWithoutChangingRanges(t *testing.T) {
	payload := []byte("registered-file")
	path := filepath.Join(t.TempDir(), "asset.bin")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"platform", "cpk", "card"} {
		t.Run(kind, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Range", "bytes=2-5")
			route := chi.NewRouteContext()
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
			w := &resourceDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
			before := time.Now()
			switch kind {
			case "platform":
				servePlatformFile(w, r, "asset.bin", platformDeliveryFile{path: path, size: int64(len(payload))}, "application/octet-stream")
			case "cpk":
				route.URLParams.Add("*", "CPK/CueSheet_BGM.cpk")
				cnBootstrapCPKResource([]cpk.File{{Name: "CueSheet_BGM.cpk", SourcePath: path, Size: 52 * 1024 * 1024}})(w, r)
			case "card":
				route.URLParams.Add("*", "test/chr51/example.png")
				images := cardImages{Namespace: "test", Files: map[string]cardImage{"chr51/example.png": {Bytes: int64(len(payload)), SourcePath: path}}}
				images.serveHTTP(w, r)
			}
			if w.Code != http.StatusPartialContent || w.Body.String() != string(payload[2:6]) {
				t.Fatalf("range changed: %d %q", w.Code, w.Body.String())
			}
			want := time.Minute
			if kind == "cpk" {
				want += 52 * 16 * time.Second
			}
			if w.deadline.Before(before.Add(want)) || w.deadline.After(time.Now().Add(want+time.Second)) {
				t.Fatalf("wrong write allowance: %v", w.deadline.Sub(before))
			}
		})
	}
}
