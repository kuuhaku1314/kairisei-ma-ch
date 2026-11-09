package admin

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestBossEconomySharedScopeAtomicSaveAndRestart(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	base := gamestate.State{
		TeamBattleSolo:           json.RawMessage(`{"9":[],"10":[{"0":1,"4":"3D与2D","10":[{"0":11,"4":"超级","5":35,"6":18,"14":1},{"0":12,"4":"超级","5":35,"6":18,"14":0},{"0":13,"4":"超级","5":35,"6":18},{"0":100000011,"1":1,"4":"超级","5":35,"6":18},{"0":77,"4":"主线专属","5":5,"6":3},{"0":88,"4":"塔层专属","5":30,"6":15}]},{"0":2,"4":"钥匙素材","10":[{"0":20000001,"4":"超级","5":7,"6":4}]}],"11":[],"12":[]}`),
		TeamBattlePastBossGroups: []json.RawMessage{json.RawMessage(`{"0":3,"13":[{"0":13,"4":"超级","5":35,"6":18}]}`)},
		TeamBattleOwnDeckSources: map[int]int{100000011: 11},
	}
	for i, id := range []int{11, 12, 13, 100000011, 20000001, 77, 88} {
		p := gamestate.TeamBattleRewardProfile{BossID: id, ResultRewards: []gamestate.Reward{{Type: 0, Num: (i + 1) * 10}, {Type: 4, Num: 100}}}
		if id == 77 {
			p.StageQuestAreaID = 1
		}
		if id == 88 {
			p.TowerID = 1
		}
		base.TeamBattleRewards = append(base.TeamBattleRewards, p)
	}
	if err := ops.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: ops}
	router := chi.NewRouter()
	router.Get("/economy", a.bossEconomy)
	router.Put("/economy", a.saveBossEconomy)
	read := func() map[string]json.RawMessage {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/economy", nil))
		if w.Code != 200 {
			t.Fatalf("GET %d %s", w.Code, w.Body.String())
		}
		var d map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	check := func(id, bp, xp int) {
		t.Helper()
		state := ops.content.configuration.State
		var top map[string][]struct {
			Bosses []struct {
				ID   int `json:"0"`
				BP   int `json:"5"`
				Half int `json:"6"`
			} `json:"10"`
		}
		if err := json.Unmarshal(state.TeamBattleSolo, &top); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, gs := range top {
			for _, g := range gs {
				for _, b := range g.Bosses {
					if b.ID == id {
						found = true
						if b.BP != bp || b.Half != (bp+1)/2 {
							t.Fatalf("%d cost %d/%d", id, b.BP, b.Half)
						}
					}
				}
			}
		}
		if !found {
			t.Fatalf("missing %d", id)
		}
		for _, p := range state.TeamBattleRewards {
			if p.BossID == id {
				n := 0
				for _, r := range p.ResultRewards {
					if r.Type == 0 {
						n += r.Num
					}
					if r.Type == 4 && r.Num != 100 {
						t.Fatal("gold changed")
					}
				}
				if n != xp {
					t.Fatalf("%d XP=%d want %d", id, n, xp)
				}
			}
		}
	}
	var initial struct {
		Limits map[string]int `json:"limits"`
		Rules  []struct {
			Difficulty string            `json:"difficulty"`
			BossCount  int               `json:"boss_count"`
			Defaults   bossEconomyValues `json:"defaults"`
		} `json:"rules"`
	}
	raw, _ := json.Marshal(read())
	if err := json.Unmarshal(raw, &initial); err != nil {
		t.Fatal(err)
	}
	if len(initial.Rules) != 1 || initial.Rules[0].Difficulty != "超级" || initial.Rules[0].BossCount != 3 {
		t.Fatalf("scope/defaults %+v", initial)
	}
	if initial.Limits["bp_use_min"] != 1 || initial.Limits["experience_min"] != 0 {
		t.Fatalf("unexpected limits: %+v", initial.Limits)
	}
	testfixture.CallContentAdmin(t, router, "PUT", "/economy", map[string]any{"expected_revision": 0, "rules": []any{map[string]any{"difficulty": "超级", "bp_use": 19, "experience": 99}}}, 200)
	for _, id := range []int{11, 12, 13, 100000011} {
		check(id, 19, 99)
	}
	check(20000001, 7, 50)
	check(77, 5, 60)
	check(88, 30, 70)
	var past struct {
		Bosses []struct {
			BP   int `json:"5"`
			Half int `json:"6"`
		} `json:"13"`
	}
	_ = json.Unmarshal(ops.content.configuration.State.TeamBattlePastBossGroups[0], &past)
	if past.Bosses[0].BP != 19 || past.Bosses[0].Half != 10 {
		t.Fatal("past view not updated")
	}
	testfixture.CallContentAdmin(t, router, "PUT", "/economy", map[string]any{"expected_revision": 0, "rules": []any{map[string]any{"difficulty": "超级", "bp_use": 1}}}, 409)
	testfixture.CallContentAdmin(t, router, "PUT", "/economy", map[string]any{"expected_revision": 1, "rules": []any{map[string]any{"difficulty": "超级", "bp_use": 1}, map[string]any{"difficulty": "主线专属", "bp_use": 1}}}, 400)
	check(11, 19, 99)
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	if restarted.content.economyRevision != 1 || *restarted.content.economy["超级"].Experience != 99 {
		t.Fatal("restart lost economy")
	}
	if base.TeamBattleRewards[0].ResultRewards[0].Num != 10 {
		t.Fatal("base mutated")
	}
	testfixture.CallContentAdmin(t, router, "PUT", "/economy", map[string]any{"expected_revision": 1, "rules": []any{map[string]any{"difficulty": "超级", "reset": true}}}, 200)
	check(11, 35, 10)
	check(12, 35, 20)
	check(13, 35, 30)
	testfixture.CallContentAdmin(t, router, "PUT", "/economy", map[string]any{"expected_revision": 2, "rules": []any{map[string]any{"difficulty": "超级", "bp_use": 1, "experience": 0}}}, 200)
	check(11, 1, 0)
	check(20000001, 7, 50)
	for _, body := range []map[string]any{
		{"difficulty": "超级", "bp_use": -1}, {"difficulty": "超级", "bp_use": 0}, {"difficulty": "超级", "experience": 10000001}, {"difficulty": "超级", "reset": true, "bp_use": 1},
	} {
		testfixture.CallContentAdmin(t, router, "PUT", "/economy", map[string]any{"expected_revision": 3, "rules": []any{body}}, 400)
	}
	check(11, 1, 0)
}
