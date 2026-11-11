package masterdata

import (
	"encoding/json"
	"slices"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestPastBossDropCatalogUsesObtainableCards(t *testing.T) {
	raw := json.RawMessage(`{"0":700400010,"4":"boss","7":[11],"8":[12],"13":[{"0":41,"9":900,"12":[{"0":11,"1":0}]}]}`)
	zero, possible := 0, 500000
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: 41,
		ResultRewards: []gamestate.Reward{{Type: 6, Num: 1, RewardTypeID: 11}},
		EnemyDrops: []gamestate.TeamBattleEnemyDrop{
			{Reward: gamestate.Reward{Type: 6, Num: 1, RewardTypeID: 21}, ChancePerMillion: &possible},
			{Reward: gamestate.Reward{Type: 6, Num: 1, RewardTypeID: 11}, ChancePerMillion: &zero},
			{Reward: gamestate.Reward{Type: 13, Num: 3, RewardTypeID: 20000001}},
		},
	}}
	transitions := []gamestate.EvolutionTransition{
		{FromCardID: 11, ToCardID: 12}, // similarly named, wrong family
		{FromCardID: 21, ToCardID: 22}, {FromCardID: 22, ToCardID: 23},
		{FromCardID: 21, ToCardID: 99, Type: 1, Materials: []gamestate.EvolutionMaterial{{CardID: 77, Num: 1}}},
		{FromCardID: 31, ToCardID: 32, Type: 1, Materials: []gamestate.EvolutionMaterial{{CardID: 21, Num: 1}}},
		{FromCardID: 31, ToCardID: 33, Type: 1, Materials: []gamestate.EvolutionMaterial{{CardID: 21, Num: 1}}},
		{FromCardID: 41, ToCardID: 42, Materials: []gamestate.EvolutionMaterial{{CardID: 21, Num: 1}}},
		{FromCardID: 51, ToCardID: 52, Type: 1, Materials: []gamestate.EvolutionMaterial{{CardID: 11, Num: 1}}},
		{FromCardID: 61, ToCardID: 62, Type: 1, Materials: []gamestate.EvolutionMaterial{{CardID: 21, Num: 1}}},
	}
	before, _ := json.Marshal(profiles)
	got, err := ProjectPastBossDropCatalog([]json.RawMessage{raw}, profiles, transitions)
	if err != nil {
		t.Fatal(err)
	}
	var group struct {
		Cards  []int `json:"7"`
		GuaiLi []int `json:"8"`
		Bosses []struct {
			Picture int `json:"9"`
			Cards   []struct {
				ID int `json:"0"`
			} `json:"12"`
		} `json:"13"`
	}
	if err := json.Unmarshal(got[0], &group); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(group.Cards, []int{21}) || !slices.Equal(group.GuaiLi, []int{31, 61}) ||
		len(group.Bosses[0].Cards) != 1 || group.Bosses[0].Cards[0].ID != 21 || group.Bosses[0].Picture != 900 {
		t.Fatalf("wrong actual reward/GuaiLi material users/portrait: %s", got[0])
	}
	after, _ := json.Marshal(profiles)
	if !slices.Equal(before, after) || string(raw) != `{"0":700400010,"4":"boss","7":[11],"8":[12],"13":[{"0":41,"9":900,"12":[{"0":11,"1":0}]}]}` {
		t.Fatal("view changed the source reward table or master")
	}
	profiles[0].EnemyDrops = []gamestate.TeamBattleEnemyDrop{}
	if _, err := ProjectPastBossDropCatalog([]json.RawMessage{raw}, profiles, transitions); err == nil {
		t.Fatal("an explicitly empty drop table must not advertise aggregate rewards")
	}
	profiles[0].EnemyDrops = nil
	got, err = ProjectPastBossDropCatalog([]json.RawMessage{raw}, profiles, transitions)
	if err != nil || json.Unmarshal(got[0], &group) != nil || !slices.Equal(group.Cards, []int{11}) || !slices.Equal(group.GuaiLi, []int{51}) {
		t.Fatalf("aggregate reward view failed: %v", err)
	}
	got, err = ProjectPastBossDropCatalog([]json.RawMessage{raw}, profiles, nil)
	if err != nil || json.Unmarshal(got[0], &group) != nil || !slices.Equal(group.GuaiLi, []int{0}) {
		t.Fatalf("archive without material users must leave the GuaiLi filter: %v", err)
	}
}
