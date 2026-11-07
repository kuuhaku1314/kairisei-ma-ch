package masterdata

import (
	"encoding/json"
	"reflect"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestConfigurableOwnDeckReusesEncounterAndRetainsDefaults(t *testing.T) {
	const source = 30100101
	const own = source + ownDeckBossIDOffset
	master := BattleRuntimeMaster{
		Groups:          []json.RawMessage{json.RawMessage(`{"0":301001,"10":[{"0":30100101,"1":0,"5":7,"6":4,"7":1,"10":0,"24":0},{"0":30100102,"1":0,"24":0},{"0":21,"1":1,"24":1},{"0":22,"1":0,"24":3},{"0":23,"1":0,"24":0}]}`)},
		PastBossGroups:  []json.RawMessage{json.RawMessage(`{"0":301001,"13":[{"0":30100101,"1":0,"5":7,"6":4,"7":1,"24":0}]}`)},
		OwnDeckVariants: []battleOwnDeckVariant{{SourceBossID: 30100102, BossID: 130100102}},
		Replays:         []gamestate.TeamBattleReplay{{BossID: source, EnemyPartyID: 500, Seed: 77, Battles: []gamestate.TeamBattleReplayBattle{{EnemyPartyID: 501}}}},
		Rewards:         []gamestate.TeamBattleRewardProfile{{BossID: source, ResultRewards: []gamestate.Reward{{Type: 4, Num: 25}}}, {BossID: 30100102}, {BossID: 21}, {BossID: 22}, {BossID: 23, TowerID: 1}},
	}
	prepared, err := withConfigurableOwnDeckEntries(master)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prepared.OptionalOwnDeckBossIDs, []int{own}) || len(prepared.OwnDeckVariants) != 2 || len(master.OwnDeckVariants) != 1 {
		t.Fatal("existing defaults, special entries, or source master changed")
	}
	again, err := withConfigurableOwnDeckEntries(prepared)
	if err != nil || !reflect.DeepEqual(again, prepared) {
		t.Fatal("preparation duplicated entries", err)
	}
	view, err := ExpandBattleEntries(prepared)
	if err != nil {
		t.Fatal(err)
	}
	var group struct {
		Bosses []game.TeamBattleEntryRules `json:"10"`
	}
	if err := json.Unmarshal(view.Groups[0], &group); err != nil {
		t.Fatal(err)
	}
	var rule game.TeamBattleEntryRules
	for _, entry := range group.Bosses {
		if entry.BossID == own {
			rule = entry
		}
	}
	if rule.BossID != own || rule.OnlyMyDeck != 1 || !rule.AllowsSolo() || rule.AllowsMultiplayer() || rule.BPUse != 7 || rule.Continue != 1 {
		t.Fatal("own-deck entry did not use the native solo rule")
	}
	replay := view.Replays[len(view.Replays)-1]
	replay.BossID = source
	if !reflect.DeepEqual(replay, master.Replays[0]) {
		t.Fatal("mode variant changed enemy waves or RNG seed")
	}
	for _, reward := range view.Rewards {
		if reward.BossID == own {
			reward.BossID = source
			if !reflect.DeepEqual(reward, master.Rewards[0]) {
				t.Fatal("mode variant changed reward content")
			}
		}
	}
	master.Replays = append(master.Replays, gamestate.TeamBattleReplay{BossID: own})
	if _, err := withConfigurableOwnDeckEntries(master); err == nil {
		t.Fatal("own-deck identity collision accepted")
	}
}
