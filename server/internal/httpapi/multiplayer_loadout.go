package httpapi

import (
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/multiplayer"
)

func roomMember(loadout game.BattleLoadout) multiplayer.Member {
	return multiplayer.Member{
		ClearDeck:    loadout.ClearDeck,
		UserID:       loadout.UserID,
		Level:        loadout.Level,
		ArthurType:   loadout.ArthurType,
		JobType:      loadout.JobType,
		HP:           loadout.HP,
		Attack:       loadout.Attack,
		Magic:        loadout.Magic,
		Mind:         loadout.Mind,
		Name:         loadout.Name,
		LeaderCardID: loadout.LeaderCardID,
		LeaderFame:   loadout.LeaderFame,
		LeaderLevel:  loadout.LeaderLevel,
		DeckRank:     loadout.DeckRank,
		DeckName:     loadout.DeckName,
		CostumeID:    loadout.CostumeID,
		PartsIDs:     loadout.PartsIDs,
		DeckHonorIDs: loadout.DeckHonorIDs,
		DeckCards:    loadout.DeckCards,
		SupportCards: loadout.SupportCards,
		DeckSpheres:  loadout.DeckSpheres,
		DeckBuddies:  loadout.DeckBuddies,
		IsBurst:      loadout.IsBurst,
		IsBuddy:      loadout.IsBuddy,
	}
}

func (a *API) multiplayerMember(arthur, index int8) (multiplayer.Member, error) {
	loadout, err := a.account.BattleLoadout(a.initialState.User.UserID, arthur, index)
	return roomMember(loadout), err
}

func (a *API) multiplayerFallbackParty(arthur int8) []multiplayer.Member {
	loadouts := a.account.OwnerFallbackParty(a.initialState.User.UserID, arthur)
	members := make([]multiplayer.Member, 0, len(loadouts))
	for _, loadout := range loadouts {
		member := roomMember(loadout)
		member.IsRoomLoading = 1
		members = append(members, member)
	}
	return members
}
