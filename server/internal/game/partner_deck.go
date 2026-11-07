package game

import (
	"fmt"
	"kairisei.local/server/internal/gamestate"
)

// PartnerDeckWire is the original CN shared deck projection used by solo partners
// and immutable clear-deck snapshots. It excludes account inventory identifiers.
func PartnerDeckWire(
	userID int,
	arthurType int8,
	deck DeckInfo,
	cards map[int64]CardInfo,
	job gamestate.JobParameter,
	avatar gamestate.Avatar,
	spheres map[int64]gamestate.Sphere,
	buddies map[int64]gamestate.Buddy,
	supportUnlocked int8,
	isBurst int8,
	calculated *gamestate.CardParameter,
) (map[string]any, error) {
	if arthurType == 5 && calculated == nil {
		return nil, fmt.Errorf("alchemist partner deck requires calculated parameters")
	}
	cardDeck := make([]any, 0, len(deck.CardUniqueIDs))
	for _, uniqueID := range deck.CardUniqueIDs {
		card, exists := cards[uniqueID]
		if !exists {
			return nil, fmt.Errorf("team battle deck card %d is unavailable", uniqueID)
		}
		cardDeck = append(cardDeck, PartnerCardWire(card))
	}
	if len(cardDeck) == 0 {
		return nil, fmt.Errorf("team battle deck is empty")
	}
	if supportUnlocked < 0 || int(supportUnlocked) > len(deck.SupportCardUniqueIDs) {
		return nil, fmt.Errorf("team battle support-card slot count is invalid")
	}
	// ProtoGen only allocates PartnerDeckInfo.support_deck when the wire list is
	// non-empty. ClearDeck then unconditionally unions deck and support_deck, so
	// sending [] for a profession with zero unlocked slots becomes a client-side
	// null and aborts the view. Keep the complete fixed slot vector on the wire;
	// support_card_unlock_slot_num remains the independent lock-state owner.
	supportDeck := make([]any, len(deck.SupportCardUniqueIDs))
	for index := range supportDeck {
		uniqueID := deck.SupportCardUniqueIDs[index]
		if uniqueID == 0 {
			supportDeck[index] = PartnerCardWire(CardInfo{SkillLevels: []int16{}})
			continue
		}
		card, exists := cards[uniqueID]
		if !exists {
			return nil, fmt.Errorf("team battle support card %d is unavailable", uniqueID)
		}
		supportDeck[index] = PartnerCardWire(card)
	}
	hp, attack, magic, mind := 0, 0, 0, 0
	if calculated != nil {
		hp, attack, magic, mind = calculated.HP, calculated.Attack, calculated.Magic, calculated.Mind
	} else {
		hp, attack, magic, mind = PartnerDeckStats(deck, cards, job)
	}
	if len(deck.SphereUniqueIDs) != DeckSphereSlots {
		return nil, fmt.Errorf("team battle deck must contain %d sphere slots", DeckSphereSlots)
	}
	sphereDeck := make([]any, DeckSphereSlots)
	for index, uniqueID := range deck.SphereUniqueIDs {
		if uniqueID == 0 {
			sphereDeck[index] = map[string]any{"sphrid": 0, "lv": 0}
			continue
		}
		sphere, exists := spheres[uniqueID]
		if !exists || sphere.SphereID <= 0 || sphere.Level <= 0 {
			return nil, fmt.Errorf("team battle deck sphere %d is unavailable", uniqueID)
		}
		sphereDeck[index] = map[string]any{
			"sphrid": sphere.SphereID,
			"lv":     sphere.Level,
		}
	}
	const battleBuddySlots = 5
	buddyDeck := make([]any, battleBuddySlots)
	for index := range buddyDeck {
		buddyDeck[index] = map[string]any{"buddyid": 0, "lv": 0}
	}
	buddyIndex := 0
	for _, uniqueID := range deck.BuddyUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		buddy, exists := buddies[uniqueID]
		if !exists {
			return nil, fmt.Errorf("team battle deck buddy %d is unavailable", uniqueID)
		}
		if buddyIndex >= len(buddyDeck) {
			return nil, fmt.Errorf("team battle deck has more than %d buddies", battleBuddySlots)
		}
		buddyDeck[buddyIndex] = map[string]any{
			"buddyid": buddy.BuddyID,
			"lv":      buddy.Level,
		}
		buddyIndex++
	}
	return map[string]any{
		"userid":                       userID,
		"arthur_type":                  arthurType,
		"job_type":                     deck.JobType,
		"is_burst":                     isBurst,
		"hp":                           hp,
		"atkp":                         attack,
		"intp":                         magic,
		"mndp":                         mind,
		"deck":                         cardDeck,
		"support_deck":                 supportDeck,
		"support_card_unlock_slot_num": supportUnlocked,
		"sphrs":                        sphereDeck,
		"buddys":                       buddyDeck,
		"avatar": map[string]any{
			"costumeid":       avatar.CostumeID,
			"avatar_partsids": append([]int(nil), avatar.AvatarPartIDs...),
		},
		"deck_rank":       deck.DeckRank,
		"leader_card_idx": deck.LeaderCardIndex,
		"name":            deck.Name,
		"rental_idx":      deck.Index,
		"play_log_turn":   0,
	}, nil
}

func PartnerCardWire(card CardInfo) map[string]any {
	return map[string]any{
		"cardid":   card.CardID,
		"lv":       card.Level,
		"skill_lv": append([]int16(nil), card.SkillLevels...),
		"love":     card.Love,
		"hp":       card.HP,
		"atkp":     card.Attack,
		"intp":     card.Magic,
		"mndp":     card.Mind,
		"fame":     card.Fame,
	}
}
