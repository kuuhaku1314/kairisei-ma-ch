package masterdata_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func TestAvatarCostumeCatalogFromResourceMaster(t *testing.T) {
	path := testfixture.WriteTestAvatarMaster(t, t.TempDir())
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	write := func(rows []gamestate.CollectionRewardDefinition) {
		t.Helper()
		document["costume_rewards"], err = json.Marshal(rows)
		if err != nil {
			t.Fatal(err)
		}
		content, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want := []gamestate.CollectionRewardDefinition{{Type: 14, ID: 990, Name: "Resource Costume", PictID: 49990}}
	write(want)
	master, err := masterdata.LoadAvatarRuntimeMaster(path)
	if err != nil {
		t.Fatal(err)
	}
	state := testfixture.RuntimeState(t)
	owned := append([]byte(nil), state.Costume...)
	if err := masterdata.ApplyAvatarRuntimeMaster(&state, master); err != nil {
		t.Fatal(err)
	}
	var costumes []gamestate.CollectionRewardDefinition
	for _, row := range state.CollectionRewards {
		if row.Type == 14 {
			costumes = append(costumes, row)
		}
	}
	if !reflect.DeepEqual(costumes, want) || string(state.Costume) != string(owned) {
		t.Fatal("resource catalog was supplemented or changed account ownership")
	}
	for _, rows := range [][]gamestate.CollectionRewardDefinition{nil, {want[0], want[0]}, {{Type: 14, ID: 990, Name: "Missing icon"}}} {
		write(rows)
		if _, err := masterdata.LoadAvatarRuntimeMaster(path); err == nil {
			t.Fatalf("accepted invalid costume catalog: %+v", rows)
		}
	}
}

func TestApplyAvatarShopAssetClosureRequiresIconAndModel(t *testing.T) {
	t.Parallel()
	state := gamestate.State{AvatarPartDefinitions: []gamestate.AvatarPartDefinition{
		{PartID: 50, IconPictID: 50},
		{PartID: 602, IconPictID: 602},
		{PartID: 790, IconPictID: 790},
	}}
	available := map[string]struct{}{
		"avatar_parts/icon/avatar_00050_icon.dat":   {},
		"avatar_parts/icon/avatar_00602_icon.dat":   {},
		"avatar_parts/parts/avatar_00602_parts.dat": {},
		"avatar_parts/parts/avatar_00790_parts.dat": {},
	}
	if err := masterdata.ApplyAvatarShopAssetClosure(&state, available); err != nil {
		t.Fatal(err)
	}
	if want := []int{602}; !reflect.DeepEqual(state.AvatarShopPartIDs, want) {
		t.Fatalf("asset-closed shop IDs = %v, want %v", state.AvatarShopPartIDs, want)
	}
}

func TestMissingAvatarAssetsCannotBePurchased(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.AvatarPartDefinitions = append(state.AvatarPartDefinitions, gamestate.AvatarPartDefinition{PartID: 99, IconPictID: 99})
	state.AvatarShopPartIDs = []int{99}
	if err := masterdata.ApplyAvatarShopAssetClosure(&state, map[string]struct{}{"main_c/scenes/scene_Menu.dat": {}}); err != nil {
		t.Fatal(err)
	}
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	before := account.Snapshot(state)
	if len(account.AvatarShopState()) != 0 {
		t.Fatal("missing Avatar assets were published for purchase")
	}
	if _, err := account.BuyAvatarPart(99, 0); err == nil {
		t.Fatal("unpublished Avatar part was purchased")
	}
	if after := account.Snapshot(state); before.User.Gold != after.User.Gold || !reflect.DeepEqual(before.AvatarParts, after.AvatarParts) {
		t.Fatal("rejected Avatar purchase changed the account")
	}
}
