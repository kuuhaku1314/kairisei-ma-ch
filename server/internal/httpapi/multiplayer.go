package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

type teamBattleRoomIssue struct {
	BossID              int
	DeckArthurType      int8
	DeckArthurTypeIndex int8
	RoomType            int
	Password            string
	NeedDeckRank        int
	NeedHP              int
	NeedFame            int
	Comment             string
	AllowToLeave        int
	GameStartMemberNum  int
	AutoStart           bool
	SkipBattleMessages  bool
	GameSpeed           int
}

func (a *API) teamBattleMultiShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ActiveArthurType int8 `json:"active_arthur_type"`
	}
	if err := decodeExact(request, []string{"active_arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	// Directory browsing accepts every current profession; room admission is separate.
	if payload.ActiveArthurType < 1 || payload.ActiveArthurType > 5 {
		writeError(writer, http.StatusBadRequest, "invalid active Arthur type")
		return
	}
	catalog, err := a.account.TeamBattleCatalog()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	groups, err := teamBattleMultiGroups(catalog)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, groups)
}

func (a *API) teamBattleMultiRoomCreate(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil || a.battleSV.Host == "" || a.battleSV.Port == 0 {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		BossID               int    `json:"bossid"`
		DeckArthurType       int8   `json:"deck_arthur_type"`
		DeckArthurTypeIndex  int8   `json:"deck_arthur_type_idx"`
		RoomType             int8   `json:"room_type"`
		Password             string `json:"pass"`
		NeedDeckRank         int    `json:"deck_rank"`
		NeedHP               int    `json:"hp"`
		NeedFame             int    `json:"fame"`
		Comment              string `json:"comment"`
		AllowToLeave         int8   `json:"allow_to_leave"`
		UsePunishedFreePoint int8   `json:"use_punished_free_piont"`
		SkipBattleMessages   int8   `json:"is_battle_msg_skip"`
		BattleSpeedRate      int    `json:"battle_speed_rate"`
	}
	fields := []string{
		"bossid", "deck_arthur_type", "deck_arthur_type_idx", "room_type", "pass",
		"deck_rank", "hp", "fame", "comment", "allow_to_leave", "use_punished_free_piont",
		"is_battle_msg_skip", "battle_speed_rate",
	}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.BossID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 ||
		payload.DeckArthurTypeIndex < 0 || payload.RoomType < 0 || payload.RoomType > 1 ||
		payload.NeedDeckRank < 0 || payload.NeedHP < 0 || payload.NeedFame < 0 ||
		(payload.AllowToLeave != 0 && payload.AllowToLeave != 1) ||
		(payload.SkipBattleMessages != 0 && payload.SkipBattleMessages != 1) ||
		!multiplayer.ValidGameSpeed(payload.BattleSpeedRate) ||
		(payload.UsePunishedFreePoint != 0 && payload.UsePunishedFreePoint != 1) {
		writeError(writer, http.StatusBadRequest, "invalid local multiplayer room configuration")
		return
	}
	credential, err := a.issueTeamBattleRoom(teamBattleRoomIssue{
		BossID: payload.BossID, DeckArthurType: payload.DeckArthurType,
		DeckArthurTypeIndex: payload.DeckArthurTypeIndex, RoomType: int(payload.RoomType),
		Password: payload.Password, NeedDeckRank: payload.NeedDeckRank, NeedHP: payload.NeedHP,
		NeedFame: payload.NeedFame, Comment: payload.Comment,
		// Ordinary co-op requires two real players; the remaining jobs use
		// the owner's fallback decks. The client uses this same button gate.
		AllowToLeave: int(payload.AllowToLeave), GameStartMemberNum: 2,
		SkipBattleMessages: payload.SkipBattleMessages != 0,
		GameSpeed:          payload.BattleSpeedRate,
	})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"battlesv":                   a.battleSVWire(credential),
		"is_unlimited_free_continue": 0,
		"unlimited_free_continue_lv": 0,
		"free_continue_remain":       0,
		"member_type":                int(payload.DeckArthurType),
	})
}

