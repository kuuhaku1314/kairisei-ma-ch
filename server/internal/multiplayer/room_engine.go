package multiplayer

import "kairisei.local/server/battlecore"

// Room-owned admission, account and publication data stops at this adapter.
// The constructor it calls accepts only battlecore values. The private room
// helper remains while the implementation moves into the public core package.
func newBattleEngine(catalog *CombatCatalog, spec RoomSpec, members []Member) (*BattleEngine, error) {
	config := battlecore.Config{
		EnemyPartyID: spec.EnemyPartyID, EnemyType: spec.EnemyType, CostInitial: spec.CostInitial,
		HoldMax: spec.HoldMax, BurstGaugeInitial: spec.BurstGaugeInitial,
		Seed: uint32(spec.Seed), ContinueAllowed: spec.ContinueAllowed,
		AwakeTakeover: spec.AwakeTakeover,
		Drops:         spec.Drops,
	}
	if spec.ScorePolicy != nil {
		config.EndTurn = spec.ScorePolicy.EndTurn
		config.MemberDeadEnd = spec.ScorePolicy.MemberDeadEnd
		config.ContinueAllowed = false
	}
	return NewBattleEngine(catalog, config, roomBattleParticipants(members))
}

func roomBattleParticipants(members []Member) []battlecore.Participant {
	participants := make([]battlecore.Participant, len(members))
	for i, member := range members {
		participants[i] = battlecore.Participant{
			MemberType: member.MemberType, ArthurType: member.ArthurType,
			HP: member.HP, Attack: member.Attack, Magic: member.Magic, Mind: member.Mind,
			DeckCards: member.DeckCards, SupportCards: member.SupportCards,
			DeckSpheres: member.DeckSpheres, DeckBuddies: member.DeckBuddies,
		}
	}
	return participants
}
