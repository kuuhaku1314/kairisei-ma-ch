package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"time"
)

// The cursor points just past a newline so subsequent appends do not shift it.
// A byte budget may cut through a record. Resume at that record's end unless
// it already exceeds the line limit, in which case resume at the scan boundary.
func scanRuntimeLogRange(ctx context.Context, file io.ReaderAt, offset, rangeStart, budget int64, query runtimeLogQuery, page *runtimeLogPage) (int64, error) {
	const blockSize = 64 * 1024
	var block, line [blockSize]byte
	floor := max(rangeStart, offset-budget)
	pos, lineEnd := offset, offset
	used := 0
	skip := true // Ignore the tail after the last newline, including partial appends.
	for pos > floor {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		start := max(floor, pos-blockSize)
		chunk := block[:pos-start]
		n, err := file.ReadAt(chunk, start)
		page.ScannedBytes += int64(n)
		if err != nil {
			return 0, err
		}
		for right := len(chunk); right > 0; {
			separator := bytes.LastIndexByte(chunk[:right], '\n')
			piece := chunk[separator+1 : right]
			if !skip {
				if len(piece) > len(line)-used {
					skip = true
				} else {
					used += len(piece)
					copy(line[len(line)-used:], piece)
				}
			}
			if separator < 0 {
				break
			}
			if !skip && appendRuntimeLogEntry(line[len(line)-used:], query, page) {
				return start + int64(separator) + 1, nil
			}
			skip, used = false, 0
			lineEnd = start + int64(separator)
			right = separator
		}
		pos = start
	}
	if floor == rangeStart {
		if !skip {
			appendRuntimeLogEntry(line[len(line)-used:], query, page)
		}
		return 0, nil
	}
	if !skip && lineEnd+1 < offset {
		return lineEnd + 1, nil
	}
	return floor, nil
}

// Seek with complete timestamped lines. The interval between the returned offsets
// is left unpruned, and callers pad their time bounds before using either offset.
// A missing probe timestamp leaves the original range intact.
func seekRuntimeLogTime(ctx context.Context, file io.ReaderAt, size int64, target time.Time, page *runtimeLogPage) (int64, int64) {
	var probe [64 * 1024]byte
	lo, hi := int64(0), size
	for hi-lo > 2*int64(len(probe)) {
		if ctx.Err() != nil || page.ScannedBytes+int64(len(probe)) >= runtimeLogBudget {
			return 0, size
		}
		mid := lo + (hi-lo)/2
		n, err := file.ReadAt(probe[:], mid)
		page.ScannedBytes += int64(n)
		if err != nil {
			return 0, size
		}
		newline := bytes.IndexByte(probe[:n], '\n')
		if newline < 0 {
			return 0, size
		}
		found := false
		for start := newline + 1; start < n; {
			end := bytes.IndexByte(probe[start:n], '\n')
			if end < 0 {
				break
			}
			end += start
			var stamp struct {
				Time time.Time `json:"time"`
			}
			if json.Unmarshal(probe[start:end], &stamp) == nil && !stamp.Time.IsZero() {
				if stamp.Time.Before(target) {
					lo = mid + int64(end) + 1
				} else {
					hi = mid + int64(end) + 1
				}
				found = true
				break
			}
			start = end + 1
		}
		if !found {
			return 0, size
		}
	}
	return lo, hi
}

// Only operational scalars are exposed to Admin. The file can also contain
// arbitrary payloads or nested snapshots, which stay in the source log.
func runtimeLogDiagnosticKey(key string) bool {
	switch key {
	case "address", "advertise_base_url", "admin_image_sync", "admin_listen", "battle_advertise", "battle_listen",
		"client_profile", "listen", "retry_after", "channel", "window_ms", "messages", "compressed_messages", "raw_bytes", "wire_bytes", "write_ms",
		"room_id", "user_id", "owner_user_id", "boss_id", "story_id", "code_id", "enemy_party_id", "remote", "revision", "member_type",
		"auto_start", "battle_end", "battle_index", "comeback_allowed", "connected_members", "evaluated_members", "fallback_members",
		"phase_ack", "remaining_connections", "result_rows", "attack_result_rows", "card_play_result_rows", "seed", "skill_slot",
		"sphere_slot", "sphere_target", "target", "timed_out", "turn", "team_battle_groups", "team_battle_boss_entries",
		"user_buff_profiles", "user_buff_references", "stage_quest_stages", "explore_stages", "explore_floors", "main_stories",
		"cn_main_stories", "sub_story_sections", "sub_stories", "story_events", "event_stories", "story_battle_ids", "story_battle_references", "popup_profiles":
		return true
	default:
		return false
	}
}

func appendRuntimeLogEntry(line []byte, query runtimeLogQuery, page *runtimeLogPage) bool {
	var entry runtimeLogEntry
	if query.userID != 0 {
		var row struct {
			*runtimeLogEntry
			UserID json.RawMessage `json:"user_id"`
		}
		row.runtimeLogEntry = &entry
		if json.Unmarshal(line, &row) != nil || !runtimeLogMatchesUserID(row.UserID, query.userID) {
			return false
		}
	} else if json.Unmarshal(line, &entry) != nil {
		return false
	}
	if !query.filters(entry) {
		return false
	}
	text := query.matchesText(entry)
	if !text && !query.mayContain(line) {
		return false
	}
	entry.Fields = runtimeLogExtraFields(line)
	if !text && !query.matchesFields(entry.Fields) {
		return false
	}
	entry.boundDisplayText()
	page.Entries = append(page.Entries, entry)
	return len(page.Entries) == runtimeLogPageSize
}

func runtimeLogMatchesUserID(raw json.RawMessage, want int64) bool {
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &value) != nil {
		return false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id == want
}

// runtimeLogExtraFields reads approved diagnostics in their written order.
func runtimeLogExtraFields(line []byte) []runtimeLogField {
	decoder := json.NewDecoder(bytes.NewReader(line))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	var fields []runtimeLogField
	for decoder.More() && len(fields) < runtimeLogFieldLimit {
		token, err := decoder.Token()
		key, isKey := token.(string)
		var raw json.RawMessage
		if err != nil || !isKey || decoder.Decode(&raw) != nil {
			break
		}
		if !runtimeLogDiagnosticKey(key) || len(raw) == 0 || raw[0] == '{' || raw[0] == '[' || bytes.Equal(raw, []byte("null")) {
			continue
		}
		value := string(raw)
		var text string
		if json.Unmarshal(raw, &text) == nil {
			value = text
		}
		fields = append(fields, runtimeLogField{Key: key, Value: boundRuntimeLogText(value, runtimeLogFieldText)})
	}
	return fields
}
