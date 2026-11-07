package httpapi

import (
	"errors"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func battleResultPartnerWire(memberType, isOnline int, member gamestate.Friend) map[string]any {
	return map[string]any{
		"member_type": memberType,
		"is_online":   isOnline,
		"member":      friendPayload(member),
	}
}

func teamBattleSoloResultPartners(selected []game.TeamBattleResultPartner, friendStates map[int]int8) ([]game.TeamBattleResultMember, error) {
	if len(selected) > 3 {
		return nil, errors.New("team battle result exceeds the partner slot limit")
	}
	partners := make([]game.TeamBattleResultMember, 0, len(selected))
	for index, partner := range selected {
		friendState := friendStates[partner.UserID]
		if partner.IsSelf {
			friendState = 4 // Original TeamBattleMemberWindow's all-self branch.
		}
		partners = append(partners, game.TeamBattleResultMember{MemberType: 2 + index, Member: gamestate.Friend{
			UserID: partner.UserID, Name: partner.Name, ArthurType: partner.ArthurType,
			IsBurst: partner.IsBurst, Level: partner.Level, DeckRank: partner.DeckRank,
			FriendState: friendState, LeaderCardID: partner.LeaderCardID,
			LeaderCardLevel: partner.LeaderLevel, LeaderCardFame: partner.LeaderFame,
			LastLoginTime: int(partner.LastLoginUnix), Comment: partner.Comment,
			PVPPoint: partner.PVPPoint, DeckHonorIDs: append([]int(nil), partner.HonorIDs...),
		}})
	}
	return partners, nil
}

func multiplayerResultMembers(completed multiplayer.CompletedBattle, userID int, friendStates map[int]int8) ([]game.TeamBattleResultMember, int, error) {
	// The regular multiplayer fame consumer requires a nonempty partner array.
	if len(completed.Members) < 2 || len(completed.Members) > 4 || len(completed.OnlineUserIDs) == 0 {
		return nil, 0, errors.New("completed multiplayer member projection is incomplete")
	}
	online := make(map[int]struct{}, len(completed.OnlineUserIDs))
	for _, onlineUserID := range completed.OnlineUserIDs {
		online[onlineUserID] = struct{}{}
	}
	byMemberType := make(map[int]multiplayer.Member, len(completed.Members))
	selfMemberType := 0
	for _, member := range completed.Members {
		if member.MemberType < 1 || member.MemberType > 4 || member.ArthurType < 1 || member.ArthurType > 5 || len(member.DeckHonorIDs) != 4 {
			return nil, 0, errors.New("completed multiplayer member is invalid")
		}
		if _, duplicate := byMemberType[member.MemberType]; duplicate {
			return nil, 0, errors.New("completed multiplayer member position is duplicated")
		}
		byMemberType[member.MemberType] = member
		if member.UserID == userID {
			if selfMemberType != 0 {
				return nil, 0, errors.New("completed multiplayer account member is duplicated")
			}
			selfMemberType = member.MemberType
		}
	}
	if selfMemberType == 0 {
		return nil, 0, errors.New("completed multiplayer account member is unavailable")
	}
	partners := make([]game.TeamBattleResultMember, 0, len(completed.Members)-1)
	for memberType := 1; memberType <= 4; memberType++ {
		member, exists := byMemberType[memberType]
		if !exists || memberType == selfMemberType {
			continue
		}
		isOnline := 0
		if _, exists := online[member.UserID]; exists {
			isOnline = 1
		}
		accountID, err := multiplayerMemberAccountID(completed, member)
		if err != nil {
			return nil, 0, err
		}
		friendState := friendStates[accountID]
		if accountID == userID {
			friendState = 4 // Own fallback decks must not offer a self-follow.
		}
		partners = append(partners, game.TeamBattleResultMember{MemberType: memberType, IsOnline: isOnline, Member: gamestate.Friend{
			UserID: accountID, Name: member.Name, ArthurType: int8(member.ArthurType),
			IsBurst: int8(member.IsBurst), Level: member.Level, DeckRank: int8(member.DeckRank),
			FriendState: friendState, LeaderCardID: member.LeaderCardID,
			LeaderCardLevel: member.LeaderLevel, LeaderCardFame: member.LeaderFame,
			LastLoginTime: int(completed.CompletedAtUnix), IsFirstMatching: int8(member.IsFirstMatch),
			IsRookieBonus: int8(member.RookieType), DeckHonorIDs: append([]int(nil), member.DeckHonorIDs...),
		}})
	}
	return partners, selfMemberType, nil
}

// OwnerFallbackParty reserves IDs 1..4 for its room-local Arthur identities.
// Account queries and result follow targets use the frozen owner's account;
// battle/recovery membership and reward claimants retain the original IDs.
// Disconnected humans and public system partners already have account IDs.
func multiplayerMemberAccountID(completed multiplayer.CompletedBattle, member multiplayer.Member) (int, error) {
	if member.UserID < 1 || member.UserID > 4 {
		return member.UserID, nil
	}
	for _, owner := range completed.Members {
		if owner.MemberType == completed.OwnerMemberType && owner.UserID > 4 {
			return owner.UserID, nil
		}
	}
	return 0, errors.New("completed multiplayer fallback account owner is unavailable")
}
