package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func TestServingRestartInvalidatesSessionsNotAccounts(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	const uuid = "00000000-0000-0000-0000-000000000001"
	identity, err := accounts.ResolveInternalLogin(uuid)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := accounts.BindInternalAccount(uuid, "restart_test", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	document, err := accounts.Database().WriteDocument("test/restart-policy", 0, map[string]int{"reward": 37}, "test")
	if err != nil {
		t.Fatal(err)
	}
	before, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := accounts.ResolveSession(identity.SessionKey); err != nil || id != identity.UserID {
		t.Fatalf("initial session: %d %v", id, err)
	}
	deployment := &cnDeploymentHandler{accounts: accounts}
	if id, err := accounts.ResolveSession(identity.SessionKey); err != nil || id != identity.UserID {
		t.Fatal("assembly revoked session", err)
	}
	if err := deployment.Activate(); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.ResolveSession(identity.SessionKey); !errors.Is(err, accountstore.ErrInvalidSession) {
		t.Fatalf("old session survived restart: %v", err)
	}
	after, err := accounts.LoadState(identity.UserID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("restart changed account save: %v", err)
	}
	retainedBinding, err := accounts.LoginAccount("restart_test", "test-password")
	if err != nil || retainedBinding.UserID != binding.UserID {
		t.Fatalf("account binding changed: %v", err)
	}
	retainedDocument, err := accounts.Database().ReadDocument("test/restart-policy")
	if err != nil || !reflect.DeepEqual(document, retainedDocument) {
		t.Fatalf("operator configuration changed: %v", err)
	}
	loggedIn, err := accounts.ResolveInternalLogin(uuid)
	if err != nil || loggedIn.UserID != identity.UserID || loggedIn.SessionKey == identity.SessionKey {
		t.Fatalf("relogin did not retain identity and rotate session: %v", err)
	}
	if id, err := accounts.ResolveSession(loggedIn.SessionKey); err != nil || id != identity.UserID {
		t.Fatalf("new session invalid: %d %v", id, err)
	}
}

func TestActiveBattleUsesRuntimeMemoryAcrossAccountReload(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	state, err := accounts.LoadState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	// First normal quest: its final body drops gold and a stack material.
	state.Navigation = gamestate.NavigationState{MainStoryID: 1001, MainStoryCN: true, StageAreaID: 100001}
	state.BurstProgress = [4]uint8{2}
	state.StoryTeamBattleSession = gamestate.StoryTeamBattleSession{StoryID: 45001020}
	state.ActiveTeamBattle = &gamestate.TeamBattleActiveState{
		ClearDecks: []gamestate.BattleClearDeck{{UserID: accountstore.PrimaryUserID, ArthurType: 1, Name: "frozen", HonorIDs: []int{0, 0, 0, 0}, Deck: []byte(`{"name":"frozen deck"}`)}},
		Seed:       478,
		BossID:     10000101, BattleEnemyTypes: []int8{1, 1, 1, 1},
		StageQuestAreaID: 100001, StageQuestStageID: 10000101,
		BPUse: 7, EntryRole: gamestate.BattleEntrySolo, FameSeed: "normal-quest-start",
		FameSources: []gamestate.TeamBattleFameSourceState{{MemberType: 1, LeaderFame: 100}},
		DropPlanSet: true,
		DropPlan: []gamestate.TeamBattleEnemyDrop{
			{BattleIndex: 3, Reward: gamestate.Reward{Type: 4, Num: 100, CardSkillLevels: []int16{}}},
			{BattleIndex: 3, Reward: gamestate.Reward{Type: 13, Num: 1, RewardTypeID: 20000001, CardSkillLevels: []int16{}}},
		},
	}
	if err := accounts.PersistState(accountstore.PrimaryUserID, state); err != nil {
		t.Fatalf("persist normal quest start: %v", err)
	}
	reloaded, err := accounts.LoadState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ActiveTeamBattle == nil || len(reloaded.ActiveTeamBattle.DropPlan) != 2 || reloaded.ActiveTeamBattle.Seed != 478 {
		t.Fatal("active battle drop plan was lost")
	}
	if !reflect.DeepEqual(reloaded.ActiveTeamBattle.ClearDecks, state.ActiveTeamBattle.ClearDecks) {
		t.Fatal("frozen clear deck was lost on handler reload")
	}
	if reloaded.Navigation != state.Navigation {
		t.Fatal("selected story/dungeon context was lost on account reload")
	}
	if reloaded.StoryTeamBattleSession.StoryID != 45001020 {
		t.Fatal("learning battle was lost on handler reload")
	}
	for _, drop := range reloaded.ActiveTeamBattle.DropPlan {
		if drop.Reward.CardSkillLevels == nil {
			t.Fatal("empty reward skill array became null on reload")
		}
	}
	if err := accounts.PersistState(accountstore.PrimaryUserID, reloaded); err != nil {
		t.Fatalf("persist resumed battle: %v", err)
	}
	encoded, err := accountstore.EncodeAccountMetadata(reloaded)
	if err != nil || bytes.Contains(encoded, []byte(`"active_team_battle"`)) {
		t.Fatal("ongoing battle was written to the durable account", err)
	}
	// A new repository instance is a new server lifetime, even with the same DB.
	restarted := testfixture.TestAccountRepository(t, accounts.Database())
	withoutBattle, err := restarted.LoadState(accountstore.PrimaryUserID)
	if err != nil || withoutBattle.ActiveTeamBattle != nil || withoutBattle.Navigation != state.Navigation {
		t.Fatal("restart restored a battle lock or lost persistent account data", err)
	}
	if withoutBattle.StoryTeamBattleSession.StoryID != 0 || withoutBattle.BurstProgress != state.BurstProgress {
		t.Fatal("learning battle survived restart or completed chapters were lost")
	}
	accounts.RememberBattle(accountstore.PrimaryUserID, nil, gamestate.StoryTeamBattleSession{})
	// Response-only receipts are disposable. Strict JSON seeds and SQLite must
	// accept them without replaying the obsolete DTO or losing the settled key.
	content, err := json.Marshal(accountstore.SaveFromState(withoutBattle))
	if err != nil {
		t.Fatal(err)
	}
	var seed map[string]json.RawMessage
	if err := json.Unmarshal(content, &seed); err != nil {
		t.Fatal(err)
	}
	seed["team_battle_result_receipts"] = json.RawMessage(`[{"room_id":42,"claimed_at_unix":1,"response":{"members":[]}}]`)
	seed["team_battle_solo_result_receipts"] = json.RawMessage(`[{"request_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","boss_id":10000101,"claimed_at_unix":1,"response":{"partners":[]}}]`)
	content, err = json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := accountstore.DecodeSaveState(content)
	if err != nil {
		t.Fatal("retired receipt blocked strict save loading", err)
	}
	if err := restarted.PersistState(accountstore.PrimaryUserID, retired); err != nil {
		t.Fatal(err)
	}
	loaded, err := testfixture.TestAccountRepository(t, accounts.Database()).LoadState(accountstore.PrimaryUserID)
	if err != nil || len(loaded.TeamBattleResultReceipts) != 1 || len(loaded.TeamBattleSoloResultReceipts) != 1 ||
		len(loaded.TeamBattleResultReceipts[0].Snapshot) != 0 || len(loaded.TeamBattleSoloResultReceipts[0].Snapshot) != 0 {
		t.Fatal("retired settlement identity was lost or old payload retained", err)
	}
	encoded, err = accountstore.EncodeAccountMetadata(loaded)
	if err != nil || bytes.Contains(encoded, []byte(`"response"`)) {
		t.Fatal("retired client replies were written back", err)
	}
}

func requireFollowAddError(t *testing.T, err error, resultCode int) {
	t.Helper()
	var followErr *game.FollowAddError
	if !errors.As(err, &followErr) || followErr.ResultCode != resultCode || followErr.ResultString == "" {
		t.Fatalf("follow error = %v, want result code %d", err, resultCode)
	}
}

func TestFollowCapacityPreservesOriginalClientResultCodes(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	requester := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x101)
	targetA := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x102)
	targetB := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x103)
	targetC := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x104)
	if _, err := accounts.FollowFriendPointAccounts(requester, []int{targetA, targetB}, 2); err != nil {
		t.Fatal(err)
	}
	_, err := accounts.FollowFriendPointAccounts(requester, []int{targetC}, 2)
	requireFollowAddError(t, err, -3410)
	// A full outgoing list still accepts new followers and mutual relationships.
	for _, target := range []int{targetA, targetB} {
		result, err := accounts.FollowFriendPointAccounts(target, []int{requester}, 1)
		if err != nil || !result.IsFriendFull {
			t.Fatalf("mutual follow at sole follow ceiling: %+v %v", result, err)
		}
	}
	for _, suffix := range []int{0x105, 0x106, 0x107} {
		follower := testfixture.CreateNamedFriendCapacityAccount(t, accounts, suffix)
		if _, err := accounts.FollowFriendPointAccounts(follower, []int{requester}, 1); err != nil {
			t.Fatalf("full target rejected a follower: %v", err)
		}
	}
	followed, err := accounts.ListFollowedFriendPointAccounts(requester)
	if err != nil || len(followed) != 2 || followed[0].FriendState != game.FriendStateFriend || followed[1].FriendState != game.FriendStateFriend {
		t.Fatalf("mutuals consume existing follows: %+v %v", followed, err)
	}
	followers, total, err := accounts.ListFollowerFriendPointAccounts(requester, 200)
	if err != nil || total != 5 || len(followers) != 5 {
		t.Fatalf("follower capacity was independently restricted: %d %d %v", len(followers), total, err)
	}
}

