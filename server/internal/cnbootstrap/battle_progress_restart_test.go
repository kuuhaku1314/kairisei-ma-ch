package cnbootstrap

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestMultiplayerCompletionCommitsInventoryBeforeResultQuery(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	_ = testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x308)
	other := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x309)
	users := []int{accountstore.PrimaryUserID, other}
	const boss = 30010102
	runtime := testfixture.RuntimeState(t)
	helper := &runtime.PlayerProgressionPolicy.Friends.HelperReward
	helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
	runtime.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"10":0}]}],"11":[],"12":[]}`)
	runtime.TeamBattleReplays = []gamestate.TeamBattleReplay{{BossID: boss, EnemyPartyID: 1, HoldMax: 5}}
	runtime.TeamBattleRewards = []gamestate.TeamBattleRewardProfile{{BossID: boss, ResultRewards: []gamestate.Reward{{Type: 4, Num: 10, CardSkillLevels: []int16{}}}}}
	accounts.Database().SetCatalog(runtime)
	for _, id := range users {
		state := runtime
		state.User.UserID, state.User.Gold, state.User.BP = id, 100, 0
		state.BattlePoint.NextRecoveryUnix = time.Now().Unix() + 180
		state.TeamBattleStartReceipts = []gamestate.TeamBattleStartReceipt{{RoomID: 123, BossID: boss, BPUse: 15}}
		if err := accounts.PersistState(id, state); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	completed := multiplayer.CompletedBattle{RoomID: 123, BossID: boss, EndType: 1, OwnerMemberType: 1, HostCostPaid: true,
		CompletedAtUnix: now.Unix(), OnlineUserIDs: users}
	completed.SettlementPlan = gamestate.TeamBattleSettlementPlan{Profile: runtime.TeamBattleRewards[0], EnemyTypes: []int8{0}, FameBonus: runtime.TeamBattleFameBonusPolicy, HostBonus: runtime.TeamBattleHostBonusPolicy}
	for i := 1; i <= 4; i++ {
		id := 1900000000 + i
		if i <= 2 {
			id = users[i-1]
		}
		completed.Members = append(completed.Members, multiplayer.Member{MemberType: i, ArthurType: 3, UserID: id, LeaderFame: 1, DeckHonorIDs: make([]int, 4)})
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	newRouter := func(repository *accountstore.Accounts) *accounthttp.Router {
		hub := multiplayer.NewHub()
		if err := hub.AttachCompletionRepository(repository); err != nil {
			t.Fatal(err)
		}
		return accounthttp.New(accounthttp.Config{IdleLimit: 4, PersistCompleted: repository.PersistCompletedBattle,
			Build: func(id int) (http.Handler, error) {
				state, err := repository.LoadState(id)
				if err != nil {
					return nil, err
				}
				return httpapi.New(httpapi.Config{InitialState: state, BaseURL: "http://127.0.0.1", Logger: logger, Multiplayer: hub,
					PersistState: func(state gamestate.State) error { return repository.PersistState(id, state) }})
			}})
	}
	router := newRouter(accounts)
	query := func(router *accounthttp.Router, id int) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/TeamBattleResult", strings.NewReader(`{"roomid":123}`))
		r.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(id))
		router.ServeHTTP(w, r)
		return w
	}
	// A result query cannot create rewards when no terminal commit exists.
	if w := query(router, users[0]); w.Code != 200 || !strings.Contains(w.Body.String(), `"res_code":-3207`) {
		t.Fatal(w.Body.String())
	}
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TRIGGER fail_completion_inventory BEFORE UPDATE ON cn_account_snapshot BEGIN SELECT RAISE(ABORT,'forced'); END`); err != nil {
		t.Fatal(err)
	}
	if err = router.CompleteMultiplayerBattle(completed, now.Add(time.Hour)); err == nil {
		t.Fatal("failed account write committed completion")
	}
	for _, id := range users {
		s, err := accounts.LoadPersistentState(id)
		if err != nil || s.User.Gold != 100 || len(s.TeamBattleResultReceipts) != 0 {
			t.Fatal("partial inventory/result commit", id, err)
		}
	}
	if _, _, err := accounts.LoadCompleted(123, time.Now()); !errors.Is(err, multiplayer.ErrCompletedBattleUnavailable) {
		t.Fatal("partial room completion commit", err)
	}
	if _, err = db.Exec(`DROP TRIGGER fail_completion_inventory`); err != nil {
		t.Fatal(err)
	}
	if err = router.CompleteMultiplayerBattle(completed, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, id := range users {
		s, err := accounts.LoadPersistentState(id)
		if err != nil || s.User.Gold != 110 || len(s.TeamBattleResultReceipts) != 1 {
			t.Fatal("inventory still waits for HTTP result", id, s.User.Gold, err)
		}
	}
	if err = router.CompleteMultiplayerBattle(completed, now.Add(time.Hour)); err != nil {
		t.Fatal("terminal retry", err)
	}
	fresh := testfixture.TestAccountRepository(t, accounts.Database())
	restarted := newRouter(fresh)
	for _, id := range users {
		for _, r := range []*accounthttp.Router{router, restarted} {
			w := query(r, id)
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"res_code":0`) {
				t.Fatal("result recovery", w.Body.String())
			}
		}
		s, err := fresh.LoadPersistentState(id)
		if err != nil || s.User.Gold != 110 || len(s.TeamBattleResultReceipts) != 1 {
			t.Fatal("result query granted again", id, err)
		}
	}
}

// Reload real SQLite snapshots against an unmodified catalog, using new
// repository instances as after eviction/restart, for both account tables.
func TestBattleProgressSurvivesAccountAndServerReload(t *testing.T) {
	for _, category := range []string{"9", "10", "11", "12"} {
		t.Run(category, func(t *testing.T) {
			accounts := testfixture.NewFriendCapacityTestAccounts(t)
			catalog, err := accounts.Database().CatalogState()
			if err != nil {
				t.Fatal(err)
			}
			var top map[string]json.RawMessage
			if err := json.Unmarshal(catalog.TeamBattleSolo, &top); err != nil {
				t.Fatal(err)
			}
			var groups []json.RawMessage
			if err := json.Unmarshal(top["9"], &groups); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"9", "10", "11", "12"} {
				top[key] = json.RawMessage("[]")
			}
			top[category], _ = json.Marshal(groups[:1])
			catalog.TeamBattleSolo, _ = json.Marshal(top)
			accounts.Database().SetCatalog(catalog)
			testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x490)
			other := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x491)
			for _, userID := range []int{accountstore.PrimaryUserID, other} {
				state, err := accounts.LoadPersistentState(userID)
				if err != nil {
					t.Fatal(err)
				}
				var data map[string]json.RawMessage
				_ = json.Unmarshal(state.TeamBattleSolo, &data)
				var entries []map[string]json.RawMessage
				_ = json.Unmarshal(data[category], &entries)
				var bosses []map[string]json.RawMessage
				_ = json.Unmarshal(entries[0]["10"], &bosses)
				if string(bosses[0]["10"]) != "0" {
					t.Fatal("another account modified the shared catalog")
				}
				bosses[0]["10"] = json.RawMessage("2")
				until := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
				bosses[0]["16"] = json.RawMessage(until)
				entries[0]["10"], _ = json.Marshal(bosses)
				data[category], _ = json.Marshal(entries)
				state.TeamBattleSolo, _ = json.Marshal(data)
				state.User.CoinFree += 50
				for reload := 0; reload < 2; reload++ {
					if err := accounts.PersistState(userID, state); err != nil {
						t.Fatal(err)
					}
					storage := *accounts.Database()
					fresh := testfixture.TestAccountRepository(t, &storage)
					reloaded, err := fresh.LoadPersistentState(userID)
					if err != nil {
						t.Fatal(err)
					}
					configuration, err := game.NewContentConfiguration(1, catalog)
					if err != nil {
						t.Fatal(err)
					}
					reloaded = game.ApplyContentState(reloaded, configuration)
					_ = json.Unmarshal(reloaded.TeamBattleSolo, &data)
					_ = json.Unmarshal(data[category], &entries)
					_ = json.Unmarshal(entries[0]["10"], &bosses)
					if string(bosses[0]["10"]) != "2" || string(bosses[0]["16"]) != until || reloaded.User.CoinFree != state.User.CoinFree {
						t.Fatalf("user %d category %s reload %d lost clear/wallet: %s / %d", userID, category, reload, bosses[0]["10"], reloaded.User.CoinFree)
					}
					state = reloaded
				}
			}
		})
	}
}

func TestCollectionUnlocksSurviveAccountReload(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	state, err := accounts.LoadPersistentState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Costume = json.RawMessage(`{"costumeids":[1,2]}`)
	state.InventorySequence = gamestate.InventorySequenceState{Card: 900001, Sphere: 900002, Buddy: 900003}
	state.Stamps.StampIDs = append(state.Stamps.StampIDs, 900001)
	state.Honors.HonorIDs = append(state.Honors.HonorIDs, 900002)
	// Existing schema stores an ID list independently of the legacy 63-bit mask.
	state.User.SelectableNaviIDs = append(state.User.SelectableNaviIDs, 63, 64, 70)
	state.User.NaviID = 70
	if err = accounts.PersistState(accountstore.PrimaryUserID, state); err != nil {
		t.Fatal(err)
	}
	fresh := testfixture.TestAccountRepository(t, accounts.Database())
	reloaded, err := fresh.LoadPersistentState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	if string(reloaded.Costume) != string(state.Costume) || len(reloaded.Stamps.StampIDs) != len(state.Stamps.StampIDs) || len(reloaded.Honors.HonorIDs) != len(state.Honors.HonorIDs) {
		t.Fatal("collection ownership lost on reload")
	}
	if reloaded.InventorySequence != state.InventorySequence {
		t.Fatal("instance sequence lost on reload")
	}
	if reloaded.User.NaviID != 70 || reloaded.User.NaviUnlockFlag != state.User.NaviUnlockFlag ||
		!slices.Equal(reloaded.User.SelectableNaviIDs, state.User.SelectableNaviIDs) {
		t.Fatal("expanded navigator ownership lost in existing SQLite format")
	}
}
