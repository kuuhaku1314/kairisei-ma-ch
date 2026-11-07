package masterdata_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func TestUserDerivedMOCNavigatorsUseNativeCatalogWithoutGrantingOwnership(t *testing.T) {
	path := testfixture.WriteTestNaviMaster(t, t.TempDir())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	rows := document["navigators"].([]any)
	for i, folder := range []string{"kakusan_fei", "kakusan_lifei", "kakusan_aier"} {
		id := 71 + i
		rows = append(rows, map[string]any{
			"navi_id": id, "name": folder, "pict_id": 50000000 + id*1000,
			"item_pict_id": 92000000 + id, "presentation_kind": 0,
			"live2d_folder": folder, "live2d_bundle": "live2d/live2d_" + folder + ".dat",
			"live2d_source_state": "PRESENT_IN_USER_DERIVED_OVERLAY", "voice_id": id, "client_published": true,
		})
	}
	document["navigators"] = rows
	document["publication_policy"].(map[string]any)["evidence"] = "INFERRED"
	summary := document["summary"].(map[string]any)
	summary["imported_navigator_rows"], summary["client_published"] = 3, len(rows)
	write := func() {
		t.Helper()
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	master, err := masterdata.LoadNaviRuntimeMaster(path)
	if err != nil {
		t.Fatal(err)
	}
	state := gamestate.State{}
	state.User.NaviUnlockFlag, state.User.SelectableNaviIDs = 1, []int8{0}
	if err := masterdata.ApplyNaviRuntimeMaster(&state, master); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int8{71, 72, 73} {
		if !slices.Contains(state.User.NaviCatalogIDs, id) {
			t.Fatalf("new navigator %d is absent from the native purchase catalog", id)
		}
	}
	if state.User.NaviUnlockFlag != 1 || !slices.Equal(state.User.SelectableNaviIDs, []int8{0}) {
		t.Fatal("publishing navigators granted ownership")
	}
	if state.User.NaviPurchasePrice != master.LocalPurchase.Price {
		t.Fatal("publishing navigators changed the configured purchase price")
	}
	row := rows[len(rows)-1].(map[string]any)
	reject := func(reason string) {
		t.Helper()
		write()
		if _, err := masterdata.LoadNaviRuntimeMaster(path); err == nil {
			t.Fatalf("accepted invalid navigator master: %s", reason)
		}
	}
	for _, invalid := range []struct {
		kind   int
		source string
	}{
		{1, "PRESENT_IN_USER_DERIVED_OVERLAY"},
		{2, "PRESENT_IN_USER_DERIVED_OVERLAY"},
		{0, "ABSENT_FROM_CN_OFFICIAL_ASSET_MAP"},
	} {
		row["presentation_kind"], row["live2d_source_state"] = invalid.kind, invalid.source
		reject("presentation/source mismatch")
	}
	row["presentation_kind"], row["live2d_source_state"] = 0, "PRESENT_IN_USER_DERIVED_OVERLAY"
	policy := document["publication_policy"].(map[string]any)
	policy["evidence"] = "CONFIRMED_CN_OFFICIAL_ASSET_MAP"
	reject("user-derived model marked as official")
	policy["evidence"] = "INFERRED"
	bundle := row["live2d_bundle"]
	row["live2d_bundle"] = "live2d/live2d_wrong_owner.dat"
	reject("bundle owner differs from the navigator folder")
	row["live2d_bundle"] = bundle
	summary["imported_navigator_rows"] = 2
	reject("derived navigator summary differs from the published rows")
}
