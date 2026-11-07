package battlecore

import "testing"

func TestNativeResumeBuffParametersAreNotInitialValues(t *testing.T) {
	for _, tc := range []struct {
		function string
		want     [4]int64
	}{
		{"ATTACK_BARRIER", [4]int64{1, 2}},
		{"ATTACK_BARRIER_APPOINT_ATTR", [4]int64{1, 2}},
		{"REFLECTION", [4]int64{1}},
		{"GUTS", [4]int64{2}},
		{"ATK_UP_FIXED", [4]int64{}},
		{"ATTR_DEF_DOWN", [4]int64{}},
		{"WEAKNESS", [4]int64{}},
		{"CRITICAL_UP", [4]int64{}},
		{"REGENERATE_FIXED", [4]int64{}},
		{"DEAL_BONUS", [4]int64{}},
	} {
		t.Run(tc.function, func(t *testing.T) {
			effect := battleEffect{Function: tc.function, Remaining: 3, Value: 1234,
				Uses: 2, DamageKind: "MAGIC", Attribute: "FIRE", Parameters: [4]int{1234, 4, 5, 6}}
			before := effect
			row, ok := resumeBuffResult(1, effect)
			if !ok || len(row.Args) != 11 {
				t.Fatalf("missing resume row: %+v", row)
			}
			if got := [4]int64(row.Args[7:11]); got != tc.want {
				t.Fatalf("numeric fields %v, want %v", got, tc.want)
			}
			if effect != before {
				t.Fatal("UI projection mutated retained combat state")
			}
			if tc.function == "ATTACK_BARRIER" || tc.function == "GUTS" {
				effect.Uses--
				row, _ = resumeBuffResult(1, effect)
				index := 7
				if tc.function == "ATTACK_BARRIER" {
					index = 8
				}
				if row.Args[index] != 1 {
					t.Fatal("resume returned the original rather than remaining count")
				}
			}
		})
	}
}

func TestNativeGoodStatusNotificationKeepsCombatValuesPrivate(t *testing.T) {
	for _, tc := range []struct {
		function string
		want     [4]int64
	}{
		{"ATTACK_BARRIER", [4]int64{1, 2}},
		{"REFLECTION", [4]int64{1}},
		{"GUTS", [4]int64{2}},
		{"REGENERATE_FIXED", [4]int64{}},
		{"COVERING", [4]int64{}},
		{"CARD_SEAL_REGIST", [4]int64{}},
		{"DARKNESS_REGIST", [4]int64{}},
		{"ENDURE", [4]int64{}},
		{"CRITICAL_DAMAGE_BOOST", [4]int64{}},
		{"WEAKNESS", [4]int64{}},
		{"COST_BLOCK", [4]int64{}},
	} {
		t.Run(tc.function, func(t *testing.T) {
			effect := battleEffect{Function: tc.function, Remaining: 3, Value: 1234,
				Uses: 2, DamageKind: "MAGIC", Parameters: [4]int{1234, 2}}
			row := battlePersistentResult(1, CombatSkillRole{Function: tc.function}, battleBuffCodes[tc.function], effect)
			if got := [4]int64(row.Args[7:11]); got != tc.want {
				t.Fatalf("numeric fields %v, want %v", got, tc.want)
			}
		})
	}
}