func TestFriendPointAccountStatesReadsTargetedRelationshipStates(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	requester := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x201)
	outbound := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x202)
	inbound := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x203)
	mutual := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x204)
	unrelated := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x205)
	if _, err := accounts.FollowFriendPointAccounts(requester, []int{outbound, mutual}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FollowFriendPointAccounts(inbound, []int{requester}, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FollowFriendPointAccounts(mutual, []int{requester}, 10); err != nil {
		t.Fatal(err)
	}

	states, err := accounts.FriendPointAccountStates(
		requester,
		[]int{outbound, inbound, mutual, unrelated, outbound},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]int8{outbound: 5, inbound: 6, mutual: 3, unrelated: 0}
	if !maps.Equal(states, want) {
		t.Fatalf("targeted relationship states = %v, want %v", states, want)
	}
}

func TestPartnerDiscoveryReadsProjectionInsteadOfFullSnapshots(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	requester := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x301)
	target := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x302)
	database, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`UPDATE cn_account_snapshot SET payload_json = ? WHERE user_id = ?`,
		[]byte("not-json"),
		target,
	); err != nil {
		t.Fatal(err)
	}

	relations, err := accounts.LoadFriendPointAccountRelations(requester, []int{target})
	if err != nil {
		t.Fatalf("partner discovery decoded a full account snapshot: %v", err)
	}
	found := false
	for _, relation := range relations {
		if relation.State.User.UserID == target {
			found = relation.State.User.Name == "FriendEdge770"
			break
		}
	}
	if !found {
		t.Fatalf("target %d was absent from account projections", target)
	}
}

