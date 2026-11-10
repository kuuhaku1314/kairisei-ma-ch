package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) writeProtocol(writer http.ResponseWriter, method any) {
	a.writeProtocolWithPopups(writer, method, []gamestate.PopupProfile{})
}

func (a *API) writeProtocolResult(
	writer http.ResponseWriter,
	method any,
	resultCode int,
	resultString string,
) {
	a.writeProtocolResultWithPopups(
		writer,
		method,
		[]gamestate.PopupProfile{},
		resultCode,
		resultString,
	)
}

func (a *API) writeProtocolWithPopups(
	writer http.ResponseWriter,
	method any,
	popups []gamestate.PopupProfile,
) {
	a.writeProtocolResultWithPopups(writer, method, popups, 0, "")
}

func (a *API) writeProtocolResultWithPopups(
	writer http.ResponseWriter,
	method any,
	popups []gamestate.PopupProfile,
	resultCode int,
	resultString string,
) {
	a.writeProtocolResponse(writer, method, popups, resultCode, resultString, 0)
}

func (a *API) writeProtocolResponse(
	writer http.ResponseWriter, method any, popups []gamestate.PopupProfile,
	resultCode int, resultString string, resultAction int,
) {
	if resultCode < 0 {
		a.requestRejected = true
	}
	common := newCommonResponse(a.account.UnlockedFeatureState())
	common.ResultCode = resultCode
	common.ResultString = resultString
	common.ResultErrorAction = resultAction
	if resultCode == 0 && !a.requestRejected {
		common.Missions = a.account.TakeMissionUpdates()
	}
	if len(common.Notifications) == 1 {
		// The theater entry uses the high nibble. Leave both CN story badges clear.
		if a.account.CNMainStoryAvailable() {
			common.Notifications[0].IsStoryMainCN = 0x10
		}
		unreceived := a.account.UnreceivedPresentCount()
		common.Notifications[0].PresentNum = int16(min(unreceived, math.MaxInt16))
		_, pvpChallenge := a.account.PvpStatus()
		common.Notifications[0].Challenge = pvpChallenge
		common.Notifications[0].PVPReset.StartTime = a.pvpConfig.ResetStartTime
		common.Notifications[0].PVPReset.EndTime = a.pvpConfig.ResetEndTime
		mission := a.account.MissionNotice()
		common.Notifications[0].MissionTotal = mission.TimedTotal
		common.Notifications[0].MissionComplete = mission.TimedComplete
		if mission.Claimable {
			common.Notifications[0].MissionClearReceive = 1
		}
	}
	body, err := marshalProtocolWithPopups(common, method, popups)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode response")
		return
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func decodeFlexibleFields(
	request *http.Request,
	expected []string,
) (map[string]json.RawMessage, error) {
	body, err := readBody(request)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, fmt.Errorf("decode JSON object: %w", err)
	}
	if len(fields) != len(expected) {
		return nil, errors.New("request fields differ from the endpoint contract")
	}
	result := make(map[string]json.RawMessage, len(expected))
	for index, name := range expected {
		raw, exists := fields[name]
		if !exists {
			raw, exists = fields[strconv.Itoa(index)]
		}
		if !exists {
			return nil, fmt.Errorf("request is missing %s", name)
		}
		result[name] = raw
	}
	return result, nil
}

func decodeStackUses(raw json.RawMessage) ([]gamestate.CardStackUse, error) {
	if string(raw) == "null" {
		return []gamestate.CardStackUse{}, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, errors.New("stack-card selection must be an array")
	}
	counts := make(map[int]int, len(entries))
	order := make([]int, 0, len(entries))
	for _, entry := range entries {
		use := gamestate.CardStackUse{Num: 1}
		if json.Unmarshal(entry, &use.CardID) != nil {
			var fields map[string]json.RawMessage
			if json.Unmarshal(entry, &fields) != nil {
				return nil, errors.New("stack-card entry must be an object")
			}
			cardRaw, cardExists := fields["cardid"]
			if !cardExists {
				cardRaw, cardExists = fields["0"]
			}
			numRaw, numExists := fields["num"]
			if !numExists {
				numRaw, numExists = fields["1"]
			}
			use.Num = 0
			if !cardExists || !numExists ||
				json.Unmarshal(cardRaw, &use.CardID) != nil ||
				json.Unmarshal(numRaw, &use.Num) != nil {
				return nil, errors.New("stack-card entry fields differ")
			}
		}
		// Validate each quantity before merging; negative entries or overflow
		// must not turn an invalid selection into a small, sellable total.
		if use.CardID <= 0 || use.Num <= 0 || counts[use.CardID] > math.MaxInt-use.Num {
			return nil, errors.New("invalid stack-card quantity")
		}
		if _, exists := counts[use.CardID]; !exists {
			order = append(order, use.CardID)
		}
		counts[use.CardID] += use.Num
	}
	result := make([]gamestate.CardStackUse, 0, len(order))
	for _, cardID := range order {
		result = append(result, gamestate.CardStackUse{CardID: cardID, Num: counts[cardID]})
	}
	return result, nil
}

func decodeExact(
	request *http.Request,
	expected []string,
	target any,
	optional ...string,
) error {
	body, err := readBody(request)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return fmt.Errorf("decode JSON object: %w", err)
	}
	if len(fields) < len(expected) || len(fields) > len(expected)+len(optional) {
		return errors.New("request fields differ from the endpoint contract")
	}
	allowed := make(map[string]bool, len(expected)+len(optional))
	for _, name := range expected {
		if _, exists := fields[name]; !exists {
			return fmt.Errorf("request is missing %s", name)
		}
		allowed[name] = true
	}
	for _, name := range optional {
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return errors.New("request fields differ from the endpoint contract")
		}
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	return nil
}

func readBody(request *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(request.Body, game.MaxRequestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read request: %w", err)
	}
	if len(body) > game.MaxRequestBytes {
		return nil, errors.New("request exceeds one MiB")
	}
	return body, nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(writer, "encode response", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	if capture, ok := writer.(*statusWriter); ok {
		// Keep the actual failure with its request ID. Never retain response
		// bodies or request credentials just to diagnose a transport rejection.
		capture.errorText = message
	}
	writeJSON(writer, status, map[string]any{
		"res_code": status,
		"res_str":  message,
	})
}

type statusWriter struct {
	http.ResponseWriter
	status    int
	errorText string
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (a *API) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		capture := &statusWriter{
			ResponseWriter: writer,
			status:         http.StatusOK,
		}
		next.ServeHTTP(capture, request)
		fields := []any{
			"request_id", middleware.GetReqID(request.Context()),
			"method", request.Method,
			"path", request.URL.Path,
			"status", capture.status,
			"elapsed_ms", time.Since(started).Milliseconds(),
		}
		if userID := a.initialState.User.UserID; userID > 0 {
			fields = append(fields, "user_id", userID)
		}
		if capture.errorText != "" {
			fields = append(fields, "error", capture.errorText)
		}
		a.logger.Info("http request", fields...)
	})
}