func (a *API) teamBattleAIRoomCreate(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil || a.battleSV.Host == "" || a.battleSV.Port == 0 {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		BossID               int  `json:"bossid"`
		AIID                 int  `json:"ai_id"`
		DeckArthurType       int8 `json:"deck_arthur_type"`
		DeckArthurTypeIndex  int8 `json:"deck_arthur_type_idx"`
		UsePunishedFreePoint int8 `json:"use_punished_free_piont"`
	}
	fields := []string{"bossid", "ai_id", "deck_arthur_type", "deck_arthur_type_idx", "use_punished_free_piont"}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.BossID <= 0 || payload.AIID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 ||
		payload.DeckArthurTypeIndex < 0 || (payload.UsePunishedFreePoint != 0 && payload.UsePunishedFreePoint != 1) {
		writeError(writer, http.StatusBadRequest, "invalid local AI room configuration")
		return
	}
	if payload.AIID != localAIUserID(a.initialState.User.UserID, payload.DeckArthurType) {
		writeError(writer, http.StatusBadRequest, "unknown local AI room owner")
		return
	}
	credential, err := a.issueTeamBattleRoom(teamBattleRoomIssue{
		BossID: payload.BossID, DeckArthurType: payload.DeckArthurType,
		DeckArthurTypeIndex: payload.DeckArthurTypeIndex, RoomType: 0,
		AllowToLeave: 1, GameStartMemberNum: 1, AutoStart: true,
	})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"battlesv":                   a.battleSVWire(credential),
		"is_unlimited_free_continue": 0,
		"unlimited_free_continue_lv": 0,
		"free_continue_remain":       0,
	})
}

func (a *API) issueTeamBattleRoom(issue teamBattleRoomIssue) (multiplayer.Credential, error) {
	catalog, err := a.account.TeamBattleCatalog()
	if err != nil {
		return multiplayer.Credential{}, err
	}
	if !catalog.BossAccessible(issue.BossID, time.Now().Unix()) {
		return multiplayer.Credential{}, errors.New("team battle boss is locked")
	}
	groupID, bossGroup, found := catalog.GroupForBoss(issue.BossID)
	rules, rulesFound := catalog.EntryRules(issue.BossID)
	if !found {
		return multiplayer.Credential{}, errors.New("unknown team battle boss")
	}
	if !rulesFound || !rules.AllowsMultiplayer() {
		return multiplayer.Credential{}, errors.New("this battle does not allow multiplayer rooms")
	}
	replay, found := teamBattleReplayForBoss(a.initialState.TeamBattleReplays, issue.BossID)
	if !found {
		return multiplayer.Credential{}, errors.New("unknown team battle replay")
	}
	rewardProfile, found := game.TeamBattleRewardProfileForContext(
		a.initialState.TeamBattleRewards,
		game.TeamBattleContext{BossID: issue.BossID},
	)
	if !found {
		return multiplayer.Credential{}, errors.New("unknown team battle drop profile")
	}
	member, err := a.multiplayerMember(issue.DeckArthurType, issue.DeckArthurTypeIndex)
	if err != nil {
		return multiplayer.Credential{}, err
	}
	fallbackParty := a.multiplayerFallbackParty(issue.DeckArthurType)
	// The stock multiplayer picker uses bp_use_half; the request supplies no
	// price. Freeze the published cost in the room and debit only at start.
	bpUse := rules.BPUseHalf
	if bpUse <= 0 || bpUse > rules.BPUse {
		return multiplayer.Credential{}, errors.New("multiplayer battle point cost is unavailable")
	}
	if a.account.BattlePointState().Current < bpUse {
		return multiplayer.Credential{}, errors.New("multiplayer host battle points are insufficient")
	}
	battles, err := teamBattleReplayBattles(replay)
	if err != nil {
		return multiplayer.Credential{}, err
	}
	dropPlan, err := game.PlanTeamBattleDrops(rewardProfile, teamBattleEnemyTypes(battles), fmt.Sprintf("multi:issue:%d:%d", member.UserID, time.Now().UnixNano()))
	if err != nil {
		return multiplayer.Credential{}, err
	}
	costInitial := replay.CostInitial
	if policy := rewardProfile.ScorePolicy; policy != nil && policy.IsDamageScore() {
		costInitial = policy.TeamCostInitial
		if costInitial == 0 {
			costInitial = 3
		}
	}
	return a.multiplayer.IssueCreate(multiplayer.RoomSpec{
		SettlementPlan:     gamestate.TeamBattleSettlementPlan{Profile: rewardProfile, EnemyTypes: teamBattleEnemyTypes(battles), FameBonus: a.initialState.TeamBattleFameBonusPolicy, HostBonus: a.initialState.TeamBattleHostBonusPolicy},
		ScorePolicy:        rewardProfile.ScorePolicy,
		StageType:          rules.StageType,
		Battles:            battles,
		DropPlan:           dropPlan,
		FameRewardsSet:     true,
		FameRewards:        game.TeamBattleFamePool(rewardProfile, a.initialState.TeamBattleFameBonusPolicy),
		BattlePointUse:     bpUse,
		ContinueAllowed:    rules.Continue != 0,
		BossID:             issue.BossID,
		BossGroupID:        groupID,
		EnemyPartyID:       replay.EnemyPartyID,
		EnemyType:          int(replay.EnemyType),
		RoomType:           issue.RoomType,
		Password:           issue.Password,
		NeedDeckRank:       issue.NeedDeckRank,
		NeedHP:             issue.NeedHP,
		NeedFame:           issue.NeedFame,
		Comment:            issue.Comment,
		AllowToLeave:       issue.AllowToLeave,
		GameStartMemberNum: issue.GameStartMemberNum,
		AutoStart:          issue.AutoStart,
		SkipBattleMessages: issue.SkipBattleMessages,
		GameSpeed:          issue.GameSpeed,
		CostInitial:        costInitial,
		AwakeTakeover:      rules.AwakeTakeover,
		HoldMax:            replay.HoldMax,
		BurstGaugeInitial:  replay.BurstGaugeInitial,
		Seed:               game.NewBattleSeed(),
		BossGroup:          bossGroup,
		Owner:              member,
		OwnerFallbackParty: fallbackParty,
	})
}

