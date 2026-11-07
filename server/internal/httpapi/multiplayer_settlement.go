package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"time"
)

// The account coordinator owns the transaction locks and commits all returned
// states with the immutable room completion. No HTTP request triggers grants.
func (h *accountBusinessHandler) SettleMultiplayerBattle(completed multiplayer.CompletedBattle) (*gamestate.State, error) {
	a := h.api
	if _, exists := a.account.TeamBattleResultReceipt(completed.RoomID); exists {
		return nil, nil
	}
	if err := a.settleMultiplayerBattle(completed); err != nil {
		return nil, err
	}
	state := a.account.Snapshot(a.initialState)
	return &state, nil
}

func (a *API) settleMultiplayerBattle(completed multiplayer.CompletedBattle) error {
	memberUserIDs := make([]int, 0, len(completed.Members))
	for _, member := range completed.Members {
		accountID, err := multiplayerMemberAccountID(completed, member)
		if err != nil {
			return err
		}
		memberUserIDs = append(memberUserIDs, accountID)
	}
	friendStates, err := a.friendPointAccountStates(memberUserIDs)
	if err != nil {
		return fmt.Errorf("load multiplayer friend states: %w", err)
	}
	partners, selfMemberType, err := multiplayerResultMembers(
		completed,
		a.initialState.User.UserID,
		friendStates,
	)
	if err != nil {
		return err
	}
	fameSources, claimantFameSource, err := multiplayerFameSources(
		completed,
		a.initialState.User.UserID,
	)
	if err != nil {
		return err
	}
	hostBonusMemberType, isRoomOwner, err := multiplayerHostBonusMemberType(
		completed,
		a.initialState.User.UserID,
		claimantFameSource,
	)
	if err != nil {
		return err
	}
	if isRoomOwner && !completed.HostCostPaid {
		return errors.New("multiplayer start payment is unavailable")
	}
	friendPointReward := 0
	for _, member := range completed.Members {
		if member.UserID == a.initialState.User.UserID {
			continue
		}
		friendPointReward += a.account.FriendPointRewardForState(friendStates[member.UserID])
	}
	entryRole := gamestate.BattleEntryMultiplayerGuest
	prepaidRoomID := int64(0)
	if isRoomOwner {
		entryRole = gamestate.BattleEntryMultiplayerHost
		prepaidRoomID = completed.RoomID
	}
	players, arthur := 0, int8(0)
	for _, member := range completed.Members {
		if member.IsBuddy == 0 {
			players++
		}
		if member.UserID == a.initialState.User.UserID {
			arthur = int8(member.ArthurType)
		}
	}
	completedAt := time.Unix(completed.CompletedAtUnix, 0)
	settlement, err := a.account.CompleteMultiplayerBattle(game.TeamBattleContext{
		MissionPlayers: players, MissionArthur: arthur,
		BossID: completed.BossID, StageType: completed.StageType, EntryRole: entryRole,
		FameSeed: fmt.Sprintf("multi:room=%d", completed.RoomID), FameSources: fameSources,
		HostBonusMemberType: hostBonusMemberType, FriendPointPartners: len(completed.Members) - 1,
		FriendPointReward: friendPointReward, PrepaidRoomID: prepaidRoomID,
	}, completed.SettlementPlan, completed.EndType != 2, multiplayerDropReport(completed), completedAt)
	if err != nil {
		return err
	}
	isClear := game.BoolInt(completed.EndType != 2)
	snapshot := settlement.ResultSnapshot(isClear, selfMemberType, completed.SettlementPlan.FameBonus.BonusFameAdd, partners)
	encodedSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		return errors.New("encode multiplayer settlement snapshot")
	}
	if err := a.account.RecordTeamBattleResultReceipt(completed.RoomID, encodedSnapshot, completedAt); err != nil {
		return err
	}
	return nil
}
