package cnbootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/wirecompression"
)

func readBoundedCaptures(t *testing.T, path string) []capture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries []capture
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if len(line)+1 > maxCaptureRecord {
			t.Fatalf("capture record exceeded serialized bound: %d", len(line))
		}
		var entry capture
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func TestRecorderAnonymousUploadsAndActualSessionRouteGating(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	fixture := newTestHandler(t, root, path).(testSessionHandler)
	unknown := strings.Repeat("anonymous-body-secret", maxCapturedBody/21)
	for i := 0; i < 5; i++ {
		response := httptest.NewRecorder()
		fixture.Handler.ServeHTTP(response, httptest.NewRequest("POST", "/unimplemented.php?secret=hidden", strings.NewReader(unknown)))
		if response.Code != http.StatusNotImplemented {
			t.Fatalf("unknown route changed status: %d", response.Code)
		}
	}
	entries := readBoundedCaptures(t, path)
	data, _ := os.ReadFile(path)
	if len(entries) != 5 || len(data) > 8*1024 || bytes.Contains(data, []byte("anonymous-body-secret")) {
		t.Fatalf("anonymous uploads still retain payload: entries=%d bytes=%d", len(entries), len(data))
	}
	for _, entry := range entries {
		if !entry.BodyOmitted || entry.BodyBytes != len(unknown) || entry.BodySHA == "" || entry.Query != "" || len(entry.Headers) != 0 {
			t.Fatalf("anonymous summary incorrect: %+v", entry)
		}
	}
	for _, test := range []struct {
		path, body string
		omitted    bool
	}{
		{"/Connect", fixture.sessionKey + "{}", false},
		{"/Unimplemented", fixture.sessionKey + "{}", true},
		{"/Connect", "local-cn-" + strings.Repeat("0", 64) + "{}", true},
		{"/Connect", "unauthenticated-payload", true},
	} {
		request := httptest.NewRequest("POST", test.path+"?secret=query", strings.NewReader(test.body))
		request.Header.Set(accounthttp.AccountUserHeader, "1000001")
		fixture.Handler.ServeHTTP(httptest.NewRecorder(), request)
		entries := readBoundedCaptures(t, path)
		last := entries[len(entries)-1]
		if last.BodyOmitted != test.omitted || strings.Contains(last.Body, fixture.sessionKey) || last.Query != "" {
			t.Fatalf("capture did not follow actual authentication and known route: %+v", last)
		}
		if !test.omitted && (last.Body != "<session-redacted>{}" || !strings.HasPrefix(last.Route, "/Connect")) {
			t.Fatalf("authenticated protocol diagnostic missing: %+v", last)
		}
	}
}

func TestRecorderBoundedAuthenticatedBodyPreservesCompressionAndBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := chi.NewRouter()
	router.Use(cnNetworkCompression(logger))
	router.Use((&recorder{path: path}).middleware(logger))
	// Authentication has separate real-route coverage above. Here only replace
	// account resolution, allowing arbitrary full-limit protocol probe bodies.
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), cnAuthenticatedUserKey{}, 1000001)))
		})
	})
	router.Use(markAuthenticatedCapture)
	var expected []byte
	router.Post("/CaptureProbe", func(w http.ResponseWriter, r *http.Request) {
		actual, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Error("capture changed business request bytes")
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(actual)
	})
	for _, payload := range []string{strings.Repeat("繁體日本語", maxRecordedBody/len("繁體日本語")+1), strings.Repeat("\x01", maxRecordedBody+100), "short"} {
		wirePayload := `{"probe":"` + payload + `"}`
		expected = []byte("local-cn-" + strings.Repeat("a", 64) + wirePayload)
		packed, err := wirecompression.Compress(expected)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "/CaptureProbe?password=hidden", bytes.NewReader(packed))
		request.Header.Set("Content-Encoding", "gzip")
		request.Header.Set("Accept-Encoding", "gzip")
		request.Header.Set("User-Agent", strings.Repeat("u", 9000))
		for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization", "X-Credential", accounthttp.AccountUserHeader} {
			request.Header.Set(name, "secret-header")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		actual := response.Body.Bytes()
		if response.Header().Get("Content-Encoding") == "gzip" {
			actual, err = wirecompression.Decompress(actual, maxCapturedBody)
		}
		if err != nil || response.Code != 200 || !bytes.Equal(actual, expected) {
			t.Fatal("capture changed compressed response or request", response.Code, err)
		}
		entries := readBoundedCaptures(t, path)
		last := entries[len(entries)-1]
		digest := sha256.Sum256(expected)
		if last.BodyOmitted || len(last.Body) > maxRecordedBody || !utf8.ValidString(last.Body) ||
			last.BodyBytes != len(expected) || last.BodySHA != hex.EncodeToString(digest[:]) ||
			last.BodyTruncated != (len("<session-redacted>"+wirePayload) > maxRecordedBody) || last.Query != "" {
			t.Fatalf("bounded capture did not preserve raw size/hash and truncation: %+v", last)
		}
		encoded, _ := json.Marshal(last)
		if bytes.Contains(encoded, []byte("secret-header")) || strings.Contains(last.Body, "local-cn-") || len(http.Header(last.Headers).Get("User-Agent")) > 512 {
			t.Fatal("session/headers leaked or retained unbounded metadata")
		}
	}
}

