package cnbootstrap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
)

// This is also the existing business/authentication/decompression input limit.
// Capture limits must not reduce the bytes accepted by those consumers.
const maxCapturedBody = 1024 * 1024

const (
	maxRecordedBody           = 16 * 1024
	maxCaptureRecord          = 128 * 1024
	maxRequestLogBytes  int64 = 5_000_000_000 // Active file plus one equal-sized backup: at most 10 GB (decimal).
	captureWarningDelay       = time.Minute
)

type capture struct {
	Timestamp     string              `json:"timestamp"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Route         string              `json:"route,omitempty"`
	Query         string              `json:"query,omitempty"`
	Remote        string              `json:"remote"`
	Headers       map[string][]string `json:"headers"`
	BodyBytes     int                 `json:"body_bytes"`
	BodySHA       string              `json:"body_sha256"`
	Body          string              `json:"body"`
	BodyOmitted   bool                `json:"body_omitted,omitempty"`
	BodyTruncated bool                `json:"body_truncated,omitempty"`
}

type recorder struct {
	path        string
	mu          sync.Mutex
	nextWarning time.Time
}

type captureAuthenticationKey struct{}
type captureAuthentication struct{ authenticated bool }

// Observe the established session only; never authenticate, read a body, change
// headers or decide whether the request can reach its business handler.
func markAuthenticatedCapture(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if state, ok := request.Context().Value(captureAuthenticationKey{}).(*captureAuthentication); ok {
			userID, authenticated := request.Context().Value(cnAuthenticatedUserKey{}).(int)
			state.authenticated = authenticated && userID > 0
		}
		next.ServeHTTP(w, request)
	})
}

func (r *recorder) middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/healthz" || isBulkResourceRequest(request) {
				next.ServeHTTP(writer, request)
				return
			}
			// Preserve the original pre-authentication limit and exact replay bytes.
			body, err := io.ReadAll(io.LimitReader(request.Body, maxCapturedBody+1))
			if err != nil {
				http.Error(writer, "request body read failed", http.StatusBadRequest)
				return
			}
			if len(body) > maxCapturedBody {
				http.Error(writer, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			request.Body = io.NopCloser(bytes.NewReader(body))
			state := &captureAuthentication{}
			request = request.WithContext(context.WithValue(request.Context(), captureAuthenticationKey{}, state))
			entry := capture{
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
				Method:    captureString(request.Method, 16),
				Path:      captureString(request.URL.Path, 512),
				Remote:    captureString(request.RemoteAddr, 128),
			}
			// The shared chi route context is populated during dispatch. Waiting
			// until completion distinguishes a known route from NotFound without
			// looking up routes early or changing authentication/error behavior.
			defer func() {
				if isCredentialRequest(request.URL.Path) || isGiftCodeRequest(request.URL.Path) {
					// No length/hash/query/headers: do not recreate a credential oracle.
					entry.Body, entry.BodyOmitted = redactCNRequestBody(request.URL.Path, nil), true
				} else {
					digest := sha256.Sum256(body)
					entry.BodyBytes, entry.BodySHA = len(body), hex.EncodeToString(digest[:])
					entry.Body, entry.BodyOmitted = "<body-omitted>", true
					if route := chi.RouteContext(request.Context()); route != nil && route.RoutePattern() != "" {
						entry.Route = captureString(route.RoutePattern(), 512)
						if state.authenticated {
							redacted := strings.ToValidUTF8(redactCNSessionBody(body), "\uFFFD")
							entry.Body = captureString(redacted, maxRecordedBody)
							entry.BodyTruncated = len(redacted) > len(entry.Body)
							entry.BodyOmitted = false
							entry.Headers = captureRequestHeaders(request.Header)
						}
					}
				}
				if err := r.append(entry); err != nil {
					r.warn(logger, err)
				}
			}()
			next.ServeHTTP(writer, request)
		})
	}
}

func captureString(value string, limit int) string {
	value = strings.ToValidUTF8(value, "\uFFFD")
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

func captureRequestHeaders(headers http.Header) map[string][]string {
	// Omit query strings and arbitrary headers, which may carry credentials.
	// Keep only bounded transport/client hints useful for protocol diagnosis.
	result := make(map[string][]string)
	for name, limit := range map[string]int{"Content-Type": 256, "Content-Encoding": 64, "Accept-Encoding": 256, "User-Agent": 512} {
		if value := headers.Get(name); value != "" {
			result[name] = []string{captureString(value, limit)}
		}
	}
	return result
}

func redactCNSessionBody(body []byte) string {
	_, payload, ok := splitCNSessionPayload(body)
	if !ok {
		return string(body)
	}
	return "<session-redacted>" + string(payload)
}

func isCredentialRequest(path string) bool {
	path = "/" + strings.TrimLeft(path, "/")
	return path == "/loginSDK.php" || path == "/local/client/login" || strings.HasPrefix(path, "/local/account/")
}

func isGiftCodeRequest(path string) bool { return "/"+strings.TrimLeft(path, "/") == "/GiftCodeAd" }

func redactCNRequestBody(path string, body []byte) string {
	if isGiftCodeRequest(path) {
		return "<gift-code-redacted>"
	}
	if isCredentialRequest(path) {
		return "<login-redacted>"
	}
	return redactCNSessionBody(body)
}

func (r *recorder) append(entry capture) error {
	return r.appendWithLimit(entry, maxRequestLogBytes)
}

// Keep the same rotation path testable without creating production-sized logs.
func (r *recorder) appendWithLimit(entry capture, logBytes int64) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxCaptureRecord {
		return fmt.Errorf("request capture exceeds %d bytes", maxCaptureRecord)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// The configured path and its .1 sibling are the only managed capture files.
	// Drop pre-existing oversized captures instead of moving unbounded legacy
	// data into the backup and leaving the disk-usage limit ineffective.
	backup := r.path + ".1"
	if info, err := os.Stat(backup); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("request capture backup is not a regular file")
		}
		if info.Size() > logBytes {
			if err := os.Remove(backup); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	info, err := os.Stat(r.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("request capture is not a regular file")
	}
	flags := os.O_WRONLY | os.O_APPEND
	if os.IsNotExist(err) {
		// Recover if creation after a successful rotation previously failed.
		flags |= os.O_CREATE
	} else if info.Size() > logBytes {
		flags = os.O_WRONLY | os.O_TRUNC
	} else if info.Size()+int64(len(data)) > logBytes {
		if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(r.path, backup); err != nil {
			return err
		}
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	file, err := os.OpenFile(r.path, flags, 0o600)
	if err != nil {
		return err
	}
	if n, err := file.Write(data); err != nil || n != len(data) {
		_ = file.Close()
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	// Diagnostic evidence needs ordinary close durability, not per-request fsync.
	return file.Close()
}

func (r *recorder) warn(logger *slog.Logger, err error) {
	r.mu.Lock()
	now := time.Now()
	if now.Before(r.nextWarning) {
		r.mu.Unlock()
		return
	}
	r.nextWarning = now.Add(captureWarningDelay)
	r.mu.Unlock()
	logger.Warn("skip failed request capture", "error", err)
}

func isBulkResourceRequest(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	path := "/" + strings.TrimLeft(request.URL.Path, "/")
	return strings.HasPrefix(path, "/local/content/") || strings.HasPrefix(path, "/local/resources/")
}