func TestAccountProjectionBoundsInventoryAndKeepsDeckReferences(t *testing.T) {
	state := gamestate.State{User: gamestate.User{UserID: accountstore.PrimaryUserID},
		Cards:       make([]gamestate.Card, 10000),
		Decks:       []gamestate.Deck{{CardUniqueIDs: []int64{1}, SupportCardUniqueIDs: []int64{2}}},
		PVP:         gamestate.PVPPlayerState{DefenseDecks: []gamestate.PVPDeckSelection{{CardUniqueIDs: []int64{10000}}}},
		SupportDeck: gamestate.SupportDeckState{CardCollectionIDs: []int{101}, CardCollectionLoveMaxIDs: []int{101}, UnlockSlotNums: []int8{1, 0, 0, 0}},
	}
	for i := range state.Cards {
		state.Cards[i] = gamestate.Card{UniqueID: int64(i + 1), CardID: 101 + i, Level: 50, HP: 1234, Love: 5000, LoveMax: 10000, SkillLevels: []int16{3, 4}}
	}
	content, digest, err := accountstore.EncodeAccountProjection(state)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := accountstore.DecodeAccountProjection(content, digest)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) > 16*1024 || len(loaded.Cards) != 3 || loaded.Cards[2].UniqueID != 10000 || loaded.Cards[1].LoveMax != 10000 || loaded.Cards[0].HP != 1234 || !slices.Equal(loaded.Cards[0].SkillLevels, []int16{3, 4}) || len(loaded.SupportDeck.CardCollectionIDs) != 0 || len(loaded.SupportDeck.CardCollectionLoveMaxIDs) != 0 || loaded.SupportDeck.UnlockSlotNums[0] != 1 {
		t.Fatalf("public projection lost deck data or retained inventory/history: %d bytes, %+v", len(content), loaded.Cards)
	}
	t.Logf("10,000 inventory cards: public projection %d bytes, %d referenced cards", len(content), len(loaded.Cards))
}