func TestRecorderRotationBoundsConcurrencyAndLegacyRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	const logBytes int64 = 32 * 1024
	entry := capture{Path: "/Connect", Body: strings.Repeat("p", 256), BodyBytes: maxCapturedBody}
	encoded, _ := json.Marshal(entry)
	// Fill the first segment with complete JSONL, then exercise rotation while
	// many requests contend for the same recorder. Every accepted line survives.
	line := append(encoded, '\n')
	prefill := bytes.Repeat(line, int((logBytes-int64(len(line)))/int64(len(line))))
	if err := os.WriteFile(path, prefill, 0600); err != nil {
		t.Fatal(err)
	}
	record := &recorder{path: path}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := record.appendWithLimit(entry, logBytes); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var total int64
	var count int
	for _, name := range []string{path, path + ".1"} {
		info, err := os.Stat(name)
		if err != nil || info.Size() > logBytes {
			t.Fatal("rotation segment exceeded size bound", name, err)
		}
		total += info.Size()
		count += len(readBoundedCaptures(t, name))
	}
	if total > 2*logBytes || count != len(prefill)/len(line)+64 {
		t.Fatal("concurrent rotation lost/duplicated records or exceeded total limit", total, count)
	}
	for _, name := range []string{path, path + ".1"} {
		if err := os.Truncate(name, 3*logBytes); err != nil {
			t.Fatal(err)
		}
	}
	// Restart must enforce the cap on old, already-oversized files too.
	if err := (&recorder{path: path}).appendWithLimit(capture{Path: "/after-restart"}, logBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("oversized old backup was retained", err)
	}
	if entries := readBoundedCaptures(t, path); len(entries) != 1 || entries[0].Path != "/after-restart" {
		t.Fatal("oversized old active capture was retained")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := (&recorder{path: path}).appendWithLimit(capture{Path: "/recovered"}, logBytes); err != nil {
		t.Fatal("capture could not recover a missing active file", err)
	}
}

func TestRecorderReadLimitAndCaptureFailureLogBound(t *testing.T) {
	var warnings bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&warnings, nil))
	record := &recorder{path: filepath.Join(t.TempDir(), "missing", "requests.jsonl")}
	forwarded := 0
	handler := record.middleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		if len(body) != maxCapturedBody {
			t.Error("original body limit changed")
		}
		forwarded++
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := 0; i < 16; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("POST", "/Unknown", strings.NewReader(strings.Repeat("x", maxCapturedBody))))
		if response.Code != 204 {
			t.Fatal("capture IO failure changed business response")
		}
	}
	if forwarded != 16 || strings.Count(warnings.String(), "skip failed request capture") != 1 || strings.Contains(warnings.String(), "captured CN request") {
		t.Fatal("capture failures emitted per-request logs or blocked forwarding", forwarded, warnings.String())
	}
	record.nextWarning = time.Now().Add(-time.Minute)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/Unknown", strings.NewReader(strings.Repeat("x", maxCapturedBody))))
	if strings.Count(warnings.String(), "skip failed request capture") != 2 {
		t.Fatal("bounded error reporting did not recover after its interval")
	}
	tooLarge := httptest.NewRecorder()
	handler.ServeHTTP(tooLarge, httptest.NewRequest("POST", "/Unknown", strings.NewReader(strings.Repeat("x", maxCapturedBody+1))))
	if tooLarge.Code != 413 || forwarded != 17 {
		t.Fatal("original 1MiB rejection boundary changed")
	}
	broken := httptest.NewRequest("POST", "/Unknown", nil)
	broken.Body = io.NopCloser(errorCaptureReader{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, broken)
	if response.Code != 400 || forwarded != 17 {
		t.Fatal("original read-error boundary changed")
	}
}

type errorCaptureReader struct{}

func (errorCaptureReader) Read([]byte) (int, error) { return 0, errors.New("synthetic read failure") }
