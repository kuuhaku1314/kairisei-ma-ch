package admin

import (
	"encoding/json"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestBossBattleOpeningScopeInheritanceAtomicSaveAndRestart(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	base := gamestate.State{
		TeamBattleSolo:           json.RawMessage(`{"9":[],"10":[{"0":1,"4":"活动","10":[{"0":11,"4":"超级","5":35,"6":18,"14":1},{"0":12,"4":"超级","5":35,"6":18,"14":0},{"0":13,"4":"超级","5":35,"6":18},{"0":111,"1":1,"4":"超级","5":35,"6":18},{"0":77,"4":"主线","5":7,"6":4},{"0":88,"4":"塔","5":7,"6":4},{"0":99,"4":"圣剑杯","5":7,"6":4}]},{"0":2,"4":"钥匙素材","10":[{"0":20000001,"4":"超级","5":7,"6":4}]}],"11":[],"12":[]}`),
		TeamBattlePastBossGroups: []json.RawMessage{json.RawMessage(`{"0":3,"13":[{"0":13,"4":"超级","5":35,"6":18}]}`)},
		TeamBattleOwnDeckSources: map[int]int{111: 11},
	}
	for _, id := range []int{11, 12, 13, 111, 77, 88, 99, 20000001} {
		p := gamestate.TeamBattleRewardProfile{BossID: id}
		if id == 77 {
			p.StageQuestAreaID = 1
		}
		if id == 88 {
			p.TowerID = 1
		}
		if id == 99 {
			p.ScorePolicy = &gamestate.TeamBattleScorePolicy{Rate: 1, Grades: make([]gamestate.TeamBattleScoreGrade, 9)}
		}
		base.TeamBattleRewards = append(base.TeamBattleRewards, p)
		base.TeamBattleReplays = append(base.TeamBattleReplays, gamestate.TeamBattleReplay{BossID: id, CostInitial: 3, BurstGaugeInitial: 20})
	}
	if err := o.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	if len(o.content.battleOpeningBases) != 3 {
		t.Fatalf("scope: %+v", o.content.battleOpeningBases)
	}
	a := &API{operations: o}
	router := chi.NewRouter()
	router.Put("/opening", a.saveBossBattleOpening)
	call := func(revision int, config map[string]any, status int) {
		t.Helper()
		testfixture.CallContentAdmin(t, router, "PUT", "/opening", map[string]any{"expected_revision": revision, "config": config}, status)
	}
	config := map[string]any{"global": map[string]any{"cost_initial": 7, "burst_gauge_initial": 300}, "bosses": map[string]any{"11": map[string]any{"burst_gauge_initial": 0}}}
	call(0, config, 200)
	check := func(store *contentStore, id, cost, gauge int) {
		t.Helper()
		for _, r := range store.configuration.State.TeamBattleReplays {
			if r.BossID == id {
				if r.CostInitial != cost || r.BurstGaugeInitial != gauge {
					t.Fatalf("%d: %d/%d", id, r.CostInitial, r.BurstGaugeInitial)
				}
				return
			}
		}
		t.Fatal("missing replay", id)
	}
	for _, id := range []int{11, 111} {
		check(o.content, id, 7, 0)
	}
	for _, id := range []int{12, 13} {
		check(o.content, id, 7, 300)
	}
	for _, id := range []int{77, 88, 20000001} {
		check(o.content, id, 3, 20)
	}
	check(o.content, 99, 3, 0) // Cup owns its own opening gauge.
	call(0, config, 409)
	for _, id := range []string{"77", "88", "99", "111", "20000001", "999"} {
		call(1, map[string]any{"global": map[string]any{}, "bosses": map[string]any{id: map[string]int{"cost_initial": 9}}}, 400)
	}
	for _, rule := range []map[string]int{{"cost_initial": 2}, {"cost_initial": 11}, {"burst_gauge_initial": -1}, {"burst_gauge_initial": 301}} {
		call(1, map[string]any{"global": rule, "bosses": map[string]any{}}, 400)
	}
	check(o.content, 12, 7, 300)
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	check(restarted.content, 111, 7, 0)
	check(restarted.content, 13, 7, 300)
	if base.TeamBattleReplays[0].CostInitial != 3 || base.TeamBattleReplays[0].BurstGaugeInitial != 20 {
		t.Fatal("resource defaults mutated")
	}
	call(1, map[string]any{"global": map[string]int{"cost_initial": 8}, "bosses": map[string]any{}}, 200)
	check(o.content, 11, 8, 20) // Clearing the override restores each field's resource default.
	call(2, map[string]any{"global": map[string]any{}, "bosses": map[string]any{}}, 200)
	check(o.content, 11, 3, 20)
}
