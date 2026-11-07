package cnbootstrap

import (
	"encoding/json"
	"testing"
)

func TestIndexedUserInfoRetainsAlchemistDeckCapacity(t *testing.T) {
	value, err := remapCNUserInfo(json.RawMessage(`{"userid":12,"deck_max_per_arthur":15,"deck_max_alchemist":8,"tag_name":"骑士","total_cost":10,"jobs":[{"hp":1,"atkp":2,"intp":3,"mndp":4}]}`))
	if err != nil {
		t.Fatal(err)
	}
	row := value.(map[string]json.RawMessage)
	if string(row["41"]) != "15" || string(row["42"]) != `"骑士"` ||
		string(row["43"]) != "10" || string(row["deck_max_alchemist"]) != "8" {
		t.Fatalf("UserInfo envelope lost or shifted fields: %v", row)
	}
	var jobs []map[string]int
	if err := json.Unmarshal(row["40"], &jobs); err != nil || len(jobs) != 1 || jobs[0]["3"] != 4 {
		t.Fatalf("nested job fields changed: %s (%v)", row["40"], err)
	}
}
