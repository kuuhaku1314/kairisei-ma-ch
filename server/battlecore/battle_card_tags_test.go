package battlecore

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"kairisei.local/server/battlecontent"
)

func TestCardProfileTagsDriveEnhancedUserAttack(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	row := make([]string, 113)
	row[0], row[38] = "1", "1"
	row[68], row[69], row[75] = "-1", "30020003", "30020004"
	card, err := battlecontent.ParseCard(row)
	if err != nil || card.ProfileTags[0] != 0 || card.ProfileTags[1] != 30020003 || card.ProfileTags[7] != 30020004 {
		t.Fatalf("wrong profile CSV mapping: %+v, %v", card, err)
	}
	engine.catalog.Cards[1] = card
	base := engine.catalog.Skills[1][0]
	enhanced := base
	enhanced.BranchCondition = "FRIEND_PLAY_TAG"
	enhanced.BranchParameters = [5]string{"30020003", "30020004", "0", "2"}
	enhanced.BranchPriority, enhanced.FunctionID = 1, 2
	engine.catalog.Skills[1] = []CombatSkillDefinition{base, enhanced}
	role := engine.catalog.SkillRoles[1][0]
	role.Parameters[0] = "500"
	engine.catalog.SkillRoles[2] = []CombatSkillRole{role}
	engine.phase, engine.turn = PhaseUser, 1
	engine.enemies[0].HP, engine.enemies[0].MaxHP = 10000, 10000
	engine.players[0].Hand[0], engine.players[0].Hand[1] = 1, 2
	engine.selectedPlays = map[int]cardPlaySubmission{1: {CardTypes: [5]int{1, 2}, Targets: [5]int{5, 5}}, 2: {}, 3: {}, 4: {}}
	if _, err := engine.UserAttack(); err != nil {
		t.Fatal(err)
	}
	// Both selected cards have this tag; multiple tags on each still count
	// once. The later card continues to see the first selection after play.
	if engine.enemies[0].HP != 9000 {
		t.Fatalf("tag-enhanced two-card action damage=%d, want1000", 10000-engine.enemies[0].HP)
	}
	delete(engine.catalog.Cards, 1)
	action := battleAction{memberType: 1, cardID: 1, cardType: 1, skill: base}
	action.skill.Groups = [3]int{30020003}
	if engine.branchConditionSatisfied("FRIEND_PLAY_TAG", [5]string{"30020003", "0", "0", "1"}, action, []battleAction{action}, nil) {
		t.Fatal("target-hand skill groups must never substitute for card profile tags")
	}
}

func TestNativeCardTagBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_CARD_TAG_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_CARD_TAG_RECEIPT to the original tag selector receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	type cardInput struct {
		Member, HP, Normal, Bonus int
		Tags                      []int
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Vectors      []struct {
			Name       string
			Parameters []int
			Cards      []cardInput
			Result     int
		}
		DeckVectors []struct {
			Attribute, Tag, Count int
			Cards                 []cardInput
		} `json:"deck_vectors"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-card-profile-tags" || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) == 0 {
		t.Fatal("invalid original card tag receipt")
	}
	for _, vector := range receipt.Vectors {
		engine, _ := coreBattleFixture(t)
		var params [5]string
		for i, value := range vector.Parameters {
			params[i] = strconv.Itoa(value)
		}
		var actions []battleAction
		for i, input := range vector.Cards {
			definition := CombatCardDefinition{ID: i + 1}
			copy(definition.ProfileTags[:], input.Tags)
			engine.catalog.Cards[i+1] = definition
			actions = append(actions, battleAction{memberType: input.Member, cardType: i + 1, cardID: i + 1})
			engine.players[input.Member-1].HP = input.HP
		}
		got := engine.branchConditionSatisfied("FRIEND_PLAY_TAG", params, battleAction{memberType: 1, cardType: 1}, actions, nil)
		if got != (vector.Result == 0) {
			t.Fatalf("%s: Go=%t original=%d", vector.Name, got, vector.Result)
		}
	}
	attributes := map[int]string{0: "NULL", 1: "FIRE", 2: "ICE", 3: "WIND", 4: "LIGHT", 5: "DARK", 102: "FIRE_ICE"}
	for _, vector := range receipt.DeckVectors {
		engine, _ := coreBattleFixture(t)
		engine.players[0].Deck = [10]BattleCard{}
		for i, input := range vector.Cards {
			definition := CombatCardDefinition{ID: i + 1, NormalSkillID: i*2 + 1, ArthurSkillID: i*2 + 2}
			copy(definition.ProfileTags[:], input.Tags)
			engine.catalog.Cards[i+1] = definition
			engine.catalog.Skills[i*2+1] = []CombatSkillDefinition{{Attribute: attributes[input.Normal]}}
			engine.catalog.Skills[i*2+2] = []CombatSkillDefinition{{Attribute: attributes[input.Bonus], Job: "SINGER"}}
			engine.players[0].Deck[i] = BattleCard{CardType: i + 1, CardID: i + 1}
		}
		bonus := CombatSkillRole{Function: "PARAM_UP_SKILL_BONUS", Parameters: [10]string{"ATK", "0", "1", "0", attributes[vector.Attribute], strconv.Itoa(vector.Tag)}}
		action := battleAction{memberType: 1, cardLevel: 1, skillBonus: &battleSkillBonus{}}
		if _, err := engine.executeBurstRole(action, bonus, nil); err != nil {
			t.Fatal(err)
		}
		role := CombatSkillRole{Function: "ATK_UP_FIXED", Parameters: [10]string{"2", "ATK", "1000", "100"}}
		if got := engine.fixedParameterActionValue(action, role, 1); got != 100+vector.Count || action.skillBonus.active {
			t.Fatalf("deck attribute=%d tag=%d Go=%d original=%d", vector.Attribute, vector.Tag, got, 100+vector.Count)
		}
	}
}
