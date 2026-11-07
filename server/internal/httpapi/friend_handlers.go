package httpapi

import (
	"errors"
	"net/http"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func friendPayload(friend gamestate.Friend) map[string]any {
	return map[string]any{
		"userid":            friend.UserID,
		"name":              friend.Name,
		"arthur_type":       friend.ArthurType,
		"is_burst":          friend.IsBurst,
		"lv":                friend.Level,
		"deck_rank":         friend.DeckRank,
		"state":             friend.FriendState,
		"leader_cardid":     friend.LeaderCardID,
		"leader_card_lv":    friend.LeaderCardLevel,
		"leader_card_fame":  friend.LeaderCardFame,
		"last_login_time":   friend.LastLoginTime,
		"comment":           friend.Comment,
		"pvp_point":         friend.PVPPoint,
		"is_first_matching": friend.IsFirstMatching,
		"is_rookie_bonus":   friend.IsRookieBonus,
		"deck_honorids":     friend.DeckHonorIDs,
	}
}

func friendPayloads(friends []gamestate.Friend) []any {
	result := make([]any, 0, len(friends))
	for _, friend := range friends {
		result = append(result, friendPayload(friend))
	}
	return result
}

func (a *API) friendSearch(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		InviteID string `json:"inviteid"`
	}
	if err := decodeExact(request, []string{"inviteid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	relations, err := a.friendPointAccounts.FindFriendPointAccountByInviteID(a.initialState.User.UserID, payload.InviteID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	friends := make([]gamestate.Friend, 0, 1)
	for _, relation := range relations {
		if relation.State.User.InviteID != payload.InviteID {
			continue
		}
		if friend, ok := friendPointAccountFriend(relation); ok {
			friends = append(friends, friend)
		}
		break
	}
	a.writeProtocol(writer, map[string]any{"user": friendPayloads(friends)})
}

func (a *API) followShow(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	relations, err := a.friendPointAccounts.ListFollowedFriendPointAccounts(a.initialState.User.UserID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	friends := make([]gamestate.Friend, 0)
	for _, relation := range relations {
		if relation.FriendState != game.FriendStateFriend && relation.FriendState != game.FriendStateFollow {
			continue
		}
		if friend, ok := friendPointAccountFriend(relation); ok {
			friends = append(friends, friend)
		}
	}
	a.writeProtocol(writer, map[string]any{
		"friends": friendPayloads(friends),
	})
}

func (a *API) followerShow(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		Page int `json:"page"`
	}
	if err := decodeExact(request, []string{"page"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	relations, total, err := a.friendPointAccounts.ListFollowerFriendPointAccounts(a.initialState.User.UserID, a.account.FollowerDisplayLimit())
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	followerFriends := make([]gamestate.Friend, 0)
	for _, relation := range relations {
		if relation.FriendState != game.FriendStateFriend && relation.FriendState != game.FriendStateFollower {
			continue
		}
		if friend, ok := friendPointAccountFriend(relation); ok {
			followerFriends = append(followerFriends, friend)
		}
	}
	friends := friendPayloads(followerFriends)
	a.writeProtocol(writer, map[string]any{
		"friends":      friends,
		"follower_num": total,
		"page_max":     1,
	})
}

func (a *API) followAdd(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UserIDs []int `json:"userids"`
	}
	if err := decodeExact(request, []string{"userids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if a.friendPointAccounts == nil {
		writeError(writer, http.StatusInternalServerError, "local-account friend repository is unavailable")
		return
	}
	followMax := a.account.FollowMaximum()
	result, err := a.friendPointAccounts.FollowFriendPointAccounts(
		a.initialState.User.UserID,
		payload.UserIDs,
		followMax,
	)
	if err != nil {
		var followErr *game.FollowAddError
		if errors.As(err, &followErr) {
			a.writeProtocolResult(writer, map[string]any{
				"request_userids": []int{},
				"is_friend_full":  0,
			}, followErr.ResultCode, followErr.ResultString)
			return
		}
		a.writeStoreError(writer, err)
		return
	}
	friendFull := 0
	if result.IsFriendFull {
		friendFull = 1
	}
	a.writeProtocol(writer, map[string]any{
		"request_userids": result.RequestUserIDs,
		"is_friend_full":  friendFull,
	})
}

func (a *API) followUnfollow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UserID int `json:"userid"`
	}
	if err := decodeExact(request, []string{"userid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if a.friendPointAccounts == nil {
		writeError(writer, http.StatusInternalServerError, "local-account friend repository is unavailable")
		return
	}
	if err := a.friendPointAccounts.UnfollowFriendPointAccount(
		a.initialState.User.UserID,
		payload.UserID,
	); err != nil {
		var followErr *game.FollowAddError
		if errors.As(err, &followErr) {
			a.writeProtocolResult(writer, map[string]any{}, followErr.ResultCode, followErr.ResultString)
			return
		}
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{})
}

func profilePayload(friend gamestate.Friend) map[string]any {
	return map[string]any{
		"name": friend.Name,
		"lv":   friend.Level,
		"hp":   friend.HP,
		"atkp": friend.Attack,
		"intp": friend.Magic,
		"mndp": friend.Mind,
		"deck": map[string]any{
			"arthur_type":      friend.ArthurType,
			"is_burst":         friend.IsBurst,
			"job_type":         friend.JobType,
			"deck_rank":        friend.DeckRank,
			"leader_cardid":    friend.LeaderCardID,
			"leader_card_lv":   friend.LeaderCardLevel,
			"leader_card_fame": friend.LeaderCardFame,
		},
		"comment":         friend.Comment,
		"last_login_time": friend.LastLoginTime,
		"friend_state":    friend.FriendState,
		"pvp_point":       friend.PVPPoint,
		"deck_honorids":   friend.DeckHonorIDs,
	}
}

func (a *API) localProfilePayload() map[string]any {
	user := a.initialState.User
	activeArthurType := a.account.ActiveArthurType()
	deck, leader, hp, attack, magic, mind, found := a.activeDeckProfile()
	leaderLevel := 1
	leaderFame := 1
	leaderCardID := user.LeaderCardID
	jobType := activeArthurType
	deckRank := user.DeckRank
	if found {
		leaderCardID = leader.CardID
		leaderLevel = leader.Level
		leaderFame = leader.Fame
		jobType = deck.JobType
		deckRank = deck.DeckRank
	}
	pvpPoint, _ := a.account.PvpStatus()
	return map[string]any{
		"name": a.account.UserName(),
		"lv":   a.account.UserLevel(),
		"hp":   hp,
		"atkp": attack,
		"intp": magic,
		"mndp": mind,
		"deck": map[string]any{
			"arthur_type":      activeArthurType,
			"is_burst":         a.account.ArthurBurstUnlocked(activeArthurType),
			"job_type":         jobType,
			"deck_rank":        deckRank,
			"leader_cardid":    leaderCardID,
			"leader_card_lv":   leaderLevel,
			"leader_card_fame": leaderFame,
		},
		"comment":         a.account.UserComment(),
		"last_login_time": time.Now().Unix(),
		"friend_state":    0,
		"pvp_point":       pvpPoint,
		"deck_honorids": func() []int {
			deckHonorIDs, _ := a.account.HonorState()
			return deckHonorIDs
		}(),
	}
}

func (a *API) activeDeckProfile() (
	game.DeckInfo,
	game.CardInfo,
	int,
	int,
	int,
	int,
	bool,
) {
	cards, decks := a.account.Show()
	deck, found := game.SelectPartnerDeck(decks, a.account.ActiveArthurType())
	if !found {
		return game.DeckInfo{}, game.CardInfo{}, 0, 0, 0, 0, false
	}
	cardByUniqueID := make(map[int64]game.CardInfo, len(cards))
	for _, card := range cards {
		cardByUniqueID[card.UniqueID] = card
	}
	leader, found := game.PartnerLeaderCard(deck, cardByUniqueID)
	if !found {
		return game.DeckInfo{}, game.CardInfo{}, 0, 0, 0, 0, false
	}
	hp, attack, magic, mind := game.PartnerDeckStats(
		deck,
		cardByUniqueID,
		a.account.JobParameter(deck.JobType),
	)
	return deck, leader, hp, attack, magic, mind, true
}

func (a *API) userProfileShow(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		UserID int `json:"userid"`
	}
	if err := decodeExact(request, []string{"userid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.UserID == a.initialState.User.UserID {
		a.writeProtocol(
			writer,
			map[string]any{"profile": a.localProfilePayload()},
		)
		return
	}
	relations, err := a.friendPointAccounts.LoadFriendPointAccountRelations(a.initialState.User.UserID, []int{payload.UserID})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	for _, relation := range relations {
		if relation.State.User.UserID != payload.UserID {
			continue
		}
		if friend, ok := friendPointAccountFriend(relation); ok {
			a.writeProtocol(writer, map[string]any{"profile": profilePayload(friend)})
			return
		}
		break
	}
	writeError(writer, http.StatusBadRequest, "unknown profile user ID")
}
