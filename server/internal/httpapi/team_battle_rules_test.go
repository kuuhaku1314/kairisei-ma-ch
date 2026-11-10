package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestTeamBattleSoloShowPreservesOverflowBattlePoints(t *testing.T) {
	for arthur := 1; arthur <= 5; arthur++ {
		t.Run(fmt.Sprint(arthur), func(t *testing.T) {
			s := testAccount(t, func(state *gamestate.State) {
				state.User.BP = state.User.BPMax + 43
				state.BattlePoint.NextRecoveryUnix = 0
				state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[],"11":[],"12":[]}`)
			})
			want := s.BattlePointState()
			a := &API{account: s}
			response := httptest.NewRecorder()
			a.teamBattleSoloShow(response, httptest.NewRequest(http.MethodPost, "/TeamBattleSoloShow", strings.NewReader(fmt.Sprintf(`{"active_arthur_type":%d}`, arthur))))
			lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
			if response.Code != http.StatusOK || len(lines) != 3 {
				t.Fatal("overflow BP prevented returning to the dungeon picker", response.Body.String())
			}
			var points struct {
				Current int `json:"0"`
				Max     int `json:"1"`
			}
			if err := json.Unmarshal([]byte(lines[1]), &points); err != nil || points.Current != want.Current || points.Max != want.Max || s.BattlePointState().NextSeconds != 0 {
				t.Fatalf("overflow BP changed on display: %+v / %v", points, err)
			}
		})
	}
}

func TestLostSoloBattleReportReturnsClientResult(t *testing.T) {
	for _, cleared := range []int{0, 1} {
		s := testAccount(t, func(state *gamestate.State) {
			state.User.Coin = 123
			state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"10":[{"0":30010102}]}],"11":[],"12":[]}`)
		})
		a := &API{account: s, initialState: gamestate.State{}}
		response := httptest.NewRecorder()
		a.teamBattleSoloEnd(response, httptest.NewRequest(http.MethodPost, "/TeamBattleSoloEnd", strings.NewReader(
			fmt.Sprintf(`{"progress":0,"is_clear":%d,"input_cmd":[""],"enemy_dead_bit":[0],"bossid":30010102}`, cleared))))
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		if response.Code != http.StatusOK || len(lines) != 3 {
			t.Fatal("expired report returned a transport failure", response.Body.String())
		}
		var common commonResponse
		if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
			t.Fatal(err)
		}
		wantCode := 0
		if cleared == 1 {
			wantCode = -3207 // Original SV_ERR.TEAMBATTLE_REWARD_NOT_FOUND.
		}
		if common.ResultCode != wantCode || s.Snapshot(gamestate.State{}).User.Coin != 123 || s.Snapshot(gamestate.State{}).ActiveTeamBattle != nil || len(s.Snapshot(gamestate.State{}).TeamBattleSoloResultReceipts) != 0 {
			t.Fatal("expired report changed the account or returned the wrong business result")
		}
		body := fmt.Sprintf(`{"progress":0,"is_clear":%d,"input_cmd":[""],"enemy_dead_bit":[0],"bossid":30010102}`, cleared)
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
		var receipt gamestate.TeamBattleSoloResultReceipt
		if err := json.Unmarshal([]byte(fmt.Sprintf(`{"request_sha256":"%s","boss_id":30010102,"claimed_at_unix":1,"response":{"partners":[]}}`, digest)), &receipt); err != nil {
			t.Fatal(err)
		}
		a.account = testAccount(t, func(state *gamestate.State) {
			state.User.Coin = 123
			state.TeamBattleSoloResultReceipts = []gamestate.TeamBattleSoloResultReceipt{receipt}
		})
		response = httptest.NewRecorder()
		a.teamBattleSoloEnd(response, httptest.NewRequest(http.MethodPost, "/TeamBattleSoloEnd", strings.NewReader(body)))
		lines = strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		if response.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.ResultCode != -3207 ||
			a.account.Snapshot(gamestate.State{}).User.Coin != 123 {
			t.Fatal("retired solo reply blocked recovery or changed rewards", response.Body.String())
		}
	}
}

