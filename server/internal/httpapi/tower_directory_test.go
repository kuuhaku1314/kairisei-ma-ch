package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

// Opt-in integration of the actual selected catalog, using disposable fixture
// accounts. Native AI/formula evidence is reused rather than replayed here.
func TestSelectedTowerRuntimeIntegration(t *testing.T) {
	path := os.Getenv("CN602_TOWER_RUNTIME")
	if path == "" {
		t.Skip("selected tower runtime not supplied")
	}
	master, err := masterdata.LoadBattleRuntimeMaster(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(master.TowerProfiles) != 9 || master.Summary.TowerFloors != 367 {
		t.Fatal("selected catalog is incomplete")
	}
	state := testfixture.RuntimeState(t)
	cards, err := masterdata.LoadCardRuntimeMaster(os.Getenv("CN602_TOWER_CARD_MASTER"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = masterdata.ApplyCardRuntimeMaster(&state, cards); err != nil {
		t.Fatal(err)
	}
	items, err := masterdata.LoadItemRuntimeMaster(os.Getenv("CN602_TOWER_ITEM_MASTER"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = masterdata.ApplyItemRuntimeMaster(&state, items); err != nil {
		t.Fatal(err)
	}
	progression, err := masterdata.LoadPlayerProgressionRuntimeMaster("../../config/cn602-player-progression-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := masterdata.ApplyPlayerProgressionRuntimeMaster(&state, progression); err != nil {
		t.Fatal(err)
	}
	state.TowerQuestProgress = []gamestate.TowerQuestProgress{{TowerID: 1, Floor: 4, ClearedFloors: []int{1, 2, 3}, ClearRatings: map[int]int{2: 7}}}
	if err = masterdata.ApplyBattleRuntimeMaster(&state, master); err != nil {
		t.Fatal(err)
	}
	state.User.BP = 100000
	state.Items = append(state.Items, gamestate.Item{ItemID: 3022, Num: 1000})
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(account.TowerQuestDirectory()) != 9 {
		t.Fatal("new tower roots missing")
	}
	for _, entry := range account.TowerQuestDirectory() {
		if entry.TowerID == 1 {
			t.Fatal("retired prototype still has an entry")
		}
	}
	api := &API{account: account, initialState: state}
	for _, tower := range master.TowerProfiles {
		story := tower.Floors[0]
		if story.Story.TalkID == 0 || tower.LoseCountMax != 4 {
			t.Fatalf("tower %d lacks its story/lose contract", tower.TowerID)
		}
		w := httptest.NewRecorder()
		api.towerQuestShow(w, httptest.NewRequest(http.MethodPost, "/TowerQuestShow", strings.NewReader(`{"towerid":`+strconv.Itoa(tower.TowerID)+`}`)))
		if w.Code != 200 {
			t.Fatalf("tower %d DTO: %s", tower.TowerID, w.Body.String())
		}
		if started, err := account.BeginTowerStory(story.Story.TalkID, tower.TowerID, story.Floor); err != nil || !started {
			t.Fatalf("tower %d story start: %v", tower.TowerID, err)
		}
		if _, err := account.EndSubStory(true); err != nil {
			t.Fatalf("tower %d story clear: %v", tower.TowerID, err)
		}
		_, progress, _, err := account.TowerQuestShowState(tower.TowerID)
		if err != nil || progress.Floor != 2 || !slices.Contains(progress.ClearedFloors, 1) {
			t.Fatalf("tower %d story did not open the next floor: %+v, %v", tower.TowerID, progress, err)
		}
		var boss struct {
			ID int `json:"0"`
		}
		if err := json.Unmarshal(tower.Floors[1].Boss, &boss); err != nil {
			t.Fatal(err)
		}
		entry := game.TeamBattleEntry{BossID: boss.ID, StageType: gamestate.BattleStageAlchemist,
			EntryRole: gamestate.BattleEntrySolo, EnemyTypes: []int8{1}, Profiles: state.TeamBattleRewards,
			FameSeed: "tower-content-integration", FameSources: []game.TeamBattleFameSource{{MemberType: 1, LeaderFame: 1}}}
		for run := 0; run < 2; run++ {
			if _, _, started, err := account.BeginTeamBattle(entry); err != nil || !started {
				t.Fatalf("tower %d battle start %d: %v", tower.TowerID, run, err)
			}
			settled, err := account.CompleteTeamBattle(boss.ID, true, state.TeamBattleRewards, game.TeamBattleDropReport{Turns: 10, EnemyDeadBits: []int{15}})
			if err != nil {
				t.Fatalf("tower %d battle clear %d: %v", tower.TowerID, run, err)
			}
			if settled.WasFirstClear != (run == 0) {
				t.Fatalf("tower %d first-clear repeated or missing on run %d", tower.TowerID, run)
			}
		}
		_, progress, _, err = account.TowerQuestShowState(tower.TowerID)
		if err != nil || progress.Floor != 3 || progress.ClearRatings[2] != 7 {
			t.Fatalf("tower %d repeat/rating state: %+v, %v", tower.TowerID, progress, err)
		}
	}
	saved := account.Snapshot(state)
	foundRetired := false
	for _, p := range saved.TowerQuestProgress {
		if p.TowerID == 1 {
			foundRetired = p.Floor == 4 && slices.Equal(p.ClearedFloors, []int{1, 2, 3}) && p.ClearRatings[2] == 7
		}
	}
	if !foundRetired {
		t.Fatal("content refresh removed retired private progress")
	}
	// Use one reached checkpoint to cover the alternate, per-floor key price
	// and the boundary between closing admission and accepted settlement.
	checkpoint := master.TowerProfiles[0].Floors[0]
	for _, f := range master.TowerProfiles[0].Floors {
		if f.ItemUse > 0 {
			checkpoint = f
			break
		}
	}
	for i := range saved.TowerQuestProgress {
		if saved.TowerQuestProgress[i].TowerID == master.TowerProfiles[0].TowerID {
			saved.TowerQuestProgress[i].Floor = checkpoint.Floor
		}
	}
	account, err = game.New(saved)
	if err != nil {
		t.Fatal(err)
	}
	var boss struct {
		ID int `json:"0"`
	}
	if err := json.Unmarshal(checkpoint.Boss, &boss); err != nil {
		t.Fatal(err)
	}
	entry := game.TeamBattleEntry{BossID: boss.ID, StageType: gamestate.BattleStageAlchemist,
		EntryRole: gamestate.BattleEntrySolo, EnemyTypes: []int8{1}, Profiles: saved.TeamBattleRewards,
		FameSeed: "tower-key-integration", FameSources: []game.TeamBattleFameSource{{MemberType: 1, LeaderFame: 1}}}
	keyBalance := func(s gamestate.State) int {
		for _, item := range s.Items {
			if item.ItemID == 3022 {
				return item.Num
			}
		}
		return 0
	}
	before := keyBalance(account.Snapshot(saved))
	account.ApplyTowerAdmission(game.TowerAdmission{TowerIDs: map[int]struct{}{}})
	if len(account.TowerQuestDirectory()) != 0 {
		t.Fatal("closed towers remain visible")
	}
	if _, _, started, err := account.BeginTeamBattle(entry); err == nil || started || keyBalance(account.Snapshot(saved)) != before {
		t.Fatal("closed checkpoint accepted or charged entry", err)
	}
	account.ApplyTowerAdmission(game.TowerAdmission{})
	context, _, started, err := account.BeginTeamBattle(entry)
	if err != nil || !started || context.ItemID != 3022 || context.ItemUse != checkpoint.ItemUse ||
		keyBalance(account.Snapshot(saved)) != before-checkpoint.ItemUse {
		t.Fatal("checkpoint key price mismatch", err)
	}
	account.ApplyTowerAdmission(game.TowerAdmission{TowerIDs: map[int]struct{}{}})
	if _, err := account.CompleteTeamBattle(boss.ID, true, saved.TeamBattleRewards, game.TeamBattleDropReport{Turns: 10, EnemyDeadBits: []int{15}}); err != nil {
		t.Fatal("closure blocked an already accepted settlement", err)
	}
}

func TestSoloCatalogIncludesNamedTowerRootsAndKeepsResultForTowerShow(t *testing.T) {
	account := testAccount(t, func(s *gamestate.State) {
		s.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[],"11":[],"12":[]}`)
		s.TowerQuestProfiles = []gamestate.TowerQuestProfile{{TowerID: 7, Name: "炼金塔", LoseCountMax: 3, Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Boss: json.RawMessage(`{"9":123}`)}}}}
		s.TowerQuestProgress = []gamestate.TowerQuestProgress{{TowerID: 7, Floor: 0, LastBattleFloor: 1, LastResult: "win", LastRankUp: true}}
	})
	a := &API{account: account}
	read := func() []map[string]json.RawMessage {
		t.Helper()
		w := httptest.NewRecorder()
		a.teamBattleSoloShow(w, httptest.NewRequest(http.MethodPost, "/TeamBattleSoloShow", strings.NewReader(`{"active_arthur_type":5}`)))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		if w.Code != 200 || len(lines) != 3 {
			t.Fatal(w.Body.String())
		}
		var method struct {
			Groups []map[string]json.RawMessage `json:"alchemist_groups"`
		}
		if err := json.Unmarshal([]byte(lines[1]), &method); err != nil {
			t.Fatal(err)
		}
		if method.Groups == nil {
			t.Fatal("named directory missing")
		}
		return method.Groups
	}
	groups := read()
	if len(groups) != 1 {
		t.Fatal(groups)
	}
	root := groups[0]
	for k, want := range map[string]string{"boss_groupid": "7", "stage_type": "19", "pictid": "123", "tower_quest_state": "[2]", "bosses": "[]", "stories": "[]"} {
		if string(root[k]) != want {
			t.Fatalf("%s: %s", k, root[k])
		}
	}
	read()
	_, progress, consumed, err := account.TowerQuestShowState(7)
	if err != nil || !consumed || progress.LastResult != "win" || !progress.LastRankUp {
		t.Fatal("directory ate tower result", progress, consumed, err)
	}
	account.ApplyTowerAdmission(game.TowerAdmission{TowerIDs: map[int]struct{}{}})
	if len(read()) != 0 {
		t.Fatal("closed tower remained visible")
	}
}

// The original client builds its lower-rank replay list by excluding the
// current challenge rank. Future ranks must not be included in that projection.
func TestTowerShowReachedRankProjection(t *testing.T) {
	for _, tc := range []struct {
		name         string
		floor        int
		ranks        []int
		replayFloors []int
	}{
		{"first floor", 1, []int{1}, nil},
		{"last shallow floor", 5, []int{1}, nil},
		{"first deep floor", 6, []int{1, 2}, []int{5, 4, 3, 2, 1}},
		{"all clear", 0, []int{1, 2}, []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := testAccount(t, func(s *gamestate.State) {
				floors := make([]gamestate.TowerQuestFloorProfile, 10)
				for i := range floors {
					floors[i] = gamestate.TowerQuestFloorProfile{Floor: i + 1, Rank: i/5 + 1, Boss: json.RawMessage(`{"9":123}`)}
				}
				s.TowerQuestProfiles = []gamestate.TowerQuestProfile{{TowerID: 7, Name: "test", LoseCountMax: 3,
					Ranks: []gamestate.TowerQuestRankProfile{{Rank: 1, RankName: "shallow"}, {Rank: 2, RankName: "deep"}}, Floors: floors}}
				s.TowerQuestProgress = []gamestate.TowerQuestProgress{{TowerID: 7, Floor: tc.floor}}
			})
			a := &API{account: account}
			w := httptest.NewRecorder()
			a.towerQuestShow(w, httptest.NewRequest(http.MethodPost, "/TowerQuestShow", strings.NewReader(`{"towerid":7}`)))
			lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
			if w.Code != 200 || len(lines) != 3 {
				t.Fatal(w.Code, w.Body.String())
			}
			var response struct {
				Ranks []struct {
					Rank int `json:"rank"`
				} `json:"ranks"`
				Floors []struct {
					Rank  int `json:"rank"`
					Floor int `json:"floor"`
				} `json:"floors"`
			}
			if err := json.Unmarshal([]byte(lines[1]), &response); err != nil {
				t.Fatal(err)
			}
			ranks := make([]int, 0, len(response.Ranks))
			for _, rank := range response.Ranks {
				ranks = append(ranks, rank.Rank)
			}
			if !slices.Equal(ranks, tc.ranks) {
				t.Fatalf("ranks=%v want %v", ranks, tc.ranks)
			}
			if len(response.Floors) != 10 {
				t.Fatalf("full tower definition lost: %d", len(response.Floors))
			}
			currentRank := 0
			for _, floor := range response.Floors {
				if floor.Floor == tc.floor {
					currentRank = floor.Rank
				}
			}
			// Match GetListPastRank + GetRankQuest's descending rank/floor order.
			var replay []int
			for r := len(response.Ranks) - 1; r >= 0; r-- {
				rank := response.Ranks[r].Rank
				if rank == currentRank {
					continue
				}
				for f := len(response.Floors) - 1; f >= 0; f-- {
					if response.Floors[f].Rank == rank {
						replay = append(replay, response.Floors[f].Floor)
					}
				}
			}
			if !slices.Equal(replay, tc.replayFloors) {
				t.Fatalf("lower-rank replay floors=%v want %v", replay, tc.replayFloors)
			}
		})
	}
}
