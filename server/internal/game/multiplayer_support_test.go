package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestMultiplayerMemberFreezesSupportSlotsAndLove(t *testing.T) {
	deck := DeckInfo{Name: "support deck", ArthurType: 1, JobType: 1, LeaderCardIndex: 0, SupportCardUniqueIDs: []int64{0, 11}, SphereUniqueIDs: make([]int64, 3)}
	s := &Account{currentName: "player name", deckHonorIDs: []int{0, 0, 0, 0}, avatars: []gamestate.Avatar{{}}, supportUnlockedSlots: make([]int8, 4)}
	for i := int64(1); i <= 10; i++ {
		deck.CardUniqueIDs = append(deck.CardUniqueIDs, i)
		s.cards = append(s.cards, CardInfo{UniqueID: i, CardID: int(i), Level: 1, HP: 100, Fame: 1})
	}
	s.cards = append(s.cards, CardInfo{UniqueID: 11, CardID: 123, Level: 60, Love: 5000, LoveMax: 10000})
	s.decks = []DeckInfo{deck}
	setLoadoutFixtureFamilies(s)
	member, err := s.BattleLoadout(0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if member.Name != "player name" || member.DeckName != "support deck" {
		t.Fatal("room player identity was replaced by a deck label")
	}
	if len(member.SupportCards) != 1 || member.SupportCards[0].CardType != 12 || member.SupportCards[0].Love != 5000 || member.SupportCards[0].CardID != 123 || member.SupportCards[0].Level != 60 {
		t.Fatalf("support identity/love/empty slot lost: %+v", member.SupportCards)
	}
	s.cards[10].Love = 10000
	s.decks[0].SupportCardUniqueIDs[1] = 0
	if member.SupportCards[0].Love != 5000 || member.SupportCards[0].CardID != 123 {
		t.Fatal("editing an account mutated the frozen battle deck")
	}
}

func TestOwnerFallbackUsesSelectedDeckWithoutReplacingEmptySelection(t *testing.T) {
	s := &Account{currentName: "owner name", deckHonorIDs: make([]int, 4), avatars: make([]gamestate.Avatar, 4), supportUnlockedSlots: make([]int8, 4)}
	for id := int64(1); id <= 10; id++ {
		s.cards = append(s.cards, CardInfo{UniqueID: id, CardID: int(id), Level: 1, HP: 100, Fame: 1})
	}
	for arthur := int8(2); arthur <= 4; arthur++ {
		for index := int8(0); index <= 1; index++ {
			deck := DeckInfo{ArthurType: arthur, JobType: arthur, Index: index, IsActive: index,
				Name: "first", CardUniqueIDs: make([]int64, 10), SphereUniqueIDs: make([]int64, 3)}
			if index == 1 {
				deck.Name = "selected second"
			}
			if arthur == 2 || (arthur == 3 && index == 0) {
				for i := range deck.CardUniqueIDs {
					deck.CardUniqueIDs[i] = int64(i + 1)
				}
			}
			s.decks = append(s.decks, deck)
		}
	}
	setLoadoutFixtureFamilies(s)
	party := s.OwnerFallbackParty(1000001, 1)
	if len(party) != 1 || party[0].Name != "owner name" || party[0].ArthurType != 2 || party[0].DeckName != "selected second" || party[0].UserID != 2 {
		t.Fatalf("fallback must use the selected playable deck, never another deck for an empty selection: %+v", party)
	}
	s.decks[1].CardUniqueIDs[0] = 0
	if party[0].DeckCards[0].CardID != 1 || len(s.OwnerFallbackParty(1000001, 1)) != 0 {
		t.Fatal("room deck snapshot changed or empty selected deck still became an AI")
	}
}

func setLoadoutFixtureFamilies(s *Account) {
	s.cardDefinitions = make(map[int]gamestate.Card)
	for _, card := range s.cards {
		s.cardDefinitions[card.CardID] = gamestate.Card{CardID: card.CardID, SameCardID: card.CardID, SameSupportCardID: card.CardID}
	}
}
