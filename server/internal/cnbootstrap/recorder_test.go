package cnbootstrap

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsBulkResourceRequest(t *testing.T) {
	tests := []struct {
		method string
		path   string
		want   bool
	}{
		{http.MethodGet, "/local/resources/cpk/data.cpk", true},
		{http.MethodGet, "//local/resources/cpk/data.cpk", true},
		{http.MethodHead, "/local/content/StandaloneWindows64/6000.3.26f1/current/CPK/cpk_file.csv", true},
		{http.MethodPost, "/local/resources/cpk/data.cpk", false},
		{http.MethodGet, "/HomeShow", false},
		{http.MethodGet, "//local/content/windows/bundle", true},
	}
	for _, test := range tests {
		request := &http.Request{Method: test.method, URL: &url.URL{Path: test.path}}
		if got := isBulkResourceRequest(request); got != test.want {
			t.Fatalf("isBulkResourceRequest(%s, %s) = %v, want %v", test.method, test.path, got, test.want)
		}
	}
}

func TestRecorderSkipsBulkResourcesAndDoesNotBlockOnCaptureFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	next := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})

	logPath := filepath.Join(t.TempDir(), "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	bulk := (&recorder{path: logPath}).middleware(logger)(next)
	bulkResponse := httptest.NewRecorder()
	bulk.ServeHTTP(bulkResponse, httptest.NewRequest(
		http.MethodGet,
		"http://local/local/resources/cpk/data.cpk",
		nil,
	))
	if bulkResponse.Code != http.StatusNoContent {
		t.Fatalf("bulk status = %d", bulkResponse.Code)
	}
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 0 {
		t.Fatalf("bulk request was captured: %q", content)
	}

	broken := (&recorder{path: filepath.Join(t.TempDir(), "missing", "requests.jsonl")}).middleware(logger)(next)
	brokenResponse := httptest.NewRecorder()
	broken.ServeHTTP(brokenResponse, httptest.NewRequest(
		http.MethodPost,
		"http://local/HomeShow",
		strings.NewReader(`{"probe":true}`),
	))
	if brokenResponse.Code != http.StatusNoContent {
		t.Fatalf("capture failure blocked request with status %d", brokenResponse.Code)
	}
}

func TestRecorderRedactsSessionBody(t *testing.T) {
	session := "local-cn-" + strings.Repeat("a", 64)
	redacted := redactCNSessionBody([]byte(session + `{"value":1}`))
	if strings.Contains(redacted, session) || redacted != `<session-redacted>{"value":1}` {
		t.Fatalf("redacted session body = %q", redacted)
	}
	login := `{"uuid":"00000000-0000-4000-8000-000000000001"}`
	if redacted := redactCNRequestBody("/loginSDK.php", []byte(login)); redacted != "<login-redacted>" || strings.Contains(redacted, "00000000") {
		t.Fatalf("redacted login body = %q", redacted)
	}
	if redacted := redactCNRequestBody("///loginSDK.php", []byte(login)); redacted != "<login-redacted>" {
		t.Fatalf("redacted repeated-slash login body = %q", redacted)
	}
}

func TestRecorderCredentialsContainRoutingOnly(t *testing.T) {
	for _, path := range []string{"/local/client/login", "//local/account/login", "///loginSDK.php", "/GiftCodeAd", "///GiftCodeAd"} {
		t.Run(path, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "requests.jsonl")
			if err := os.WriteFile(logPath, nil, 0600); err != nil {
				t.Fatal(err)
			}
			handler := (&recorder{path: logPath}).middleware(slog.New(slog.NewTextHandler(io.Discard, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if string(body) != "credential-secret" {
					t.Error("recorder changed request")
				}
			}))
			r := httptest.NewRequest(http.MethodPost, "http://local"+path+"?credential=secret", strings.NewReader("credential-secret"))
			r.Header.Set("X-Credential", "secret")
			handler.ServeHTTP(httptest.NewRecorder(), r)
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			var entry capture
			if err := json.Unmarshal(data, &entry); err != nil {
				t.Fatal(err)
			}
			expected := "<login-redacted>"
			if strings.HasSuffix(path, "GiftCodeAd") {
				expected = "<gift-code-redacted>"
			}
			if entry.Body != expected || entry.BodySHA != "" || entry.BodyBytes != 0 || entry.Query != "" || len(entry.Headers) != 0 {
				t.Fatalf("credentials leaked: %s", data)
			}
		})
	}
}
