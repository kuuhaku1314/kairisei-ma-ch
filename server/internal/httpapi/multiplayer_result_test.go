package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestMultiplayerResultUsesEveryWaveAndReplaysReceipt(t *testing.T) {
	const boss = 30010102
	completed := multiplayer.CompletedBattle{RoomID: 123, BossID: boss, OwnerMemberType: 1,
		BattleIndex: 5, Progress: 5, OnlineUserIDs: []int{1001}, CompletedAtUnix: time.Now().Unix(), HostCostPaid: true}
	for i := 1; i <= 4; i++ {
		completed.Members = append(completed.Members, multiplayer.Member{MemberType: i, ArthurType: i, UserID: 1000 + i,
			LeaderFame: 1, DeckHonorIDs: make([]int, 4)})
	}
	replay := gamestate.TeamBattleReplay{BossID: boss, EnemyPartyID: 1}
	profile := gamestate.TeamBattleRewardProfile{BossID: boss}
	for i := 0; i < 6; i++ {
		replay.Battles = append(replay.Battles, gamestate.TeamBattleReplayBattle{EnemyPartyID: i + 1})
		drop := gamestate.TeamBattleEnemyDrop{BattleIndex: i, Reward: gamestate.Reward{Type: 4, Num: i + 1}}
		profile.EnemyDrops = append(profile.EnemyDrops, drop)
		completed.ReleasedDrops = append(completed.ReleasedDrops, drop)
	}
	completed.SettlementPlan = gamestate.TeamBattleSettlementPlan{Profile: profile, EnemyTypes: make([]int8, 6)}
	hub := multiplayer.NewHub()
	if err := hub.AttachCompletionRepository(resultCompletionRepository{completed: completed}); err != nil {
		t.Fatal(err)
	}
	s := testAccount(t, func(state *gamestate.State) {
		state.User.Gold, state.User.BP, state.User.BPMax = 0, 100, 100
		state.BattlePoint.RecoverySeconds = 180
		state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"10":0,"half_bp":8}]}],"11":[],"12":[]}`)
		state.TeamBattleStartReceipts = []gamestate.TeamBattleStartReceipt{{RoomID: 123, BossID: boss, BPUse: 15}}
		helper := &state.PlayerProgressionPolicy.Friends.HelperReward
		helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
	})
	runtime := gamestate.State{TeamBattleReplays: []gamestate.TeamBattleReplay{replay}, TeamBattleRewards: []gamestate.TeamBattleRewardProfile{profile}}
	runtime.User.UserID = 1001
	writes := 0
	var saved gamestate.State
	a := &API{initialState: runtime, account: s, multiplayer: hub, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		persistState: func(state gamestate.State) error { saved = state; writes++; return nil }}
	// A separate accepted solo run and a later publication must not change
	// the already-paid multiplayer outcome or consume that solo context.
	_, _, started, err := s.BeginTeamBattle(game.TeamBattleEntry{BossID: boss, EntryRole: gamestate.BattleEntrySolo, EnemyTypes: make([]int8, 6), Profiles: runtime.TeamBattleRewards, FameSeed: "independent-solo", FameSources: []game.TeamBattleFameSource{{MemberType: 1, LeaderFame: 1}}})
	if err != nil || !started {
		t.Fatal("start independent solo", err)
	}
	beforeBP := s.BattlePointState().Current
	a.initialState.TeamBattleRewards = nil
	a.initialState.TeamBattleReplays = nil
	// The server terminal callback settles before any client asks for results.
	settled, err := (&accountBusinessHandler{api: a}).SettleMultiplayerBattle(completed)
	if err != nil || settled == nil {
		t.Fatal("terminal account settlement failed", err)
	}
	if settled.ActiveTeamBattle == nil || settled.ActiveTeamBattle.FameSeed != "independent-solo" {
		t.Fatal("multiplayer replaced solo context")
	}
	if err := a.persistState(*settled); err != nil {
		t.Fatal(err)
	}
	if s.GoldState() != 21 || writes != 1 {
		t.Fatal("inventory still depended on result HTTP")
	}
	if repeated, err := (&accountBusinessHandler{api: a}).SettleMultiplayerBattle(completed); err != nil || repeated != nil || s.GoldState() != 21 {
		t.Fatal("repeated terminal callback granted again", err)
	}
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		a.teamBattleResult(w, httptest.NewRequest(http.MethodPost, "/result", strings.NewReader(`{"roomid":123}`)))
		return w
	}
	first := request()
	if first.Code != http.StatusOK {
		t.Fatalf("multi-wave result rejected: %d %s", first.Code, first.Body.String())
	}
	var result struct {
		MemberType int `json:"member_type"`
		Partners   []struct {
			MemberType int `json:"member_type"`
			IsOnline   int `json:"is_online"`
			Member     struct {
				UserID int `json:"userid"`
			} `json:"member"`
		} `json:"partners"`
	}
	lines := strings.Split(strings.TrimSpace(first.Body.String()), "\n")
	if len(lines) != 3 || json.Unmarshal([]byte(lines[1]), &result) != nil || result.MemberType != 1 || len(result.Partners) != 3 {
		t.Fatal("result lost self position or wrapped partners", first.Body.String())
	}
	for i, partner := range result.Partners {
		if partner.MemberType != i+2 || partner.IsOnline != 0 || partner.Member.UserID != 1002+i {
			t.Fatal("result lost a member position or NPC flag")
		}
	}
	if writes != 1 || s.Snapshot(gamestate.State{}).User.Gold != 21 || s.Snapshot(gamestate.State{}).User.BP != beforeBP || saved.User.Gold != 21 || len(saved.TeamBattleResultReceipts) != 1 {
		t.Fatalf("incorrect wave rewards or debit: writes=%d gold=%d BP=%d", writes, s.Snapshot(gamestate.State{}).User.Gold, s.Snapshot(gamestate.State{}).User.BP)
	}
	// Once saved, a retry is independent of the expired room and must return
	// the exact original response without rerolling rewards or charging again.
	a.multiplayer = multiplayer.NewHub()
	second := request()
	if second.Code != http.StatusOK || second.Body.String() != first.Body.String() || writes != 1 || s.Snapshot(gamestate.State{}).User.Gold != 21 {
		t.Fatal("result retry changed the receipt or granted rewards twice")
	}
	// Rebuild the account from its saved receipt and change the live balance.
	// Rewards stay frozen; replaying a receipt must not roll the user object back.
	a.account = testAccount(t, func(state *gamestate.State) {
		state.TeamBattleResultReceipts = saved.TeamBattleResultReceipts
		state.User.Gold = 31
	})
	reloaded := request()
	var before, after map[string]json.RawMessage
	if reloaded.Code != 200 || json.Unmarshal([]byte(lines[1]), &before) != nil ||
		json.Unmarshal([]byte(strings.Split(strings.TrimSpace(reloaded.Body.String()), "\n")[1]), &after) != nil {
		t.Fatal("saved result could not be rendered after reload", reloaded.Body.String())
	}
	var current struct {
		Gold int `json:"gold"`
	}
	if json.Unmarshal(after["user"], &current) != nil || current.Gold != 31 || writes != 1 || a.account.GoldState() != 31 {
		t.Fatal("replay changed the balance or serialized a stale user")
	}
	for _, field := range []string{"result_rewards", "clear_rewards", "fame_rewards", "host_rewards", "new_cards", "partners", "member_type"} {
		if string(before[field]) != string(after[field]) {
			t.Fatalf("persisted %s changed after reload", field)
		}
	}
}

func TestMultiplayerResultMembersFollowBattlePositions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		positions []int
		self      int
		want      []int
	}{
		{"sparse positions", []int{4, 1}, 4, []int{1}},
		{"repeated profession", []int{4, 2, 1, 3}, 2, []int{1, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completed := multiplayer.CompletedBattle{OnlineUserIDs: []int{1000 + tc.self, 1001}, CompletedAtUnix: 123456}
			for _, position := range tc.positions {
				completed.Members = append(completed.Members, multiplayer.Member{MemberType: position, ArthurType: 3, UserID: 1000 + position,
					DeckHonorIDs: make([]int, 4), RookieType: 1})
			}
			partners, self, err := multiplayerResultMembers(completed, 1000+tc.self, map[int]int8{1001: game.FriendStateFollow})
			if err != nil || self != tc.self || len(partners) != len(tc.want) {
				t.Fatal("actual member positions were not retained", self, partners, err)
			}
			for i, value := range partners {
				wrapper := value
				member := wrapper.Member
				if wrapper.MemberType != tc.want[i] || member.ArthurType != int8(3) || member.IsRookieBonus != int8(1) {
					t.Fatal("position, profession or rookie bonus was lost", wrapper)
				}
				if tc.want[i] == 1 && (wrapper.IsOnline != 1 || member.FriendState != game.FriendStateFollow) {
					t.Fatal("online friend became an NPC", wrapper)
				}
			}
			if len(completed.Members) == 4 {
				completed.OwnerMemberType = tc.self
				for i := range completed.Members {
					completed.Members[i].LeaderFame = 100
				}
				sources, claimant, err := multiplayerFameSources(completed, 1000+tc.self)
				if err != nil || claimant.MemberType != tc.self || len(sources) != 2 {
					t.Fatal("fame sources use profession instead of eligible member positions", sources, claimant, err)
				}
				host, owner, err := multiplayerHostBonusMemberType(completed, 1000+tc.self, claimant)
				if err != nil || !owner || host != tc.self {
					t.Fatal("room-owner reward lost its battle position", host, owner, err)
				}
			}
			completed.Members[len(completed.Members)-1] = completed.Members[0]
			if _, _, err := multiplayerResultMembers(completed, 1000+tc.self, nil); err == nil {
				t.Fatal("duplicate battle position was accepted")
			}
		})
	}
}

type resultCompletionRepository struct {
	multiplayer.CompletionRepository
	completed multiplayer.CompletedBattle
	err       error
}

func TestLostMultiplayerResultDoesNotBlockLogin(t *testing.T) {
	for _, ineligible := range []bool{false, true} {
		hub := multiplayer.NewHub()
		if ineligible {
			if err := hub.AttachCompletionRepository(resultCompletionRepository{completed: multiplayer.CompletedBattle{RoomID: 123, OnlineUserIDs: []int{1002}}}); err != nil {
				t.Fatal(err)
			}
		}
		s := testAccount(t, func(state *gamestate.State) { state.User.Gold = 123 })
		runtime := gamestate.State{}
		runtime.User.UserID = 1001
		a := &API{initialState: runtime, account: s, multiplayer: hub}
		for i := 0; i < 2; i++ {
			w := httptest.NewRecorder()
			a.teamBattleResult(w, httptest.NewRequest(http.MethodPost, "/TeamBattleResult", strings.NewReader(`{"roomid":123}`)))
			lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
			var common commonResponse
			if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.ResultCode != -3207 || common.ResultDeleteSaveData != 0 {
				t.Fatalf("lost result still blocks native recovery: %d %s", w.Code, w.Body.String())
			}
		}
		if s.Snapshot(gamestate.State{}).User.Gold != 123 || len(s.Snapshot(gamestate.State{}).TeamBattleResultReceipts) != 0 {
			t.Fatal("lost result mutated rewards")
		}
		// A retired wire-only receipt still prevents a completed room replaying
		// grants. Its old response shape is never sent to the modern client.
		var retired gamestate.TeamBattleResultReceipt
		if err := json.Unmarshal([]byte(`{"room_id":123,"claimed_at_unix":1,"response":{"members":[]}}`), &retired); err != nil {
			t.Fatal(err)
		}
		a.account = testAccount(t, func(state *gamestate.State) {
			state.User.Gold = 123
			state.TeamBattleResultReceipts = []gamestate.TeamBattleResultReceipt{retired}
		})
		w := httptest.NewRecorder()
		a.teamBattleResult(w, httptest.NewRequest(http.MethodPost, "/TeamBattleResult", strings.NewReader(`{"roomid":123}`)))
		var common commonResponse
		if w.Code != 200 || json.Unmarshal([]byte(strings.Split(strings.TrimSpace(w.Body.String()), "\n")[0]), &common) != nil ||
			common.ResultCode != -3207 || a.account.GoldState() != 123 {
			t.Fatal("retired result blocked recovery or granted again", w.Body.String())
		}
	}
}

func (resultCompletionRepository) NextRoomID(minimum int64) (int64, error) { return minimum, nil }

func (repository resultCompletionRepository) LoadCompleted(int64, time.Time) (multiplayer.CompletedBattle, time.Time, error) {
	return repository.completed, time.Now().Add(time.Minute), repository.err
}

func TestMultiplayerResultStorageFailureRemainsRetryable(t *testing.T) {
	hub := multiplayer.NewHub()
	if err := hub.AttachCompletionRepository(resultCompletionRepository{err: errors.New("temporary storage failure")}); err != nil {
		t.Fatal(err)
	}
	runtime := gamestate.State{}
	runtime.User.UserID = 1001
	a := &API{initialState: runtime, account: &game.Account{}, multiplayer: hub}
	w := httptest.NewRecorder()
	a.teamBattleResult(w, httptest.NewRequest(http.MethodPost, "/TeamBattleResult", strings.NewReader(`{"roomid":123}`)))
	if w.Code != http.StatusConflict {
		t.Fatalf("storage failure discarded a recoverable battle: %d %s", w.Code, w.Body.String())
	}
}

// Exercise the real relationship repository: the old nil/mock repository
// fixtures accepted the room-local IDs used by the owner's other Arthur decks.
func TestMultiplayerCompletionSeparatesFallbackIDsFromAccountRelationships(t *testing.T) {
	const host, boss, roomID = 1000001, 30010102, int64(123)
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if got := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 1); got != host {
		t.Fatalf("unexpected primary account: %d", got)
	}
	peer := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 2)
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	// Both real teammates are mutual friends, independently of AI possession.
	stamp := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT OR IGNORE INTO cn_local_account_follow (follower_user_id, followed_user_id, created_utc) VALUES (?, ?, ?), (?, ?, ?)`, host, peer, stamp, peer, host, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.FriendPointAccountStates(host, []int{peer, host, 2, 4}); err == nil || err.Error() != "invalid CN local-account relationship target" {
		t.Fatal("account repository must still reject synthetic account IDs", err)
	}
	drop := gamestate.TeamBattleEnemyDrop{Reward: gamestate.Reward{Type: 4, Num: 21, CardSkillLevels: []int16{}}}
	profile := gamestate.TeamBattleRewardProfile{BossID: boss, EnemyDrops: []gamestate.TeamBattleEnemyDrop{drop}}
	completed := multiplayer.CompletedBattle{RoomID: roomID, BossID: boss, OwnerMemberType: 1,
		EndType: 1, ReleasedDrops: []gamestate.TeamBattleEnemyDrop{drop},
		OnlineUserIDs: []int{host, peer}, CompletedAtUnix: time.Now().Unix(), HostCostPaid: true,
		SettlementPlan: gamestate.TeamBattleSettlementPlan{Profile: profile, EnemyTypes: []int8{0}}}
	for i, id := range []int{host, 2, peer, 4} {
		completed.Members = append(completed.Members, multiplayer.Member{MemberType: i + 1, ArthurType: i + 1, UserID: id, LeaderFame: 1, DeckHonorIDs: make([]int, 4), IsBuddy: 1})
	}
	var states []gamestate.State
	for _, claimant := range []int{host, peer} {
		s := testAccount(t, func(state *gamestate.State) {
			state.User.UserID = claimant
			state.User.Name = "SettlementPlayer"
			state.User.Gold, state.User.FriendPoint, state.User.BP, state.User.BPMax = 0, 0, 100, 100
			state.BattlePoint.RecoverySeconds = 180
			state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"5":15,"10":0,"half_bp":8}]}],"11":[],"12":[]}`)
			state.TeamBattleStartReceipts = []gamestate.TeamBattleStartReceipt{{RoomID: roomID, BossID: boss, BPUse: 15}}
			helper := &state.PlayerProgressionPolicy.Friends.HelperReward
			helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
		})
		runtime, err := accounts.LoadState(claimant)
		if err != nil {
			t.Fatal(err)
		}
		a := &API{initialState: runtime, account: s, friendPointAccounts: accounts}
		handler := &accountBusinessHandler{api: a}
		settled, err := handler.SettleMultiplayerBattle(completed)
		if err != nil || settled == nil {
			t.Fatalf("mixed human/AI completion failed for %d: %v", claimant, err)
		}
		if s.GoldState() != 21 || s.FriendPointState() != 20 {
			t.Fatalf("human friendship or existing AI reward semantics changed: gold=%d fp=%d", s.GoldState(), s.FriendPointState())
		}
		relations, err := accounts.FriendPointAccountStates(claimant, []int{host, peer})
		if err != nil {
			t.Fatal(err)
		}
		partners, self, err := multiplayerResultMembers(completed, claimant, relations)
		wantSelf := 1
		if claimant == peer {
			wantSelf = 3
		}
		if err != nil || self != wantSelf || len(partners) != 3 {
			t.Fatal("result lost the original battle seat", err)
		}
		for _, partner := range partners {
			if partner.MemberType == 2 || partner.MemberType == 4 {
				wantRelation := game.FriendStateFriend
				if claimant == host {
					wantRelation = 4
				}
				if partner.Member.UserID != host || partner.IsOnline != 0 || partner.Member.FriendState != wantRelation || partner.Member.ArthurType != int8(partner.MemberType) {
					t.Fatalf("fallback lost its source account, self/friend relation or battle identity: %+v", partner)
				}
			}
		}
		states = append(states, *settled)
		if again, err := handler.SettleMultiplayerBattle(completed); err != nil || again != nil || s.GoldState() != 21 || s.FriendPointState() != 20 {
			t.Fatal("completion retry granted twice", err)
		}
	}
	expires := time.Now().Add(time.Hour)
	if err := accounts.PersistCompletedBattle(completed, expires, states); err != nil {
		t.Fatal("atomic completion persistence failed", err)
	}
	if persisted, _, err := accounts.LoadCompleted(roomID, time.Now()); err != nil || len(persisted.OnlineUserIDs) != 2 {
		t.Fatal("completed room was not persisted", err)
	}
	for _, claimant := range []int{host, peer} {
		reloaded, err := accounts.LoadState(claimant)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, receipt := range reloaded.TeamBattleResultReceipts {
			found = found || receipt.RoomID == roomID
		}
		if !found || reloaded.User.Gold != 21 || reloaded.User.FriendPoint != 20 {
			t.Fatalf("missing or inconsistent saved result for %d", claimant)
		}
	}
	if completed.Members[1].UserID != 2 || completed.Members[3].UserID != 4 {
		t.Fatal("public account projection changed internal battle membership")
	}
	// Multiple displayed roles can lead to one source account. Follow remains
	// deduplicated before capacity checks, while synthetic IDs stay invalid.
	viewer := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 3)
	follow, err := accounts.FollowFriendPointAccounts(viewer, []int{host, host, host}, 1)
	if err != nil || len(follow.RequestUserIDs) != 1 || follow.RequestUserIDs[0] != host {
		t.Fatal("repeated result roles did not follow one account", err)
	}
	if _, err := accounts.FollowFriendPointAccounts(viewer, []int{2}, 10); err == nil {
		t.Fatal("battle placeholder became a public follow target")
	}
	// Public system partners and disconnected humans already carry account
	// identities; neither an offline seat nor IsBuddy identifies a placeholder.
	for _, accountID := range []int{1900000002, viewer} {
		copy := completed
		copy.Members = append([]multiplayer.Member(nil), completed.Members...)
		copy.Members[1].UserID = accountID
		partners, _, err := multiplayerResultMembers(copy, host, nil)
		if err != nil || partners[0].Member.UserID != accountID || partners[0].IsOnline != 0 {
			t.Fatal("public NPC/disconnected account identity was replaced", err)
		}
	}
}