func (a *API) teamBattleMultiRoomEnter(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil || a.battleSV.Host == "" || a.battleSV.Port == 0 {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		RoomID               int64 `json:"roomid"`
		DeckArthurType       int8  `json:"deck_arthur_type"`
		DeckArthurTypeIndex  int8  `json:"deck_arthur_type_idx"`
		UsePunishedFreePoint int8  `json:"use_punished_free_piont"`
	}
	fields := []string{"roomid", "deck_arthur_type", "deck_arthur_type_idx", "use_punished_free_piont"}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.RoomID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 ||
		payload.DeckArthurTypeIndex < 0 || (payload.UsePunishedFreePoint != 0 && payload.UsePunishedFreePoint != 1) {
		writeError(writer, http.StatusBadRequest, "invalid local multiplayer room entry")
		return
	}
	if err := a.authorizeMultiplayerRoom(payload.RoomID); err != nil {
		a.writeMultiplayerAvailabilityError(writer, err, false, http.StatusForbidden)
		return
	}
	member, err := a.multiplayerMember(payload.DeckArthurType, payload.DeckArthurTypeIndex)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	credential, err := a.multiplayer.IssueEnter(payload.RoomID, member)
	if err != nil {
		a.writeMultiplayerAvailabilityError(writer, err, false, http.StatusBadRequest)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"battlesv":                   a.battleSVWire(credential),
		"is_unlimited_free_continue": 0,
		"unlimited_free_continue_lv": 0,
		"free_continue_remain":       0,
	})
}

func (a *API) teamBattleMultiRoomReserve(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		RoomID         int64 `json:"roomid"`
		DeckArthurType int8  `json:"deck_arthur_type"`
	}
	if err := decodeExact(request, []string{"roomid", "deck_arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.RoomID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 {
		writeError(writer, http.StatusBadRequest, "invalid local multiplayer room reservation")
		return
	}
	if err := a.authorizeMultiplayerRoom(payload.RoomID); err != nil {
		a.writeMultiplayerAvailabilityError(writer, err, true, http.StatusForbidden)
		return
	}
	limit, err := a.multiplayer.Reserve(
		payload.RoomID,
		a.initialState.User.UserID,
		int(payload.DeckArthurType),
	)
	if err != nil {
		a.writeMultiplayerAvailabilityError(writer, err, true, http.StatusBadRequest)
		return
	}
	a.writeProtocol(writer, map[string]any{"reserve_limit_time": limit})
}

func (a *API) writeMultiplayerAvailabilityError(writer http.ResponseWriter, err error, reserving bool, fallbackStatus int) {
	code := -3208 // CN TEAMBATTLE_ROOM_NOT_FOUND.
	if errors.Is(err, multiplayer.ErrRoomArthurUnavailable) {
		// Both original reservation callbacks retry only NOT_FOUND. Entry also
		// handles ARTHUR_TYPE_FILL; retain that distinction after deck selection.
		if !reserving {
			code = -3202
		}
	} else if !errors.Is(err, multiplayer.ErrRoomUnavailable) {
		writeError(writer, fallbackStatus, err.Error())
		return
	}
	a.writeProtocolResult(writer, map[string]any{}, code, "房间或所选职业已不可用，请重新选择房间。")
}

