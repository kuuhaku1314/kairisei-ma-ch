package httpapi

import (
	"reflect"
	"testing"
)

func TestTeamBattleBossNamedDTORetainsEntryExtensions(t *testing.T) {
	entries := []any{map[string]any{
		"slot1_arthur_type": 5, "slot2_arthur_type": 2,
		"slot3_arthur_type": -1, "slot4_arthur_type": 0,
		"slot1_partner_type": 0, "slot2_partner_type": 2,
		"slot3_partner_type": 0, "slot4_partner_type": 1,
	}}
	source := map[string]any{
		"0": 123, "19": "S", "24": 1,
		"entry_limit": entries, "costume_fix_type": 4,
	}
	result, err := teamBattleBossNamedDTO(source)
	if err != nil {
		t.Fatal(err)
	}
	if result["bossid"] != 123 || result["needrank"] != "S" || result["start_rule"] != 1 {
		t.Fatal("existing CN numeric fields changed")
	}
	if !reflect.DeepEqual(result["entry_limit"], entries) || result["costume_fix_type"] != 4 {
		t.Fatal("named BOSS extensions were lost or remapped")
	}
	delete(source, "entry_limit")
	delete(source, "costume_fix_type")
	legacy, err := teamBattleBossNamedDTO(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := legacy["entry_limit"]; exists {
		t.Fatal("an ordinary BOSS acquired entry restrictions")
	}
}
