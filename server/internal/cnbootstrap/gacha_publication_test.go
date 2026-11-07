package cnbootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/operations"
	"kairisei.local/server/internal/testfixture"
)

func TestCNCardAcquisitionFollowsCurrentGachaGroupPublication(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	runtime, err := operations.NewRuntime(accounts.Database())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.CloseMaintenance()
	state := testfixture.RuntimeState(t)
	cardID := state.Cards[0].CardID
	state.TeamBattleRewards = nil
	state.Gachas = []gamestate.GachaProfile{
		{GachaID: 101, GroupID: 1, Name: "卡池一", PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1, CardIDs: []int{cardID}, CardWeights: []int{1}},
		{GachaID: 202, GroupID: 2, Name: "卡池二", PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1, CardIDs: []int{cardID}, CardWeights: []int{1}},
	}
	business, err := httpapi.New(httpapi.Config{InitialState: state, BaseURL: "http://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	handler := cnBootstrapHowToGetCardShow(business, runtime)
	for _, open := range []int{2, 1, 0, 2} {
		active := map[int]struct{}{}
		if open != 0 {
			active[open] = struct{}{}
		}
		runtime.Publish(operations.Publication{GachaGroups: map[int]int{101: 1, 202: 2}, ManagedGachaGroups: map[int]struct{}{1: {}, 2: {}}, DefaultGachaGroups: active})
		request := httptest.NewRequest(http.MethodPost, "/HowToGetCardShow", strings.NewReader(fmt.Sprintf(`test-session-00000000{"cardids":[%d]}`, cardID)))
		request = request.WithContext(context.WithValue(request.Context(), cnAuthenticatedUserKey{}, accountstore.PrimaryUserID))
		response := httptest.NewRecorder()
		handler(response, request)
		lines := bytes.Split(bytes.TrimSpace(response.Body.Bytes()), []byte{'\n'})
		var body struct {
			Lists []struct {
				Entries []struct {
					Type int `json:"type"`
					ID   int `json:"contentid"`
				} `json:"get_cards"`
			} `json:"how_to_list"`
		}
		if response.Code != http.StatusOK || len(lines) != 3 || json.Unmarshal(lines[1], &body) != nil || len(body.Lists) != 1 {
			t.Fatal("invalid card acquisition response", response.Body.String())
		}
		ids := []int{}
		for _, entry := range body.Lists[0].Entries {
			if entry.Type == 7 {
				ids = append(ids, entry.ID)
			}
		}
		if open == 0 && len(ids) != 0 || open != 0 && (len(ids) != 1 || ids[0] != open*101) {
			t.Fatalf("publication %d returned gacha sources %v", open, ids)
		}
	}
}

func TestFilterCNGachaPublicationKeepsBaseAndEnabledManagedPools(t *testing.T) {
	method := map[string]json.RawMessage{
		"gacha_category_list": json.RawMessage(`[
			{"category_num":1,"pictid":1},
			{"category_num":2,"pictid":2},
			{"category_num":3,"pictid":3}
		]`),
		"gacha_list": json.RawMessage(`[
			{"gachaid":1,"groupid":1,"category_num":1},
			{"gachaid":100,"groupid":100,"category_num":2},
			{"gachaid":200,"groupid":200,"category_num":3}
		]`),
	}
	active := map[int]struct{}{200: {}}
	managed := map[int]struct{}{100: {}, 200: {}}

	if err := filterCNGachaPublication(method, active, managed); err != nil {
		t.Fatal(err)
	}
	var gachas []struct {
		GroupID int `json:"groupid"`
	}
	if err := json.Unmarshal(method["gacha_list"], &gachas); err != nil {
		t.Fatal(err)
	}
	if len(gachas) != 2 || gachas[0].GroupID != 1 || gachas[1].GroupID != 200 {
		t.Fatalf("filtered gachas = %+v", gachas)
	}
	var categories []struct {
		CategoryNum int `json:"category_num"`
	}
	if err := json.Unmarshal(method["gacha_category_list"], &categories); err != nil {
		t.Fatal(err)
	}
	if len(categories) != 2 || categories[0].CategoryNum != 1 || categories[1].CategoryNum != 3 {
		t.Fatalf("filtered categories = %+v", categories)
	}
}