func multiplayerRoomAllowsVisitor(roomType int, friendState int8) bool {
	return roomType != 1 || friendState == game.FriendStateFriend
}

func (a *API) authorizeMultiplayerRoom(roomID int64) error {
	room, exists := a.multiplayer.Snapshot(roomID)
	if !exists {
		return multiplayer.ErrRoomUnavailable
	}
	return a.authorizeMultiplayerRoomSnapshot(room)
}

func (a *API) authorizeMultiplayerRoomSnapshot(room multiplayer.RoomSnapshot) error {
	rules, found := a.account.TeamBattleEntryRulesForBoss(room.BossID)
	if !found || !rules.AllowsMultiplayer() {
		return multiplayer.ErrRoomUnavailable
	}
	if room.RoomType != 1 {
		return nil
	}
	ownerID := 0
	for _, member := range room.Members {
		if member.MemberType == room.OwnerMemberType {
			ownerID = member.UserID
			break
		}
	}
	states, err := a.friendPointAccountStates([]int{ownerID})
	if err != nil {
		return fmt.Errorf("load room owner friend state: %w", err)
	}
	if !multiplayerRoomAllowsVisitor(room.RoomType, states[ownerID]) {
		return errors.New("room only accepts the owner's friends")
	}
	return nil
}

func (a *API) teamBattleMultiRoomReserveCancel(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		RoomID         int64 `json:"roomid"`
		DeckArthurType int8  `json:"deck_arthur_type"`
	}
	if err := decodeExact(request, []string{"roomid", "deck_arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.RoomID <= 0 || payload.DeckArthurType < 1 || payload.DeckArthurType > 4 {
		writeError(writer, http.StatusBadRequest, "invalid local multiplayer room reservation cancellation")
		return
	}
	if err := a.multiplayer.CancelReservation(
		payload.RoomID,
		a.initialState.User.UserID,
		int(payload.DeckArthurType),
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{})
}

func (a *API) teamBattleResult(writer http.ResponseWriter, request *http.Request) {
	if a.multiplayer == nil {
		writeError(writer, http.StatusServiceUnavailable, "local multiplayer is unavailable")
		return
	}
	var payload struct {
		RoomID int64 `json:"roomid"`
	}
	if err := decodeExact(request, []string{"roomid"}, &payload); err != nil {
		a.writeExpiredBattleReport(writer)
		return
	}
	if payload.RoomID <= 0 {
		a.writeExpiredBattleReport(writer)
		return
	}
	a.teamBattleResultMu.Lock()
	defer a.teamBattleResultMu.Unlock()
	if cached, exists := a.account.TeamBattleResultReceipt(payload.RoomID); exists {
		a.writeTeamBattleResultSnapshot(writer, cached, true)
		return
	}
	_, err := a.multiplayer.SettlementFor(payload.RoomID, a.initialState.User.UserID)
	if err != nil {
		if errors.Is(err, multiplayer.ErrCompletedBattleIneligible) ||
			errors.Is(err, multiplayer.ErrCompletedBattleUnavailable) {
			// ResultMgr.onTeamBattleResult clears the pending battle report
			// before handling the native error. HTTP 409 never reaches it and
			// would make every subsequent login resubmit the same lost room.
			// A live, unfinished room or a storage failure remains retryable.
			a.writeExpiredBattleReport(writer)
			return
		}
		writeError(writer, http.StatusConflict, err.Error())
		return
	}
	// A completed room without an account snapshot is an expired historical
	// result. Querying results never performs a new inventory transaction.
	a.writeExpiredBattleReport(writer)
}

