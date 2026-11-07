package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"testing"
)

// The retained CN scalar positions must not shift when modern parameter
// families pass through the same card adapter as named extensions.
func TestCardParameterFamiliesAcrossIndexedAndNamedResponses(t *testing.T) {
	card := json.RawMessage(`{"0":9223372036,"1":123,"6":[1,2],"7":901,"10":904,"11":[{"hp":101,"atkp":102,"intp":103,"mndp":104}],"12":[{"summonsid":17,"hp":201,"atkp":202,"intp":203,"mndp":204,"defp":205,"mdefp":206}],"13":1301,"19":90,"20":2,"21":[{"hp":701,"atkp":702}]}`)
	for _, spec := range []struct {
		name                                 string
		adapt                                func([]byte) ([]byte, error)
		field, id, next, fame, slot, passive string
	}{
		{"show", adaptCNCardShow2Response, "1", "0", "11", "17", "18", "19"},
		{"move", adaptCNCardMoveResponse, "upd_cards", "uniqid", "next_lv_exp", "fame", "slot", "passive"},
	} {
		t.Run(spec.name, func(t *testing.T) {
			input := append([]byte("{\"res_code\":0}\n{\"0\":["), card...)
			input = append(input, []byte("]}\n{}")...)
			body, err := spec.adapt(input)
			if err != nil {
				t.Fatal(err)
			}
			var method map[string]json.RawMessage
			if err := json.Unmarshal(bytes.Split(body, []byte{'\n'})[1], &method); err != nil {
				t.Fatal(err)
			}
			var cards []map[string]json.RawMessage
			if err := json.Unmarshal(method[spec.field], &cards); err != nil || len(cards) != 1 {
				t.Fatalf("cards: %s %v", body, err)
			}
			got := cards[0]
			for key, want := range map[string]string{
				spec.id: "9223372036", spec.next: "1301", spec.fame: "90", spec.slot: "2", spec.passive: `[{"hp":701,"atkp":702}]`,
				"alchemist_param": `[{"hp":101,"atkp":102,"intp":103,"mndp":104}]`,
				"summons_param":   `[{"summonsid":17,"hp":201,"atkp":202,"intp":203,"mndp":204,"defp":205,"mdefp":206}]`,
			} {
				if string(got[key]) != want {
					t.Fatalf("%s = %s, want %s", key, got[key], want)
				}
			}
		})
	}
}
