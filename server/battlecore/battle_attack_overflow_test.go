package battlecore

import (
	"strconv"
	"testing"
)

func TestNativeAttackOverflowAndSignedAttributeDifference(t *testing.T) {
	// Direct 25/60 output from D-329 v3's original x86 API chains, using
	// official body 30909171 (DARK rate 200, DEF/fixed defense 200000 each).
	for _, tc := range []struct{ input, power, damage, difference int }{
		{100000000, 14100654, 27801308, 14100654},
		{21474836, 21474836, 42549672, 21474836},
		{21474837, -21474835, 1, -21474835},
		{-21474837, 21474835, 42549670, 21474835},
		{16777217, 16777217, 33154434, 16777217},
	} {
		t.Run(strconv.Itoa(tc.input), func(t *testing.T) {
			role := CombatSkillRole{Function: "ATTACK_AA", Parameters: [10]string{strconv.Itoa(tc.input)}}
			power := attackRolePower(role, 1, 0)
			damage, difference := nativeAttributeAttackDamage(power, 200, 100, 200000, -200000)
			if power != tc.power || damage != tc.damage || difference != tc.difference {
				t.Fatalf("power/damage/attribute delta=%d/%d/%d, original=%d/%d/%d",
					power, damage, difference, tc.power, tc.damage, tc.difference)
			}
		})
	}
}