func multiplayerFameSources(
	completed multiplayer.CompletedBattle,
	claimantUserID int,
) ([]game.TeamBattleFameSource, game.TeamBattleFameSource, error) {
	if claimantUserID <= 0 || len(completed.Members) != 4 || len(completed.OnlineUserIDs) == 0 {
		return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame projection is incomplete")
	}
	eligibleUserIDs := make(map[int]struct{}, len(completed.OnlineUserIDs))
	for _, userID := range completed.OnlineUserIDs {
		if userID <= 0 {
			return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame claimant is invalid")
		}
		eligibleUserIDs[userID] = struct{}{}
	}
	sources := make([]game.TeamBattleFameSource, 0, len(eligibleUserIDs))
	seenMemberTypes := make(map[int]struct{}, len(completed.Members))
	claimant := game.TeamBattleFameSource{}
	for _, member := range completed.Members {
		if member.MemberType < 1 || member.MemberType > 4 || member.LeaderFame <= 0 ||
			member.LeaderFame > 100 {
			return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame member is invalid")
		}
		if _, duplicate := seenMemberTypes[member.MemberType]; duplicate {
			return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame member position is duplicated")
		}
		seenMemberTypes[member.MemberType] = struct{}{}
		if _, eligible := eligibleUserIDs[member.UserID]; !eligible {
			continue
		}
		source := game.TeamBattleFameSource{
			MemberType: member.MemberType,
			LeaderFame: member.LeaderFame,
		}
		sources = append(sources, source)
		if member.UserID == claimantUserID {
			claimant = source
		}
	}
	if len(sources) == 0 || claimant.MemberType == 0 {
		return nil, game.TeamBattleFameSource{}, errors.New("completed multiplayer fame claimant is unavailable")
	}
	sort.Slice(sources, func(left, right int) bool {
		return sources[left].MemberType < sources[right].MemberType
	})
	return sources, claimant, nil
}

func multiplayerHostBonusMemberType(
	completed multiplayer.CompletedBattle,
	claimantUserID int,
	claimant game.TeamBattleFameSource,
) (int, bool, error) {
	if claimantUserID <= 0 || claimant.MemberType < 1 || claimant.MemberType > 4 ||
		completed.OwnerMemberType < 1 || completed.OwnerMemberType > 4 || len(completed.Members) != 4 {
		return 0, false, errors.New("completed multiplayer host projection is incomplete")
	}
	for _, member := range completed.Members {
		if member.MemberType != completed.OwnerMemberType {
			continue
		}
		if member.UserID <= 0 || member.MemberType < 1 || member.MemberType > 4 {
			return 0, false, errors.New("completed multiplayer room owner is invalid")
		}
		if member.UserID != claimantUserID {
			return 0, false, nil
		}
		if member.MemberType != claimant.MemberType {
			return 0, false, errors.New("completed multiplayer room owner identity is inconsistent")
		}
		return member.MemberType, true, nil
	}
	return 0, false, errors.New("completed multiplayer room owner is unavailable")
}

func (a *API) battleSVWire(credential multiplayer.Credential) map[string]any {
	return map[string]any{
		"host":       a.battleSV.Host,
		"port":       a.battleSV.Port,
		"nginx_host": a.battleSV.Host,
		"nginx_port": a.battleSV.Port,
		"auth_token": credential.AuthToken,
		"signature":  credential.Signature,
	}
}

func teamBattleMultiGroups(catalog *game.TeamBattleCatalog) (map[string]any, error) {
	result := map[string]any{"event_bg_pictid": 0}
	for key, name := range map[string]string{
		"9": "normal_groups", "10": "special_groups", "11": "key_groups", "12": "event_groups",
	} {
		sourceGroups := catalog.Groups(key)
		groups := make([]any, 0, len(sourceGroups))
		for _, sourceGroup := range sourceGroups {
			group, err := teamBattleBossGroupNamedDTO(sourceGroup)
			if err != nil {
				return nil, fmt.Errorf("adapt team battle %s: %w", name, err)
			}
			groups = append(groups, group)
		}
		result[name] = groups
	}
	return result, nil
}

func nextAIArthurType(selectedArthurType int8) int8 {
	if selectedArthurType < 1 || selectedArthurType > 4 {
		return 1
	}
	return selectedArthurType%4 + 1
}

func localAIUserID(accountUserID int, selectedArthurType int8) int {
	return accountUserID*10 + int(nextAIArthurType(selectedArthurType))
}

func teamBattleGroupForID(configuration json.RawMessage, groupID int) (any, bool) {
	if groupID <= 0 {
		return nil, false
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(configuration, &top) != nil {
		return nil, false
	}
	for _, key := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if json.Unmarshal(top[key], &groups) != nil {
			continue
		}
		for _, group := range groups {
			var current int
			if json.Unmarshal(group["0"], &current) != nil || current != groupID {
				continue
			}
			encoded, err := json.Marshal(group)
			if err != nil {
				return nil, false
			}
			var wire any
			if json.Unmarshal(encoded, &wire) != nil {
				return nil, false
			}
			return wire, true
		}
	}
	return nil, false
}

