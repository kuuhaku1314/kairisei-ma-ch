package accountstore_test

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestMissionMetadataKeepsProgressWithoutCachedDefinitionsOrPreallocatedMail(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.MissionDefinitions = map[int]gamestate.MissionDefinition{101: {MissionID: 101, Published: true, Title: "当前公共文案", ProgressMax: 1}}
	base, err := accountstore.EncodeAccountMetadata(state)
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]json.RawMessage
	if err := json.Unmarshal(base, &old); err != nil {
		t.Fatal(err)
	}
	old["engagement"] = json.RawMessage(`{"initialized":true,"missions":[{"info":{"missionid":101,"state":1,"progress_now":2,"clear_limit_time":0,"title":"旧缓存文案","rewards":[{"type":4,"num":999}]},"reward_present":{"presentid":123}}]}`)
	payload, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := accountstore.DecodeAccountSnapshot(payload)
	if err != nil {
		t.Fatal(err)
	}
	state.Engagement = decoded.Engagement
	fresh, err := accountstore.EncodeAccountMetadata(state)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(fresh, &metadata); err != nil {
		t.Fatal(err)
	}
	if _, exists := metadata["mission_definitions"]; exists {
		t.Fatal("public definitions entered account metadata")
	}
	var engagement struct {
		Missions []map[string]json.RawMessage `json:"missions"`
	}
	if err := json.Unmarshal(metadata["engagement"], &engagement); err != nil || len(engagement.Missions) != 1 || len(engagement.Missions[0]) != 1 {
		t.Fatal("mission save kept a cached reward mail", err)
	}
	var info map[string]int
	if err := json.Unmarshal(engagement.Missions[0]["info"], &info); err != nil || len(info) != 4 || info["missionid"] != 101 || info["state"] != 1 || info["progress_now"] != 2 {
		t.Fatal("mission save lost private progression or retained cached display fields", err)
	}
}
