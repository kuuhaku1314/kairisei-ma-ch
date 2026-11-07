package game

import (
	"encoding/json"
	"errors"
	"fmt"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/gamestate"
)

// BattleLoadout freezes only the selected deck and its owned dependencies.
type BattleLoadout struct {
	ClearDeck    json.RawMessage
	UserID       int
	Level        int
	ArthurType   int
	JobType      int
	HP           int
	Attack       int
	Magic        int
	Mind         int
	Name         string
	LeaderCardID int
	LeaderFame   int
	LeaderLevel  int
	DeckRank     int
	DeckName     string
	CostumeID    int
	PartsIDs     []int
	DeckHonorIDs []int
	DeckCards    []battlecore.BattleCard
	SupportCards []battlecore.BattleCard
	DeckSpheres  []battlecore.BattleSphere
	DeckBuddies  []battlecore.BattleBuddy
	IsBurst      int
	IsBuddy      int
}

func (s *Account) BattleLoadout(userID int, arthurType int8, deckIndex int8) (BattleLoadout, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.battleLoadoutLocked(userID, arthurType, deckIndex)
}

func (s *Account) battleLoadoutLocked(userID int, arthurType int8, deckIndex int8) (BattleLoadout, error) {
	deck, found := ExactDeck(s.decks, arthurType, deckIndex)
	if !found {
		return BattleLoadout{}, errors.New("local multiplayer deck is unavailable")
	}
	if len(deck.CardUniqueIDs) != 10 {
		return BattleLoadout{}, errors.New("local multiplayer deck must contain ten cards")
	}
	selected := make(map[int64]struct{}, len(deck.CardUniqueIDs)+len(deck.SupportCardUniqueIDs))
	for _, ids := range [][]int64{deck.CardUniqueIDs, deck.SupportCardUniqueIDs} {
		for _, id := range ids {
			selected[id] = struct{}{}
		}
	}
	cardByUniqueID := make(map[int64]CardInfo, len(selected))
	owned := make(map[int64]int, len(selected))
	for _, card := range s.cards {
		if _, needed := selected[card.UniqueID]; needed {
			cardByUniqueID[card.UniqueID] = card
			owned[card.UniqueID] = card.CardID
		}
	}
	if err := s.validateDeckCardFamilies(deck, owned, 0, 0, true); err != nil {
		return BattleLoadout{}, err
	}
	if s.deckRankPolicy.ConfigVersion != 0 {
		deck.DeckRank = s.deckRankLocked(deck, s.rankInventoryLocked())
	}
	job := gamestate.JobParameter{}
	if int(deck.JobType) >= 0 && int(deck.JobType) < len(s.currentJobs) {
		job = s.currentJobs[deck.JobType]
	}
	_, burstUnlocked := s.unlockedFeatureIDs[uint(32+arthurType)]
	burst := int8(BoolInt(burstUnlocked))
	leader, found := PartnerLeaderCard(deck, cardByUniqueID)
	if !found {
		return BattleLoadout{}, errors.New("local multiplayer leader card is unavailable")
	}
	stats, err := PartnerDeckStatsWithAlchemist(deck, cardByUniqueID, job, s.alchemistMain)
	if err != nil {
		return BattleLoadout{}, err
	}
	hp, attack, magic, mind := stats.HP, stats.Attack, stats.Magic, stats.Mind
	battleCards := make([]battlecore.BattleCard, len(deck.CardUniqueIDs))
	for index, uniqueID := range deck.CardUniqueIDs {
		card, exists := cardByUniqueID[uniqueID]
		if !exists || card.CardID <= 0 || card.Level <= 0 {
			return BattleLoadout{}, fmt.Errorf("local multiplayer card in deck slot %d is unavailable", index+1)
		}
		battleCards[index] = battlecore.BattleCard{
			CardType: index + 1,
			CardID:   card.CardID,
			Level:    card.Level,
			Fame:     card.Fame,
			Love:     card.Love,
		}
	}
	supportCards := make([]battlecore.BattleCard, 0, len(deck.SupportCardUniqueIDs))
	for index, uniqueID := range deck.SupportCardUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		card, exists := cardByUniqueID[uniqueID]
		if !exists || card.CardID <= 0 || card.Level <= 0 {
			return BattleLoadout{}, fmt.Errorf("local multiplayer support card in deck slot %d is unavailable", index+1)
		}
		supportCards = append(supportCards, battlecore.BattleCard{
			CardType: index + 11, CardID: card.CardID, Level: card.Level, Fame: card.Fame, Love: card.Love,
		})
	}
	sphereByUniqueID := make(map[int64]gamestate.Sphere)
	for _, sphere := range s.spheres {
		sphereByUniqueID[sphere.UniqueID] = sphere
	}
	battleSpheres := make([]battlecore.BattleSphere, 0, len(deck.SphereUniqueIDs))
	for index, uniqueID := range deck.SphereUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		sphere, exists := sphereByUniqueID[uniqueID]
		if !exists || sphere.SphereID <= 0 || sphere.Level <= 0 {
			return BattleLoadout{}, fmt.Errorf("local multiplayer sphere in deck slot %d is unavailable", index+1)
		}
		battleSpheres = append(battleSpheres, battlecore.BattleSphere{
			SphereType: index + 1,
			SphereID:   sphere.SphereID,
			Level:      sphere.Level,
		})
	}
	battleBuddies := make([]battlecore.BattleBuddy, 0, len(deck.BuddyUniqueIDs))
	buddyByUniqueID := make(map[int64]gamestate.Buddy)
	for _, buddy := range s.buddies {
		buddyByUniqueID[buddy.UniqueID] = buddy
	}
	for index, uniqueID := range deck.BuddyUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		buddy, exists := buddyByUniqueID[uniqueID]
		if !exists || buddy.BuddyID <= 0 || buddy.Level <= 0 {
			return BattleLoadout{}, fmt.Errorf("local multiplayer buddy in deck slot %d is unavailable", index+1)
		}
		battleBuddies = append(battleBuddies, battlecore.BattleBuddy{
			BuddyType: index + 1,
			BuddyID:   buddy.BuddyID,
			Level:     buddy.Level,
		})
	}
	// Alchemist uses the fixed client costume, not a fifth paper-doll save.
	// Nil parts are the client's supported "no paper-doll parts" form.
	avatar := gamestate.Avatar{CostumeID: 34}
	if arthurType != 5 {
		avatarIndex := int(arthurType) - 1
		if avatarIndex < 0 || avatarIndex >= len(s.avatars) {
			return BattleLoadout{}, errors.New("local multiplayer avatar is unavailable")
		}
		avatar = s.avatars[avatarIndex]
	}
	honorIDs := s.deckHonorIDs
	if len(honorIDs) != 4 {
		return BattleLoadout{}, errors.New("local multiplayer honor deck is unavailable")
	}
	if len(s.supportUnlockedSlots) != 4 {
		return BattleLoadout{}, errors.New("support unlock state unavailable")
	}
	wire, err := PartnerDeckWire(userID, arthurType, deck, cardByUniqueID, job, avatar,
		sphereByUniqueID, buddyByUniqueID, s.supportUnlockedForArthurLocked(arthurType), burst, &stats)
	if err != nil {
		return BattleLoadout{}, err
	}
	clearDeck, err := json.Marshal(wire)
	if err != nil {
		return BattleLoadout{}, err
	}
	return BattleLoadout{
		ClearDeck:    clearDeck,
		UserID:       userID,
		IsBurst:      int(burst),
		Level:        s.currentLevel,
		ArthurType:   int(arthurType),
		JobType:      int(deck.JobType),
		HP:           hp,
		Attack:       attack,
		Magic:        magic,
		Mind:         mind,
		Name:         s.currentName,
		LeaderCardID: leader.CardID,
		LeaderFame:   leader.Fame,
		LeaderLevel:  leader.Level,
		DeckRank:     int(deck.DeckRank),
		DeckName:     deck.Name,
		CostumeID:    avatar.CostumeID,
		PartsIDs:     append([]int(nil), avatar.AvatarPartIDs...),
		DeckHonorIDs: append([]int(nil), honorIDs...),
		DeckCards:    battleCards,
		SupportCards: supportCards,
		DeckSpheres:  battleSpheres,
		DeckBuddies:  battleBuddies,
		IsBuddy:      BoolInt(len(battleBuddies) > 0),
	}, nil
}

