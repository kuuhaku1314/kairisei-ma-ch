package cnbootstrap

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCNContainerSellKeepsOriginalResponseShape(t *testing.T) {
	input := []byte(`{"res_code":0}
{"card_container_num":2,"get_gold":5,"gold":10,"extract_reward":[{"reward":{"type":8,"num":50,"reward_typeid":8000}}],"is_extract_reward_in_present_box":1,"decks":[]}
{}`)
	content, err := adaptCNCardContainerSellResponse(input)
	if err != nil {
		t.Fatal(err)
	}
	segments := strings.Split(string(content), "\n")
	if len(segments) != 3 {
		t.Fatalf("unexpected response: %s", content)
	}
	var method map[string]json.RawMessage
	if err := json.Unmarshal([]byte(segments[1]), &method); err != nil {
		t.Fatal(err)
	}
	if len(method) != 3 || string(method["card_container_num"]) != "2" ||
		string(method["get_gold"]) != "5" || string(method["gold"]) != "10" {
		t.Fatalf("CN container sell received JP-only fields: %s", segments[1])
	}
}
