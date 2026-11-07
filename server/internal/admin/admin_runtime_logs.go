package admin

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	runtimeLogPageSize   = 100
	runtimeLogBudget     = 32 * 1024 * 1024
	runtimeLogTextLimit  = 2048
	runtimeLogFieldLimit = 32
	runtimeLogFieldText  = 512
	// Timestamp assignment and serialized writes can be reordered under concurrency.
	runtimeLogTimeMargin = 10 * time.Second
)

type runtimeLogReader struct {
	path       string
	mu         sync.Mutex
	file       os.FileInfo
	generation string
}

type runtimeLogPage struct {
	State        string            `json:"state"`
	Available    bool              `json:"available"`
	Entries      []runtimeLogEntry `json:"entries"`
	ScannedBytes int64             `json:"scanned_bytes"`
	NextCursor   string            `json:"next_cursor"`
}

type runtimeLogEntry struct {
	Time      time.Time `json:"time"`
	Level     string    `json:"level"`
	Message   string    `json:"msg"`
	Method    string    `json:"method,omitempty"`
	Path      string    `json:"path,omitempty"`
	Status    int       `json:"status,omitempty"`
	RequestID string    `json:"request_id,omitempty"`
	ElapsedMS *int64    `json:"elapsed_ms,omitempty"`
	Error     string    `json:"error,omitempty"`
	// Fields keeps the record's other attributes in written order, such as room_id or remote.
	Fields []runtimeLogField `json:"fields,omitempty"`
}

// runtimeLogField is display text: JSON strings are unquoted, other values keep their JSON form.
type runtimeLogField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type runtimeLogQuery struct {
	from, to                       time.Time
	level, path, requestID, search string
	status                         int
	userID                         int64
	minElapsed                     *int64
	// rawSearch is the search as it appears inside a JSON log line, or nil when escaping could hide it.
	rawSearch []byte
}

func parseRuntimeLogQuery(values url.Values) (runtimeLogQuery, error) {
	query := runtimeLogQuery{
		level:     strings.ToUpper(values.Get("level")),
		path:      values.Get("path"),
		requestID: values.Get("request_id"),
		search:    strings.ToLower(strings.TrimSpace(values.Get("q"))),
	}
	if len(query.search) > 256 || len(query.path) > 2048 || len(query.requestID) > 256 {
		return query, errors.New("日志筛选条件过长")
	}
	escaped := func(r rune) bool {
		return r < 0x20 || r == '"' || r == '\\' || r == '\u2028' || r == '\u2029' || r == utf8.RuneError
	}
	if query.search != "" && strings.IndexFunc(query.search, escaped) < 0 {
		query.rawSearch = []byte(query.search)
	}
	switch query.level {
	case "", "DEBUG", "INFO", "WARN", "ERROR":
	default:
		return query, errors.New("无效的日志级别")
	}
	if value := values.Get("status"); value != "" {
		status, err := strconv.Atoi(value)
		if err != nil || status < 100 || status > 599 {
			return query, errors.New("无效的 HTTP 状态码")
		}
		query.status = status
	}
	if value := strings.TrimSpace(values.Get("user_id")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 || value[0] == '+' {
			return query, errors.New("玩家 UID 须为正整数")
		}
		query.userID = id
	}
	if value := values.Get("min_elapsed_ms"); value != "" {
		elapsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || elapsed < 0 {
			return query, errors.New("业务耗时下限须为非负整数")
		}
		query.minElapsed = &elapsed
	}
	for _, bound := range []struct {
		key    string
		target *time.Time
	}{{"from", &query.from}, {"to", &query.to}} {
		if value := values.Get(bound.key); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return query, fmt.Errorf("日志时间 %s 须为 RFC3339 时间", bound.key)
			}
			*bound.target = parsed
		}
	}
	if !query.from.IsZero() && !query.to.IsZero() && !query.from.Before(query.to) {
		return query, errors.New("日志起始时间须早于结束时间")
	}
	return query, nil
}

