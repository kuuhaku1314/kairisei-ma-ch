package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

type historyFixture struct {
	saves   int
	records []gamestate.BattleClearRecord
	commits []game.SoloBattleCommit
}

func (h *historyFixture) PersistSoloBattle(_ gamestate.State, result game.SoloBattleCommit) error {
	h.commits = append(h.commits, result)
	if result.Clear == nil {
		return nil
	}
	h.saves++
	r := *result.Clear
	r.Count = 1
	h.records = []gamestate.BattleClearRecord{r}
	return nil
}

func TestSoloScoreContractFreezesRulesAndCommitsOwnDeckRankOnce(t *testing.T) {
	const boss, ownBoss = 30010102, 130010102
	policy := &gamestate.TeamBattleScorePolicy{SourceState: "LOCAL_POLICY_DAMAGE_SCORE", Rate: 150, EndTurn: 5, TeamCostInitial: 6, MemberDeadEnd: true}
	for i := range 9 {
		policy.Grades = append(policy.Grades, gamestate.TeamBattleScoreGrade{Score: int64(i+1) * 100, Rewards: []gamestate.Reward{{Type: 10, Num: 10, CardSkillLevels: []int16{}}}})
	}
	var initial gamestate.State
	account := testAccount(t, func(state *gamestate.State) {
		state.User.Name = "计分测试"
		state.User.BP = 100
		helper := &state.PlayerProgressionPolicy.Friends.HelperReward
		helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
		state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"1":13,"4":"计分","10":[{"0":30010102,"1":0,"5":1,"6":1,"10":0,"16":0},{"0":130010102,"1":1,"5":1,"6":1,"10":0,"16":0}]}],"11":[],"12":[]}`)
		initial = *state
	})
	state := account.Snapshot(initial)
	state.TeamBattleOwnDeckSources = map[int]int{ownBoss: boss}
	for _, id := range []int{boss, ownBoss} {
		state.TeamBattleReplays = append(state.TeamBattleReplays, gamestate.TeamBattleReplay{BossID: id, EnemyPartyID: 1, EnemyType: 1, CostInitial: 3})
		state.TeamBattleRewards = append(state.TeamBattleRewards, gamestate.TeamBattleRewardProfile{BossID: id, ScorePolicy: policy})
	}
	history := &historyFixture{}
	a := &API{initialState: state, account: account, battleHistory: history, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	call := func(handler http.HandlerFunc, payload string) (map[string]json.RawMessage, int) {
		t.Helper()
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/", strings.NewReader(payload)))
		parts := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		if w.Code != 200 || len(parts) != 3 {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
		var common struct {
			Code int `json:"res_code"`
		}
		var method map[string]json.RawMessage
		if json.Unmarshal([]byte(parts[0]), &common) != nil || json.Unmarshal([]byte(parts[1]), &method) != nil {
			t.Fatal("bad protocol", w.Body.String())
		}
		return method, common.Code
	}
	start := fmt.Sprintf(`{"bossid":130010102,"deck_arthur_type":3,"deck_arthur_type_idx":0,"partner_deck_selects":[{"userid":%d,"arthur_type":1,"deck_idx":0},{"userid":%d,"arthur_type":2,"deck_idx":0},{"userid":%d,"arthur_type":4,"deck_idx":0}]}`, state.User.UserID, state.User.UserID, state.User.UserID)
	method, code := call(a.teamBattleSoloStart, start)
	var cost int
	var rule struct {
		Rate          int  `json:"rate"`
		MemberDeadEnd bool `json:"member_dead_end"`
	}
	if code != 0 || json.Unmarshal(method["cost_initial"], &cost) != nil || cost != 6 || json.Unmarshal(method["score_policy"], &rule) != nil || rule.Rate != 150 || !rule.MemberDeadEnd {
		t.Fatal("solo start did not freeze configured rules", method)
	}
	policy.Rate = 900
	end := `{"bossid":130010102,"progress":1,"is_clear":0,"input_cmd":["0,22` + strings.Repeat(",0", 20) + `"],"enemy_dead_bit":[0],"score_damage":250}`
	for retry := 0; retry < 2; retry++ {
		if _, code := call(a.teamBattleSoloEnd, end); code != 0 {
			t.Fatal("accepted score failed", code)
		}
	}
	if len(history.commits) != 1 || history.commits[0].Score == nil || history.commits[0].Score.BossID != boss || history.commits[0].Score.Score != 375 {
		t.Fatal("own-deck score did not normalize identity, freeze multiplier or deduplicate", history.commits)
	}
	for _, mode := range []int{0, 1} {
		method, code := call(a.teamBattleScoreRewardLineup, fmt.Sprintf(`{"bossid":30010102,"mode":%d}`, mode))
		var high int64
		if code != 0 || json.Unmarshal(method["high_score"], &high) != nil || mode == 0 && high != 375 || mode == 1 && high != 0 {
			t.Fatal("solo reward lineup mixed modes", mode, method)
		}
	}
	if _, code = call(a.teamBattleSoloStart, start); code != 0 {
		t.Fatal("second solo start", code)
	}
	missing := strings.Replace(end, `,"score_damage":250`, "", 1)
	if _, code = call(a.teamBattleSoloEnd, missing); code != -3207 || len(history.commits) != 1 {
		t.Fatal("missing score earned a fixed clear grade or blocked cache discard", code)
	}
}
func (h *historyFixture) RecentBattleClears(int, time.Time) ([]gamestate.BattleClearRecord, error) {
	return append([]gamestate.BattleClearRecord(nil), h.records...), nil
}
func (h *historyFixture) YesterdayBattleRanks(int, int, time.Time) ([]gamestate.BattleClearRecord, error) {
	return h.records, nil
}

func TestClearHistoryUsesStartedPartyAndNativeRankAlignment(t *testing.T) {
	const boss = 30010102
	var initial gamestate.State
	s := testAccount(t, func(state *gamestate.State) {
		state.User.Name = "开战昵称"
		state.User.BP = 100
		helper := &state.PlayerProgressionPolicy.Friends.HelperReward
		helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
		state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"4":"活动","9":0,"10":[{"0":30010102,"1":1,"4":"超级","5":1,"10":0,"16":0}]}],"11":[],"12":[]}`)
		for i := range state.Decks {
			state.Decks[i].Name = fmt.Sprintf("原卡组%d", i)
		}
		initial = *state
	})
	state := s.Snapshot(initial)
	state.TeamBattleReplays = []gamestate.TeamBattleReplay{{BossID: boss, EnemyPartyID: 1}}
	state.TeamBattleRewards = []gamestate.TeamBattleRewardProfile{{BossID: boss}}
	history := &historyFixture{}
	a := &API{initialState: state, account: s, battleHistory: history, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	call := func(fn http.HandlerFunc, payload string) (map[string]json.RawMessage, int) {
		t.Helper()
		w := httptest.NewRecorder()
		fn(w, httptest.NewRequest("POST", "/", strings.NewReader(payload)))
		parts := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		if w.Code != 200 || len(parts) != 3 {
			t.Fatalf("HTTP %d %s", w.Code, w.Body.String())
		}
		var common struct {
			Code int `json:"res_code"`
		}
		var method map[string]json.RawMessage
		if json.Unmarshal([]byte(parts[0]), &common) != nil || json.Unmarshal([]byte(parts[1]), &method) != nil {
			t.Fatal("invalid response", w.Body.String())
		}
		return method, common.Code
	}
	if _, code := call(a.teamBattleClearDeckShow, `{"bossid":30010102}`); code != -1 {
		t.Fatal("empty history fabricated a cleared deck")
	}
	start := fmt.Sprintf(`{"bossid":30010102,"deck_arthur_type":3,"deck_arthur_type_idx":0,"partner_deck_selects":[{"userid":%d,"arthur_type":1,"deck_idx":0},{"userid":%d,"arthur_type":2,"deck_idx":0},{"userid":%d,"arthur_type":4,"deck_idx":0}]}`, state.User.UserID, state.User.UserID, state.User.UserID)
	method, code := call(a.teamBattleSoloStart, start)
	if code != 0 {
		t.Fatal("start failed", code)
	}
	var partners []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(method["partner_deck"], &partners); err != nil || len(partners) != 3 || partners[0].Name != "开战昵称" {
		t.Fatal("own partner nickname projection", err)
	}
	// An account-handler reload must retain the runtime snapshot, and later
	// nickname changes must not rewrite the identity recorded at battle start.
	s.SetUserName("后来昵称")
	input := `{"bossid":30010102,"progress":1,"is_clear":1,"input_cmd":["0,22` + strings.Repeat(",0", 20) + `"],"enemy_dead_bit":[0]}`
	if _, code := call(a.teamBattleSoloEnd, input); code != 0 {
		t.Fatal("clear failed", code)
	}
	if _, code := call(a.teamBattleSoloEnd, input); code != 0 || history.saves != 1 {
		t.Fatal("repeated result recorded a second clear")
	}
	if len(history.records) != 1 || history.records[0].Decks[0].ArthurType != 3 || history.records[0].Decks[0].Name != "开战昵称" {
		t.Fatal("controlled profession / frozen nickname lost")
	}
	method, code = call(a.dailyClearRankShow, `{"bossid":30010102,"is_multi":0}`)
	var lists []struct {
		Ranks []struct {
			Rank int `json:"rank"`
			Deck struct {
				Arthur int    `json:"arthur_type"`
				Name   string `json:"name"`
			} `json:"partner_deck"`
		} `json:"ranks"`
	}
	if code != 0 || json.Unmarshal(method["deck"], &lists) != nil || len(lists) != 4 {
		t.Fatal("invalid native rank lists")
	}
	var missing []json.RawMessage
	if err := json.Unmarshal(method["how_to_list"], &missing); err != nil || len(missing) != 0 {
		t.Fatalf("owned cards were marked missing in yesterday's decks: %s (%v)", method["how_to_list"], err)
	}
	for i, want := range []int{3, 1, 2, 4} {
		if len(lists[i].Ranks) != 1 || lists[i].Ranks[0].Deck.Arthur != want || !strings.HasPrefix(lists[i].Ranks[0].Deck.Name, "原卡组") {
			t.Fatal("solo four-list alignment or original deck name lost", lists)
		}
	}
	method, code = call(a.teamBattleClearDeckShow, `{"bossid":30010102}`)
	var sample []json.RawMessage
	if code != 0 || json.Unmarshal(method["partner_deck"], &sample) != nil || len(sample) != 4 {
		t.Fatal("recent clear sample missing professions")
	}
	if err := json.Unmarshal(method["how_to_list"], &missing); err != nil || len(missing) != 0 {
		t.Fatalf("owned cards were marked missing in recent decks: %s (%v)", method["how_to_list"], err)
	}
	if _, code := call(a.teamBattleSoloStart, start); code != 0 {
		t.Fatal("next start failed")
	}
	if _, code := call(a.teamBattleSoloEnd, `{"bossid":30010102,"progress":0,"is_clear":0,"input_cmd":[""],"enemy_dead_bit":[0]}`); code != 0 || history.saves != 1 {
		t.Fatal("retreat entered clear statistics")
	}
}
