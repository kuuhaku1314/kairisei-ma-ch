package cnbootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

// The native login recovery chain must reach its result callback even when
// the cached DTO is malformed. Exercise the real CN adapter and business API.
func TestInvalidCachedBattleReportReachesDiscardCallback(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.PVP.NextBattleID = 2
	state.PVP.History = []gamestate.PVPMatch{{BattleID: 1, ExpectedWin: true, CompletedAtUnix: 1}}
	writes := 0
	business, err := httpapi.New(httpapi.Config{
		InitialState: state, BaseURL: "http://127.0.0.1", Multiplayer: multiplayer.NewHub(),
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		PersistState: func(gamestate.State) error { writes++; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	initialWrites := writes
	for _, route := range []struct {
		name     string
		handler  http.HandlerFunc
		payloads []string
	}{
		{"TeamBattleSoloEnd", cnBootstrapTeamBattleSoloEnd(business), []string{"", "{}", "[", `{"bossid":"broken"}`, `{"progress":1,"is_clear":1,"input_cmd":["broken"],"enemy_dead_bit":[1],"bossid":30010102}`}},
		{"TeamBattleResult", cnBootstrapTeamBattleResult(business), []string{"", "{}", "[", `{"roomid":"broken"}`, `{"roomid":-1}`, `{"roomid":123}`}},
		{"PvpEnd", cnBootstrapPvpEnd(business), []string{"", "{}", "[", `{"btluid":"broken"}`, `{"btluid":123,"is_win":1,"is_retire":0,"input_cmd":["broken"]}`, `{"btluid":123,"is_win":0,"is_retire":1,"input_cmd":[""]}`, `{"btluid":1,"is_win":0,"is_retire":1,"input_cmd":[""]}`}},
	} {
		for _, payload := range route.payloads {
			t.Run(route.name+"/"+payload, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, "/"+route.name, strings.NewReader("test-session-00000000"+payload))
				r = r.WithContext(context.WithValue(r.Context(), cnAuthenticatedUserKey{}, state.User.UserID))
				w := httptest.NewRecorder()
				route.handler(w, r)
				segments := bytes.Split(w.Body.Bytes(), []byte{'\n'})
				var common struct {
					Code       int `json:"res_code"`
					DeleteSave int `json:"res_is_del_savedata"`
				}
				if w.Code != http.StatusOK || len(segments) != 3 || json.Unmarshal(segments[0], &common) != nil || common.Code != -3207 || common.DeleteSave != 0 {
					t.Fatalf("cached report still blocks recovery or resets account: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
	if writes != initialWrites {
		t.Fatal("discarded report changed persistent state")
	}
}
