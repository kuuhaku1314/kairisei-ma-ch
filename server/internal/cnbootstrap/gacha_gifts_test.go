package cnbootstrap

import (
	"encoding/json"
	"testing"
)

func TestGachaGiftWrappersFollowOriginalNumericReceiver(t *testing.T) {
	rows, err := remapCNGachaInfos(json.RawMessage(`[{"gachaid":7,"category_num":9,"play_count_1day":1,"play_count_1day_max":2,"stepup_count":3,"is_last_step":1,"gifts":[
		{"is_random":0,"rewards":[{"is_empty":0,"reward":{"type":8,"num":1,"reward_typeid":6202,"card_lv":0,"card_fame":0,"card_love":0,"card_skill_lv":[]}}]},
		{"is_random":1,"rewards":[{"is_empty":1,"reward":{"type":0,"num":0,"reward_typeid":0,"card_lv":0,"card_fame":0,"card_love":0,"card_skill_lv":[]}}]}
	]}]`))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	// ProtoGen.GachaShowReceive: GachaInfo[26] -> gift[1] -> entry[1]
	// -> RewardInfo[2]. Named wrappers would silently lose the advertised gift.
	var parsed []map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	row := parsed[0]
	gift := row["26"].([]any)[0].(map[string]any)
	entry := gift["1"].([]any)[0].(map[string]any)
	reward := entry["1"].(map[string]any)
	if row["30"] != float64(3) || row["31"] != float64(1) || reward["2"] != float64(6202) || len(reward["6"].([]any)) != 0 {
		t.Fatalf("invalid original gift DTO: %s", body)
	}
	random := row["26"].([]any)[1].(map[string]any)
	empty := random["1"].([]any)[0].(map[string]any)
	if row["4"] != float64(9) || row["16"] != float64(1) || row["18"] != float64(2) || random["0"] != float64(1) || empty["0"] != float64(1) {
		t.Fatalf("category, daily quota or random gift markers were lost: %s", body)
	}
}