// multiplayerOwnerFallbackParty projects available active Arthur decks
// from the room owner's persisted account. Missing/unconfigured professions
// can still be filled by humans; countdown checks the actual remaining roles.
// Occupied AI seats need positive character IDs. Each room has one seat per
// profession, and IDs 1 through 4 are below the first login account (1000001).
func (s *Account) OwnerFallbackParty(userID int, selectedArthurType int8) []BattleLoadout {
	s.mu.RLock()
	defer s.mu.RUnlock()
	decks := s.decks
	result := make([]BattleLoadout, 0, 3)
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		if arthurType == selectedArthurType {
			continue
		}
		deckIndex, found := ActiveDeckIndex(decks, arthurType)
		if !found {
			continue
		}
		member, err := s.battleLoadoutLocked(userID, arthurType, deckIndex)
		if err != nil {
			continue
		}
		member.UserID = int(arthurType)
		member.Name = s.currentName
		result = append(result, member)
	}
	return result
}

func ActiveDeckIndex(decks []DeckInfo, arthurType int8) (int8, bool) {
	var fallback int8
	hasFallback := false
	for _, deck := range decks {
		if deck.ArthurType != arthurType {
			continue
		}
		if !hasFallback || deck.Index < fallback {
			fallback = deck.Index
			hasFallback = true
		}
		if deck.IsActive != 0 {
			return deck.Index, true
		}
	}
	return fallback, hasFallback
}
func ExactDeck(decks []DeckInfo, arthurType int8, deckIndex int8) (DeckInfo, bool) {
	for _, deck := range decks {
		if deck.ArthurType == arthurType && deck.Index == deckIndex {
			return deck, true
		}
	}
	return DeckInfo{}, false
}

