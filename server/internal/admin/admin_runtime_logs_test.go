package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeLogPagingAndFilters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stdout.log")
	var lines strings.Builder
	for i := range 102 {
		fmt.Fprintf(&lines, "{\"time\":\"2026-11-06T10:00:00Z\",\"level\":\"INFO\",\"msg\":\"row %d\",\"path\":\"/HomeShow\",\"status\":200,\"elapsed_ms\":%d}\n", i, i)
	}
	lines.WriteString(strings.Repeat("x", 70*1024) + "\n")
	lines.WriteString("{\"time\":\"2026-11-06T10:00:01Z\",\"level\":\"WARN\",\"msg\":\"partial")
	if err := os.WriteFile(path, []byte(lines.String()), 0600); err != nil {
		t.Fatal(err)
	}
	admin := &API{runtimeLog: &runtimeLogReader{path: path}}
	handler := adminSecurityHeaders(http.HandlerFunc(admin.getRuntimeLogs))
	get := func(query string, status int) runtimeLogPage {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://localhost/api/runtime-logs"+query, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: HTTP %d, %s", query, w.Code, w.Body.String())
		}
		var page runtimeLogPage
		if status == http.StatusOK {
			if err := json.NewDecoder(w.Body).Decode(&page); err != nil {
				t.Fatal(err)
			}
		}
		return page
	}
	first := get("", 200)
	if !first.Available || len(first.Entries) != 100 || first.Entries[0].Message != "row 101" ||
		first.Entries[99].Message != "row 2" || first.NextCursor == "" {
		t.Fatalf("unexpected first page size %d or order", len(first.Entries))
	}
	// Finish the in-progress write after the first page's snapshot.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("\"}\n")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("append: %v / %v", writeErr, closeErr)
	}
	cursorQuery := "?cursor=" + url.QueryEscape(first.NextCursor)
	second := get(cursorQuery, 200)
	if len(second.Entries) != 2 || second.Entries[0].Message != "row 1" ||
		second.Entries[1].Message != "row 0" || second.NextCursor != "" {
		t.Fatalf("unexpected second page: %+v", second)
	}
	matching := get("?q=ROW&level=info&path=%2FHomeShow&status=200&min_elapsed_ms=100&from=2026-11-06T10:00:00Z&to=2026-11-06T10:00:01Z", 200)
	if len(matching.Entries) != 2 || matching.Entries[0].Message != "row 101" {
		t.Fatalf("unexpected filtered page: %+v", matching)
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	get(cursorQuery, 409)
}

func TestRuntimeLogBudgetResumesMidRecord(t *testing.T) {
	target := "{\"time\":\"2026-11-06T10:00:00Z\",\"level\":\"INFO\",\"msg\":\"wanted\"}\n"
	tail := strings.Repeat("x", 64*1024-1) + "\n"
	log := strings.NewReader(target + tail)
	budget := int64(len(tail) + 20)
	var first runtimeLogPage
	next, err := scanRuntimeLogRange(context.Background(), log, log.Size(), 0, budget, runtimeLogQuery{}, &first)
	if err != nil || first.ScannedBytes != budget || len(first.Entries) != 0 || next != int64(len(target)) {
		t.Fatalf("unexpected scan budget boundary: %+v, next=%d (%v)", first, next, err)
	}
	var second runtimeLogPage
	next, err = scanRuntimeLogRange(context.Background(), log, next, 0, budget, runtimeLogQuery{}, &second)
	if err != nil || next != 0 || len(second.Entries) != 1 || second.Entries[0].Message != "wanted" {
		t.Fatalf("boundary record did not survive: %+v, next=%d (%v)", second, next, err)
	}
}