func multiplayerRoomWire(
	snapshot multiplayer.RoomSnapshot,
	friendPointReward int,
	friendState int8,
) (map[string]any, error) {
	owner := snapshot.Members[0]
	for _, member := range snapshot.Members {
		if member.MemberType == snapshot.OwnerMemberType {
			owner = member
			break
		}
	}
	entryUsers := make([]any, 4)
	for index := range entryUsers {
		entryUsers[index] = map[string]any{"userid": 0, "is_burst": 0}
	}
	for _, member := range snapshot.Members {
		if member.ArthurType < 1 || member.ArthurType > len(entryUsers) {
			continue
		}
		entryUsers[member.ArthurType-1] = map[string]any{
			"userid":   member.UserID,
			"is_burst": member.IsBurst,
		}
	}
	bossGroup, err := multiplayerRoomBossGroupWire(snapshot.BossGroup, snapshot.BossID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"roomid":                 snapshot.RoomID,
		"bossid":                 snapshot.BossID,
		"owner":                  multiplayerPartnerWire(owner, friendPointReward, friendState),
		"owner_leader_card_fame": owner.LeaderFame,
		"need_deck_rank":         snapshot.NeedDeckRank,
		"need_hp":                snapshot.NeedHP,
		"need_fame":              snapshot.NeedFame,
		"comment":                snapshot.Comment,
		"boss_group":             bossGroup,
		"entry_users":            entryUsers,
		"attr_str":               "",
		"is_punished":            0,
	}, nil
}

func multiplayerRoomBossGroupWire(group any, bossID int) (any, error) {
	encoded, err := json.Marshal(group)
	if err != nil {
		return nil, fmt.Errorf("encode local multiplayer boss group: %w", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return nil, fmt.Errorf("decode local multiplayer boss group: %w", err)
	}
	named, err := teamBattleBossGroupNamedDTO(wire)
	if err != nil {
		return nil, err
	}
	bosses, ok := named["bosses"].([]any)
	if !ok || len(bosses) == 0 {
		return nil, errors.New("local multiplayer boss group has no boss")
	}
	// Both the lobby card and SetRoomIdx read bosses[0], not the room's
	// separate bossid. Reorder this detached DTO only, preserving the group.
	for index, value := range bosses {
		boss := value.(map[string]any)
		if boss["bossid"] != float64(bossID) {
			continue
		}
		copy(bosses[1:index+1], bosses[:index])
		bosses[0] = boss
		return named, nil
	}
	return nil, errors.New("local multiplayer room boss is absent from its group")
}

func teamBattleBossGroupNamedDTO(value any) (map[string]any, error) {
	source, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("team battle boss group is not an object")
	}
	result := remapNumericObject(source, []string{
		"boss_groupid", "stage_type", "searchid", "iconindex", "name",
		"appear_begin", "appear_end", "pictid", "recommended_arthur", "stage_quest_areaid",
	})
	bossValues, ok := source["10"].([]any)
	if !ok {
		return nil, errors.New("team battle boss group has no boss list")
	}
	bosses := make([]any, 0, len(bossValues))
	for _, bossValue := range bossValues {
		boss, err := teamBattleBossNamedDTO(bossValue)
		if err != nil {
			return nil, err
		}
		bosses = append(bosses, boss)
	}
	result["bosses"] = bosses

	storyValues, _ := source["11"].([]any)
	stories := make([]any, 0, len(storyValues))
	for _, storyValue := range storyValues {
		story, err := teamBattleStoryNamedDTO(storyValue)
		if err != nil {
			return nil, err
		}
		stories = append(stories, story)
	}
	result["stories"] = stories
	result["button_str1"] = arrayOrEmpty(source["12"])
	result["button_str2"] = arrayOrEmpty(source["13"])
	result["bonus_flag"] = source["14"]
	result["bonus_end_time"] = source["15"]
	for _, name := range []string{
		"stage_quest_state", "tower_quest_state", "tower_stories", "buttons",
		"main_quest_achievement_max", "main_quest_achievement_num",
	} {
		if value, exists := source[name]; exists {
			result[name] = value
		}
	}
	return result, nil
}