// PartnerDeckStats serves the existing ordinary-profession partner and PVP
// views. Profession 5 uses PartnerDeckStatsWithAlchemist at its loadout boundary.
func PartnerDeckStats(
	deck DeckInfo,
	cards map[int64]CardInfo,
	job gamestate.JobParameter,
) (int, int, int, int) {
	stats, _ := partnerDeckStats(deck, cards, job, nil)
	return stats.HP, stats.Attack, stats.Magic, stats.Mind
}

// PartnerDeckStatsWithAlchemist changes only profession 5's main-card source.
// Support cards and the shared leader/support transforms remain unchanged.
func PartnerDeckStatsWithAlchemist(deck DeckInfo, cards map[int64]CardInfo, job gamestate.JobParameter, parameters AlchemistMainParameters) (gamestate.CardParameter, error) {
	if deck.ArthurType == 5 && parameters == nil {
		return gamestate.CardParameter{}, errors.New("alchemist main-card parameters are unavailable")
	}
	return partnerDeckStats(deck, cards, job, parameters)
}

func partnerDeckStats(deck DeckInfo, cards map[int64]CardInfo, job gamestate.JobParameter, parameters AlchemistMainParameters) (gamestate.CardParameter, error) {
	stats := gamestate.CardParameter{HP: job.HP, Attack: job.Attack, Magic: job.Magic, Mind: job.Mind}
	for index, uniqueID := range deck.CardUniqueIDs {
		card, exists := cards[uniqueID]
		if !exists {
			continue
		}
		if deck.ArthurType == 5 && parameters != nil {
			resolved, present, err := parameters.AlchemistMain(card.CardID, card.Level, card.Love, card.Fame)
			if err != nil {
				return gamestate.CardParameter{}, fmt.Errorf("alchemist deck card %d: %w", card.CardID, err)
			}
			if present {
				card.HP, card.Attack, card.Magic, card.Mind = resolved.HP, resolved.Attack, resolved.Magic, resolved.Mind
			}
		}
		if index == int(deck.LeaderCardIndex) {
			// CN battle5_api_leader_card_param_calc: each card parameter *150/100,
			// truncated separately. Job stats, support cards and skill power are
			// not leader parameters.
			card.HP = gamestate.DeckLeaderParameter(card.HP)
			card.Attack = gamestate.DeckLeaderParameter(card.Attack)
			card.Magic = gamestate.DeckLeaderParameter(card.Magic)
			card.Mind = gamestate.DeckLeaderParameter(card.Mind)
		}
		stats.HP += card.HP
		stats.Attack += card.Attack
		stats.Magic += card.Magic
		stats.Mind += card.Mind
	}
	for _, uniqueID := range deck.SupportCardUniqueIDs {
		card, exists := cards[uniqueID]
		if !exists {
			continue
		}
		// CN CardUtility truncates getLovePer before calling native
		// battle5_api_support_card_param_calc. card_param_bonus.csv SUPPORT
		// supplies HP/ATK/MAG/MIND rates 60/200/200/100; native scales them
		// by (10 + 90*lovePercent/100) percent, then truncates each stat.
		lovePercent := 100 // CardCsvData.getLovePer: cards without a love cap use 100%.
		if card.LoveMax > 0 {
			lovePercent = max(0, min(100, card.Love*100/card.LoveMax))
		}
		stats.HP += gamestate.DeckSupportParameter(card.HP, 60, lovePercent)
		stats.Attack += gamestate.DeckSupportParameter(card.Attack, 200, lovePercent)
		stats.Magic += gamestate.DeckSupportParameter(card.Magic, 200, lovePercent)
		stats.Mind += gamestate.DeckSupportParameter(card.Mind, 100, lovePercent)
	}
	return stats, nil
}