func TestSoloRetreatBeforeBattleEndAllowsRetainedReport(t *testing.T) {
	command := "0,22," + strings.Repeat("2147483647,", 19) + "2147483647"
	for _, enemies := range [][]int8{{1}, {1, 1, 4}} {
		commands, dead := make([]string, len(enemies)), make([]int, len(enemies))
		if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err != nil {
			t.Fatal("original zero-progress retreat cannot finish its login retry", err)
		}
		if err := validateNativeTeamBattleReport(0, true, commands, dead, enemies); err == nil {
			t.Fatal("empty retreat accepted as victory")
		}
		dead[0] = 1
		if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err == nil {
			t.Fatal("zero-progress retreat accepted enemy drops")
		}
		dead[0], commands[0] = 0, "unexpected command"
		if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err == nil {
			t.Fatal("zero-progress retreat accepted malformed commands")
		}
		commands[0], dead[0] = command, 2
		if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err != nil {
			t.Fatal("retreat after Continue rejected the retained native report", err)
		}
		if err := validateNativeTeamBattleReport(1, false, commands, dead, enemies); err != nil {
			t.Fatal("normal defeat no longer accepts the advanced battle index", err)
		}
		if len(enemies) > 1 {
			commands[1] = command
			if err := validateNativeTeamBattleReport(0, false, commands, dead, enemies); err == nil {
				t.Fatal("retreat accepted future wave state")
			}
			commands[0] = ""
			if err := validateNativeTeamBattleReport(1, false, commands, dead, enemies); err == nil {
				t.Fatal("later-wave retreat accepted a prior wave without commands")
			}
			commands[0] = command
			if err := validateNativeTeamBattleReport(1, false, commands, dead, enemies); err != nil {
				t.Fatal("retreat from a later wave rejected its retained report", err)
			}
		}
	}
}

func TestSoloVictoryDoesNotRequireEnemyBodyDeath(t *testing.T) {
	command := "0,22," + strings.Repeat("2147483647,", 19) + "2147483647"
	for _, mask := range []int{0, 6, 7} {
		if err := validateNativeTeamBattleReport(1, true, []string{command}, []int{mask}, []int8{1}); err != nil {
			t.Fatalf("native scripted victory with death mask %d: %v", mask, err)
		}
	}
	// A truly malformed report must also leave the original client's login
	// retry path through its result callback, rather than HTTP 400.
	s := testAccount(t, func(state *gamestate.State) {
		state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"10":[{"0":30010102,"5":0}]}],"11":[],"12":[]}`)
		state.ActiveTeamBattle = &gamestate.TeamBattleActiveState{BossID: 30010102, BattleEnemyTypes: []int8{1}, FameSeed: "solo:test", FameSources: []gamestate.TeamBattleFameSourceState{{MemberType: 1, LeaderFame: 1}}}
		state.TeamBattleRewards = []gamestate.TeamBattleRewardProfile{{BossID: 30010102}}
		helper := &state.PlayerProgressionPolicy.Friends.HelperReward
		helper.OtherPerPartner, helper.FriendPerPartner = 5, 10
	})
	a := &API{account: s, initialState: gamestate.State{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := httptest.NewRecorder()
	a.teamBattleSoloEnd(response, httptest.NewRequest(http.MethodPost, "/TeamBattleSoloEnd", strings.NewReader(
		`{"progress":1,"is_clear":1,"input_cmd":["invalid"],"enemy_dead_bit":[6],"bossid":30010102}`)))
	var common commonResponse
	if err := json.Unmarshal([]byte(strings.Split(response.Body.String(), "\n")[0]), &common); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || common.ResultCode != -3207 || s.Snapshot(gamestate.State{}).ActiveTeamBattle == nil || len(s.Snapshot(gamestate.State{}).TeamBattleSoloResultReceipts) != 0 {
		t.Fatal("invalid report did not return the non-settling client business error", response.Body.String())
	}
}
