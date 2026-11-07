package admin

import (
	"encoding/json"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestTowerDropIdentityAtomicSaveAndRestart(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reward := gamestate.Reward{Type: 8, RewardTypeID: 1000, Num: 1, CardSkillLevels: []int16{}}
	first := gamestate.Reward{Type: 4, Num: 100, CardSkillLevels: []int16{}}
	boss := json.RawMessage(`{"0":11,"11":"本体","12":[{"0":21,"1":0}]}`)
	base := gamestate.State{TeamBattleSolo: json.RawMessage(`{"9":[],"10":[{"0":9,"4":"普通组","10":[{"0":11,"4":"普通"}]}],"11":[],"12":[]}`)}
	base.TeamBattleRewards = []gamestate.TeamBattleRewardProfile{{BossID: 11, EnemyDrops: []gamestate.TeamBattleEnemyDrop{{Reward: reward}}}}
	for _, id := range []int{1, 2} {
		base.TowerQuestProfiles = append(base.TowerQuestProfiles, gamestate.TowerQuestProfile{TowerID: id, Name: "塔", Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Boss: boss, ClearRewards: []gamestate.Reward{first}}}})
		base.TeamBattleRewards = append(base.TeamBattleRewards, gamestate.TeamBattleRewardProfile{BossID: 11, TowerID: id, TowerFloor: 1, EnemyDrops: []gamestate.TeamBattleEnemyDrop{{Reward: reward}}, FirstClearRewards: []gamestate.Reward{first}})
	}
	if err = ops.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	for key, row := range ops.content.towerBosses {
		row.Targets = []dropTarget{{BattleIndex: 0, EnemyIndex: 0, Name: "本体"}}
		ops.content.towerBosses[key] = row
	}
	api := &API{operations: ops, catalogByKey: map[string]AdminCatalogEntry{"8:1000": {Name: "药水"}}}
	router := chi.NewRouter()
	router.Get("/towers", api.towerDropEditor)
	router.Put("/towers", api.saveTowerDropEditor)
	var listing struct {
		Towers []struct {
			TowerID int        `json:"tower_id"`
			Floors  []DropBoss `json:"floors"`
		} `json:"towers"`
	}
	if err = json.Unmarshal(testfixture.CallContentAdmin(t, router, "GET", "/towers", nil, 200), &listing); err != nil || len(listing.Towers) != 2 || listing.Towers[0].Floors[0].Floor != 1 {
		t.Fatal("tower/floor directory", err)
	}
	testfixture.CallContentAdmin(t, router, "GET", "/towers?tower_id=1&floor=2", nil, 404)
	reward.Num = 3
	first.Num = 700
	firstRewards := []gamestate.Reward{first}
	config := TowerFloorDrops{TowerID: 1, Floor: 1, BossDrops: BossDrops{BossID: 11, Drops: []gamestate.TeamBattleEnemyDrop{{Reward: reward}}}, FirstClearRewards: &firstRewards}
	save := func(version int, configs []TowerFloorDrops, status int) {
		testfixture.CallContentAdmin(t, router, "PUT", "/towers", map[string]any{"expected_revision": version, "configs": configs}, status)
	}
	save(0, []TowerFloorDrops{config}, 200)
	projected := ops.content.configuration.State
	if projected.TeamBattleRewards[0].EnemyDrops[0].Reward.Num != 1 || projected.TeamBattleRewards[2].EnemyDrops[0].Reward.Num != 1 || projected.TeamBattleRewards[1].EnemyDrops[0].Reward.Num != 3 || projected.TeamBattleRewards[1].FirstClearRewards[0].Num != 700 {
		t.Fatal("shared boss ID crossed reward identities")
	}
	var view map[string]json.RawMessage
	if err = json.Unmarshal(projected.TowerQuestProfiles[0].Floors[0].Boss, &view); err != nil || string(view["12"]) != "[]" || projected.TowerQuestProfiles[0].Floors[0].ClearRewards[0].Num != 700 {
		t.Fatal("tower preview not projected", err)
	}
	if base.TowerQuestProfiles[0].Floors[0].ClearRewards[0].Num != 100 || base.TeamBattleRewards[1].EnemyDrops[0].Reward.Num != 1 {
		t.Fatal("shared base mutated")
	}
	bad := config
	bad.TowerID = 2
	bad.BossID = 99
	save(1, []TowerFloorDrops{config, bad}, 400)
	save(0, []TowerFloorDrops{config}, 409)
	if ops.content.towerDropRevision != 1 {
		t.Fatal("failed transaction advanced revision")
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	if got := restarted.content.configuration.State.TeamBattleRewards[1]; got.EnemyDrops[0].Reward.Num != 3 || got.FirstClearRewards[0].Num != 700 {
		t.Fatal("tower overrides lost on restart")
	}
	empty := []gamestate.Reward{}
	config.FirstClearRewards = &empty
	save(1, []TowerFloorDrops{config}, 200)
	if len(ops.content.configuration.State.TeamBattleRewards[1].FirstClearRewards) != 0 {
		t.Fatal("explicit empty first-clear ignored")
	}
	config.FirstClearRewards = nil
	save(2, []TowerFloorDrops{config}, 200)
	if ops.content.configuration.State.TeamBattleRewards[1].FirstClearRewards[0].Num != 100 {
		t.Fatal("default first-clear was not restored")
	}
	// Retiring a tower removes its editor/runtime identity; its stored operator
	// history must neither prevent startup nor attach to a replacement tower.
	modern := base
	modern.TowerQuestProfiles = append([]gamestate.TowerQuestProfile{}, base.TowerQuestProfiles[0])
	modern.TowerQuestProfiles[0].TowerID = 71001
	modern.TeamBattleRewards = append([]gamestate.TeamBattleRewardProfile{}, base.TeamBattleRewards[:2]...)
	modern.TeamBattleRewards[1].TowerID = 71001
	replaced, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = replaced.InitializeContent(modern, ""); err != nil {
		t.Fatal("retired settings prevented content refresh", err)
	}
	if len(replaced.content.towerBosses) != 1 || replaced.content.towerBosses["71001:1"].TowerID != 71001 ||
		replaced.content.towerDrops["1:1"].TowerID != 1 ||
		replaced.content.configuration.State.TeamBattleRewards[1].EnemyDrops[0].Reward.Num != 1 {
		t.Fatal("retired settings became active or were lost")
	}
}
