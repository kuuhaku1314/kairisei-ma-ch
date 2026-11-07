package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestRequestLogUsesAuthenticatedAccountIdentity(t *testing.T) {
	var output bytes.Buffer
	a := &API{logger: slog.New(slog.NewJSONHandler(&output, nil))}
	a.initialState.User.UserID = 1000002
	logged := middleware.RequestID(a.logRequest(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusBadRequest, "invalid payload")
	})))
	request := httptest.NewRequest(http.MethodPost, "/Test", nil)
	request.Header.Set(accounthttp.AccountUserHeader, "9999999")
	logged.ServeHTTP(httptest.NewRecorder(), request)
	var entry struct {
		UserID    int    `json:"user_id"`
		RequestID string `json:"request_id"`
		Status    int    `json:"status"`
	}
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil ||
		entry.UserID != 1000002 || entry.RequestID == "" || entry.Status != http.StatusBadRequest {
		t.Fatalf("request log lost the account identity or request metadata: %s", output.Bytes())
	}
}

func TestBusinessErrorsPreserveSessionAndProtocolErrorsRemainFailures(t *testing.T) {
	a := &API{account: &game.Account{}, initialState: gamestate.State{}}
	for _, expected := range []*game.BusinessError{game.ErrInsufficientGold, game.ErrInsufficientCrystals, game.ErrInsufficientMaterials, game.ErrCardCapacity, game.ErrSphereCapacity, game.ErrBuddyCapacity, game.ErrItemExpired, game.ErrGachaUnavailable, game.ErrEvolutionClosed} {
		response := httptest.NewRecorder()
		a.writeStoreError(response, fmt.Errorf("operation: %w", expected))
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		var common commonResponse
		if response.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.ResultCode != expected.Code || common.ResultString != expected.Message || common.ResultErrorAction != 2 || common.ResultDeleteSaveData != 0 {
			t.Fatalf("business failure lost native envelope: %s", response.Body.String())
		}
	}
	for _, err := range []error{battlecore.ErrMultipleRoundTableCards, battlecore.ErrTooManySummonCards, battlecore.ErrAlchemistDeckProfession} {
		response := httptest.NewRecorder()
		a.writeStoreError(response, fmt.Errorf("deck: %w", err))
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		var common commonResponse
		if response.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.ResultCode != -1 || !strings.Contains(common.ResultString, "调整卡组") || common.ResultErrorAction != 2 || common.ResultDeleteSaveData != 0 {
			t.Fatalf("deck composition failure lost native envelope: %s", response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	if a.writeBusinessError(response, errors.New("disk failure")) || response.Body.Len() != 0 {
		t.Fatal("storage error was disguised as a business rejection")
	}
	a.writeStoreError(response, errors.New("invalid payload"))
	if response.Code != 400 {
		t.Fatal("malformed input was accepted")
	}
}

func TestClosedGachaRejectsStalePlayWithoutDisconnection(t *testing.T) {
	a := &API{account: &game.Account{}, initialState: gamestate.State{}}
	r := httptest.NewRequest("POST", "/GachaPlay2", strings.NewReader(`{"gachaid":60200011,"pay_type":3,"gacha_hash":"stale","select_lineup_list":[],"popupid":0}`))
	r = WithGachaPublication(r, func(int) bool { return false })
	w := httptest.NewRecorder()
	a.gachaPlay(w, r)
	var common commonResponse
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.ResultCode != -3100 || common.ResultDeleteSaveData != 0 || common.ResultErrorAction != 2 {
		t.Fatalf("stale gacha: %s", w.Body.String())
	}
}