func TestRuntimeLogExtraFields(t *testing.T) {
	line := `{"time":"2026-11-06T10:00:00Z","level":"WARN","msg":"client frame failed",` +
		`"remote":"192.168.2.241:38382","room_id":6020000205,"channel":"\u0062attle",` +
		`"session_token":"private-token","user_id":{"password":"hidden"},"error":"read tcp"}`
	search := func(q string) runtimeLogQuery {
		query, err := parseRuntimeLogQuery(url.Values{"q": {q}})
		if err != nil {
			t.Fatal(err)
		}
		return query
	}
	var page runtimeLogPage
	appendRuntimeLogEntry([]byte(line), search("6020000205"), &page)
	want := []runtimeLogField{{"remote", "192.168.2.241:38382"}, {"room_id", "6020000205"}, {"channel", "battle"}}
	if len(page.Entries) != 1 || fmt.Sprint(page.Entries[0].Fields) != fmt.Sprint(want) {
		t.Fatalf("unexpected extra fields: %+v", page.Entries)
	}
	for _, hidden := range []string{"remote", "private-token", "hidden"} {
		var miss runtimeLogPage
		appendRuntimeLogEntry([]byte(line), search(hidden), &miss)
		if len(miss.Entries) != 0 {
			t.Fatalf("search matched a field name or hidden value: %+v", miss.Entries)
		}
	}
	page.Entries = nil
	appendRuntimeLogEntry([]byte(line), search("battle"), &page)
	if len(page.Entries) != 1 {
		t.Fatal("escaped diagnostic value was skipped")
	}
	query, err := parseRuntimeLogQuery(url.Values{"user_id": {"1000002"}, "request_id": {"req-1"}})
	if err != nil {
		t.Fatal(err)
	}
	page.Entries = nil
	for _, fields := range []string{
		`"request_id":"req-1","user_id":1000002`,
		`"request_id":"req-1","user_id":"1000002"`,
		`"request_id":"req-1","user_id":10000020`,
		`"request_id":"req-1","owner_user_id":1000002`,
		`"request_id":"req-1","user_id":{"id":1000002}`,
		`"request_id":"req-2","user_id":1000002`,
	} {
		appendRuntimeLogEntry([]byte(`{"time":"2026-11-06T10:00:00Z","level":"INFO","msg":"http request",`+fields+`}`), query, &page)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected exact numeric and string UID matches, got %+v", page.Entries)
	}
	for _, entry := range page.Entries {
		if len(entry.Fields) != 1 || entry.Fields[0] != (runtimeLogField{"user_id", "1000002"}) {
			t.Fatalf("UID missing from diagnostics: %+v", entry)
		}
	}
	for _, invalid := range []string{"0", "-1", "1.5", "9223372036854775808"} {
		if _, err := parseRuntimeLogQuery(url.Values{"user_id": {invalid}}); err == nil {
			t.Fatalf("accepted invalid UID %q", invalid)
		}
	}
}

func TestRuntimeLogTimeSeekWithReorderedRows(t *testing.T) {
	base := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	from, to := base.Add(3*time.Hour), base.Add(3*time.Hour+125*time.Second)
	var body strings.Builder
	var want []string
	for i := range 72000 {
		stamp := base.Add(time.Duration(i) * time.Second)
		if i%11 == 0 {
			stamp = stamp.Add(-5 * time.Second)
		}
		message := fmt.Sprintf("row %d", i)
		fmt.Fprintf(&body, "{\"time\":%q,\"level\":\"INFO\",\"msg\":%q}\n", stamp.Format(time.RFC3339), message)
		if !stamp.Before(from) && stamp.Before(to) {
			want = append(want, message)
		}
	}
	path := filepath.Join(t.TempDir(), "stdout.log")
	if err := os.WriteFile(path, []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	admin := &API{runtimeLog: &runtimeLogReader{path: path}}
	values := url.Values{"from": {from.Format(time.RFC3339)}, "to": {to.Format(time.RFC3339)}}
	var got []string
	var total int64
	for range 3 {
		w := httptest.NewRecorder()
		admin.getRuntimeLogs(w, httptest.NewRequest(http.MethodGet, "/api/runtime-logs?"+values.Encode(), nil))
		var page runtimeLogPage
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &page) != nil {
			t.Fatalf("time seek returned %d: %s", w.Code, w.Body.String())
		}
		total += page.ScannedBytes
		for _, entry := range page.Entries {
			got = append(got, entry.Message)
		}
		if page.NextCursor == "" {
			break
		}
		values.Set("cursor", page.NextCursor)
	}
	if total >= int64(body.Len()/2) || len(got) != len(want) {
		t.Fatalf("scanned %d / %d bytes, returned %d / %d rows", total, body.Len(), len(got), len(want))
	}
	for i, message := range got {
		if message != want[len(want)-1-i] {
			t.Fatalf("row %d differs: %q", i, message)
		}
	}
	t.Logf("scanned %d of %d bytes across two pages", total, body.Len())
}