func teamBattleBossNamedDTO(value any) (map[string]any, error) {
	source, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("team battle boss is not an object")
	}
	result := remapNumericObject(source, []string{
		"bossid", "is_only_my_deck", "arthur_limit", "end_turn", "difficulty",
		"bp_use", "bp_use_half", "is_continue", "bg_num", "pictid", "state", "hint",
	})
	result["reward_cardids"] = remapNumericObjectArray(source["12"], []string{"cardid", "is_new"})
	result["reward_sphrids"] = remapNumericObjectArray(source["13"], []string{"sphrid", "is_new"})
	for index, name := range []string{
		"is_model", "user_buff_id", "unlock_expire_time", "is_daily_ranking",
	} {
		result[name] = source[fmt.Sprintf("%d", index+14)]
	}
	result["awake"] = remapNumericObjectValue(source["18"], []string{
		"is_turn", "is_cost", "is_buff", "is_sphr_count", "is_deck_trash", "is_deal",
	})
	for index, name := range []string{
		"needrank", "autofight", "need_hp", "mission_related", "is_punished", "start_rule",
	} {
		result[name] = source[fmt.Sprintf("%d", index+19)]
	}
	challengeValues, _ := source["25"].([]any)
	if len(challengeValues) == 0 {
		result["challenge"] = nil
	} else {
		result["challenge"] = remapNumericObjectArray(
			challengeValues,
			[]string{"num", "state_bit", "is_new"},
		)
	}
	result["is_lock"] = source["26"]
	result["unlock"] = remapNumericObjectValue(source["27"], []string{
		"name", "difficulty", "story_tab_name", "story_sectionid",
		"story_section_title", "story_title", "story_teambattle_group_name", "story_teambattle_title",
	})
	for _, name := range []string{"entry_limit", "costume_fix_type", "skip_itemid", "score_ranking_reward_url"} {
		if value, exists := source[name]; exists {
			result[name] = value
		}
	}
	return result, nil
}

func teamBattleStoryNamedDTO(value any) (map[string]any, error) {
	source, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("team battle story is not an object")
	}
	result := remapNumericObject(source, []string{
		"story_teambattleid", "talkid", "state_flag", "title", "is_lock",
	})
	result["unlock"] = remapNumericObjectValue(source["5"], []string{
		"name", "difficulty", "story_tab_name", "story_sectionid",
		"story_section_title", "story_title", "story_sub_title",
	})
	return result, nil
}

func remapNumericObject(source map[string]any, names []string) map[string]any {
	result := make(map[string]any, len(names))
	for index, name := range names {
		result[name] = source[fmt.Sprintf("%d", index)]
	}
	return result
}

func remapNumericObjectValue(value any, names []string) any {
	source, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return remapNumericObject(source, names)
}

func remapNumericObjectArray(value any, names []string) []any {
	values, _ := value.([]any)
	result := make([]any, 0, len(values))
	for _, item := range values {
		if source, ok := item.(map[string]any); ok {
			result = append(result, remapNumericObject(source, names))
		}
	}
	return result
}

func arrayOrEmpty(value any) []any {
	values, _ := value.([]any)
	if values == nil {
		return []any{}
	}
	return values
}

func multiplayerPartnerWire(member multiplayer.Member, friendPointReward int, friendState int8) map[string]any {
	return map[string]any{
		"userid":        member.UserID,
		"name":          member.Name,
		"lv":            member.Level,
		"arthur_type":   member.ArthurType,
		"is_burst":      member.IsBurst,
		"job_type":      member.JobType,
		"hp":            member.HP,
		"atkp":          member.Attack,
		"intp":          member.Magic,
		"mndp":          member.Mind,
		"deck_rank":     member.DeckRank,
		"leader_card":   map[string]any{"cardid": member.LeaderCardID, "lv": member.LeaderLevel, "skill_lv": []int{1, 1}, "love": 0, "hp": 0, "atkp": 0, "intp": 0, "mndp": 0, "fame": member.LeaderFame},
		"get_fp":        friendPointReward,
		"comment":       "",
		"friend_state":  friendState,
		"rookie_type":   member.RookieType,
		"deck_name":     member.DeckName,
		"rental_idx":    0,
		"play_log_turn": 0,
		"attr_nums":     []int{0, 0, 0, 0, 0, 0},
		"kind_nums":     []int{},
		"deck_honorids": append([]int(nil), member.DeckHonorIDs...),
	}
}