// filters checks everything except the text search, which also looks at the extra fields.
func (query runtimeLogQuery) filters(row runtimeLogEntry) bool {
	return !(row.Time.IsZero() || row.Level == "" ||
		(query.level != "" && row.Level != query.level) ||
		(query.path != "" && row.Path != query.path) ||
		(query.requestID != "" && row.RequestID != query.requestID) ||
		(query.status != 0 && row.Status != query.status) ||
		(query.minElapsed != nil && (row.ElapsedMS == nil || *row.ElapsedMS < *query.minElapsed)) ||
		(!query.from.IsZero() && row.Time.Before(query.from)) ||
		(!query.to.IsZero() && !row.Time.Before(query.to)))
}

func (query runtimeLogQuery) matchesText(row runtimeLogEntry) bool {
	if query.search == "" {
		return true
	}
	for _, value := range [...]string{row.Message, row.Error, row.Path, row.RequestID} {
		if strings.Contains(strings.ToLower(value), query.search) {
			return true
		}
	}
	return false
}

func (query runtimeLogQuery) matchesFields(fields []runtimeLogField) bool {
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field.Value), query.search) {
			return true
		}
	}
	return false
}

// mayContain is a cheap necessary condition before decoding a line's extra fields for the search.
func (query runtimeLogQuery) mayContain(line []byte) bool {
	return query.rawSearch == nil || bytes.IndexByte(line, '\\') >= 0 || bytes.Contains(bytes.ToLower(line), query.rawSearch)
}

func boundRuntimeLogText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	end := limit - len("…")
	for !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + "…"
}

// Limit the display fields separately from the maximum accepted log line.
func (row *runtimeLogEntry) boundDisplayText() {
	for _, field := range []*string{&row.Level, &row.Message, &row.Method, &row.Path, &row.RequestID, &row.Error} {
		*field = boundRuntimeLogText(*field, runtimeLogTextLimit)
	}
}

func (admin *API) getRuntimeLogs(w http.ResponseWriter, r *http.Request) {
	log := admin.runtimeLog
	page := runtimeLogPage{State: "PASS", Entries: make([]runtimeLogEntry, 0)}
	if log == nil {
		WriteAdminJSON(w, http.StatusOK, page)
		return
	}
	values := r.URL.Query()
	query, err := parseRuntimeLogQuery(values)
	if err != nil {
		WriteAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !log.mu.TryLock() {
		w.Header().Set("Retry-After", "1")
		WriteAdminError(w, http.StatusTooManyRequests, "已有日志检索正在进行")
		return
	}
	defer log.mu.Unlock()

	file, err := os.Open(log.path)
	if err != nil {
		WriteAdminError(w, http.StatusServiceUnavailable, "当前运行日志暂不可读取")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		WriteAdminError(w, http.StatusServiceUnavailable, "当前运行日志暂不可读取")
		return
	}
	if log.file == nil || !os.SameFile(log.file, info) || info.Size() < log.file.Size() {
		log.generation = rand.Text()
	}
	log.file = info
	offset := info.Size()
	if cursor := values.Get("cursor"); cursor != "" {
		generation, raw, found := strings.Cut(cursor, ".")
		end, err := strconv.ParseInt(raw, 10, 64)
		if !found || generation == "" || err != nil || end <= 0 {
			WriteAdminError(w, http.StatusBadRequest, "无效的日志游标")
			return
		}
		if generation != log.generation || end > offset {
			WriteAdminError(w, http.StatusConflict, "日志已变更，请从最新记录重新查询")
			return
		}
		offset = end
	}
	page.Available = true
	floor := int64(0)
	if values.Get("cursor") == "" && !query.to.IsZero() {
		_, offset = seekRuntimeLogTime(r.Context(), file, offset, query.to.Add(runtimeLogTimeMargin), &page)
	}
	if !query.from.IsZero() {
		floor, _ = seekRuntimeLogTime(r.Context(), file, offset, query.from.Add(-runtimeLogTimeMargin), &page)
	}
	next, err := scanRuntimeLogRange(r.Context(), file, offset, floor, runtimeLogBudget-page.ScannedBytes, query, &page)
	switch {
	case r.Context().Err() != nil:
		return
	case errors.Is(err, io.EOF):
		WriteAdminError(w, http.StatusConflict, "日志已变更，请从最新记录重新查询")
	case err != nil:
		WriteAdminError(w, http.StatusServiceUnavailable, "当前运行日志暂不可读取")
	default:
		if next > 0 {
			page.NextCursor = log.generation + "." + strconv.FormatInt(next, 10)
		}
		WriteAdminJSON(w, http.StatusOK, page)
	}
}