func TestSystemPartnerAccountsArePersistentFriendTargetsAndDoNotConsumePlayerIDs(t *testing.T) {
	seedPath := filepath.Join("..", "..", "config", "cn602-save-template.json")
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	temporary := t.TempDir()
	jsonPath := filepath.Join(temporary, "save.json")
	if err := os.WriteFile(jsonPath, seed, 0o600); err != nil {
		t.Fatalf("write temporary primary seed: %v", err)
	}
	storage, err := accountstore.OpenDatabase(
		jsonPath,
		seedPath,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := storage.LoadOrImport(); err != nil {
		t.Fatalf("import primary account: %v", err)
	}
	seedState, err := accountstore.LoadSaveState(seedPath)
	if err != nil {
		t.Fatalf("load system-partner seed: %v", err)
	}
	expectedPopulationCards, expectedPopulationDecks, _, err := accountstore.SystemPartnerPopulationFromSeed(seedState)
	if err != nil {
		t.Fatalf("load expected system-partner population: %v", err)
	}
	accounts, err := accountstore.NewAccounts(storage)
	if err != nil {
		t.Fatalf("initialize account store: %v", err)
	}

	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		_, expectedDeck, expectedLeader, err := accountstore.SystemPartnerLoadoutFromSeed(seedState, arthurType)
		if err != nil {
			t.Fatalf("load expected system partner %d: %v", arthurType, err)
		}
		userID := accountstore.SystemPartnerUserID(arthurType)
		state, err := accounts.LoadState(userID)
		if err != nil {
			t.Fatalf("load system partner %d: %v", arthurType, err)
		}
		if state.User.Name != accountstore.SystemPartnerNameByArthur[arthurType] ||
			state.User.ActiveArthurType != int(arthurType) || len(state.Cards) != len(expectedPopulationCards) {
			t.Fatalf("system partner %d identity = %+v, cards = %d", arthurType, state.User, len(state.Cards))
		}
		actualCardIDs := make([]int, len(state.Cards))
		expectedCardIDs := make([]int, len(expectedPopulationCards))
		for index, card := range state.Cards {
			actualCardIDs[index] = card.CardID
			expectedCardIDs[index] = expectedPopulationCards[index].CardID
		}
		var activeDeck *gamestate.Deck
		for index := range state.Decks {
			if state.Decks[index].ArthurType == arthurType && state.Decks[index].Index == 0 {
				activeDeck = &state.Decks[index]
				break
			}
		}
		if !slices.Equal(actualCardIDs, expectedCardIDs) || len(state.Decks) != len(expectedPopulationDecks) ||
			activeDeck == nil || !slices.Equal(activeDeck.CardUniqueIDs, expectedDeck.CardUniqueIDs) {
			t.Fatalf(
				"system partner %d loadout = cards %v decks %+v, want cards %v deck %+v",
				arthurType,
				actualCardIDs,
				state.Decks,
				expectedCardIDs,
				expectedDeck,
			)
		}
		if arthurType == 4 {
			cardIDByUniqueID := make(map[int64]int, len(state.Cards))
			for _, card := range state.Cards {
				cardIDByUniqueID[card.UniqueID] = card.CardID
			}
			activeCardIDs := make([]int, len(activeDeck.CardUniqueIDs))
			for index, uniqueID := range activeDeck.CardUniqueIDs {
				activeCardIDs[index] = cardIDByUniqueID[uniqueID]
			}
			if slices.Contains(activeCardIDs, 10000030) || activeCardIDs[5] != 10000034 {
				t.Fatalf(
					"singer system-partner deck retained the mixed starter attack: %v",
					activeCardIDs,
				)
			}
		}
		if state.User.LeaderCardID != expectedLeader.CardID ||
			state.User.LeaderCardUniqueID != expectedLeader.UniqueID ||
			state.Onboarding.Step != masterdata.OnboardingStepCount {
			t.Fatalf(
				"system partner %d leader/tutorial = (%d, %d, %+v)",
				arthurType,
				state.User.LeaderCardID,
				state.User.LeaderCardUniqueID,
				state.Onboarding,
			)
		}
	}

	primaryIdentity, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if primaryIdentity.UserID != accountstore.PrimaryUserID {
		t.Fatalf("primary player user ID = %d, want %d", primaryIdentity.UserID, accountstore.PrimaryUserID)
	}
	identity, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	if identity.UserID != accountstore.PrimaryUserID+1 {
		t.Fatalf("first new player user ID = %d, want %d", identity.UserID, accountstore.PrimaryUserID+1)
	}
	pvpOpponents, err := accounts.ListPVPOpponents(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pvpOpponents) != 1 || pvpOpponents[0].User.UserID != identity.UserID {
		t.Fatalf("PVP opponents included system partners: %+v", pvpOpponents)
	}

	targetID := accountstore.SystemPartnerUserID(2)
	added, err := accounts.FollowFriendPointAccounts(accountstore.PrimaryUserID, []int{targetID}, 100)
	if err != nil {
		t.Fatalf("follow system partner: %v", err)
	}
	if !slices.Equal(added.RequestUserIDs, []int{targetID}) || added.IsFriendFull {
		t.Fatalf("follow response = %v", added)
	}
	relations, err := accounts.ListFollowedFriendPointAccounts(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, relation := range relations {
		if relation.State.User.UserID == targetID {
			found = relation.System && relation.FriendState == 5
			break
		}
	}
	if !found {
		t.Fatal("followed system partner was not returned as a persistent follow relation")
	}
}

func TestPlayerAccountLoadsSharedItemShopWithoutResettingOwnedItems(t *testing.T) {
	seedPath := filepath.Join("..", "..", "config", "cn602-save-template.json")
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	temporary := t.TempDir()
	jsonPath := filepath.Join(temporary, "save.json")
	if err := os.WriteFile(jsonPath, seed, 0o600); err != nil {
		t.Fatalf("write temporary primary seed: %v", err)
	}
	storage, err := accountstore.OpenDatabase(
		jsonPath,
		seedPath,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := storage.LoadOrImport(); err != nil {
		t.Fatalf("import primary account: %v", err)
	}
	accounts, err := accountstore.NewAccounts(storage)
	if err != nil {
		t.Fatalf("initialize account store: %v", err)
	}
	identity, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-000000000099")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Items = append(state.Items, gamestate.Item{ItemID: 9010, Num: 7})
	if err := accounts.PersistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}

	reloaded, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	hasQuickBuy := false
	if len(reloaded.ItemShopTabs) == 5 {
		for _, lineup := range reloaded.ItemShopTabs[0].Lineup {
			if lineup.LineupID == 992001 && lineup.Hidden {
				hasQuickBuy = true
				break
			}
		}
	}

	if !hasQuickBuy {
		t.Fatal("shared item shop lost its quick-buy entry")
	}
	for _, item := range reloaded.Items {
		if item.ItemID == 9010 {
			if item.Num != 7 {
				t.Fatalf("water-crystal gacha coin balance = %d, want 7", item.Num)
			}
			return
		}
	}
	t.Fatal("water-crystal gacha coin is absent after reload")
}

func TestAccountSnapshotExcludesDerivedFriendProfiles(t *testing.T) {
	started := time.Now().Unix()
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	primary, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if primary.UserID != accountstore.PrimaryUserID {
		t.Fatalf("primary user ID = %d, want %d", primary.UserID, accountstore.PrimaryUserID)
	}
	identity, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-000000000199")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Friends.Users = []gamestate.Friend{{UserID: 1000002, Name: "legacy fake"}}
	if state.User.Comment != "请多关照！" || len(state.User.InviteID) != 9 {
		t.Fatal("new profile contains local diagnostic placeholders")
	}
	if _, err := strconv.Atoi(state.User.InviteID); err != nil {
		t.Fatal("friend-search ID is not numeric", err)
	}
	state.User.Comment = "一起打冰龙吧"
	if err := accounts.PersistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}

	migrated, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.Friends.Users == nil || len(migrated.Friends.Users) != 0 {
		t.Fatalf("migrated friend rows = %#v, want non-nil empty list", migrated.Friends.Users)
	}
	if migrated.User.Comment != state.User.Comment {
		t.Fatal("saved custom profile comment was replaced")
	}
	projections, err := accounts.ListLocalAccountStates(primary.UserID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, projection := range projections {
		if projection.LastLoginUnix < started || projection.LastLoginUnix > time.Now().Unix() || len(projection.User.InviteID) != 9 {
			t.Fatal("friend projection has invalid login time or search ID", projection.User.UserID)
		}
	}

	database, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	var content []byte
	if err := database.QueryRow(
		`SELECT payload_json FROM cn_account_snapshot WHERE user_id = ?`,
		identity.UserID,
	).Scan(&content); err != nil {
		t.Fatal(err)
	}
	persisted, err := accountstore.DecodeAccountSnapshot(content)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Friends.Users == nil || len(persisted.Friends.Users) != 0 {
		t.Fatalf("persisted friend rows = %#v, want non-nil empty list", persisted.Friends.Users)
	}
}
