package battlecore

import "testing"

func TestSkillQueryAbsentSelectionAndPass(t *testing.T) {
	// Original api_get_skill_extend_index, R080 native receipt: the empty
	// selection must not be inferred merely from a registered player.
	engine := &BattleEngine{rng: newXorShift128(602), selectedPlays: map[int]cardPlaySubmission{}}
	engine.players[0].MemberType = 1
	variants := make([]CombatSkillDefinition, 5)
	variants[0] = CombatSkillDefinition{FunctionID: 1}
	variants[3] = CombatSkillDefinition{FunctionID: 2, BranchPriority: 10, BranchCondition: "SELF_PLAY_MOST_LOW_COST", BranchParameters: [5]string{"2147483647", "2147483647"}}
	variants[4] = CombatSkillDefinition{FunctionID: 3, BranchPriority: 100, BranchCondition: "RANDOM", BranchParameters: [5]string{"100"}}
	rng := engine.rng
	if index, err := engine.SkillBranchIndex(1, variants); err != nil || index != 0 {
		t.Fatalf("no selection: %d %v", index, err)
	}
	engine.selectedPlays[1] = cardPlaySubmission{}
	if index, err := engine.SkillBranchIndex(1, variants); err != nil || index != 3 {
		t.Fatalf("PASS: %d %v", index, err)
	}
	if engine.rng != rng {
		t.Fatal("query consumed RNG")
	}
}
