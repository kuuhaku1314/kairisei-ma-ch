package game

import (
	"encoding/json"
	"errors"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/gamestate"
)

// Rules come from the canonical boss DTO, including its named slot extension.
type TeamBattleEntryRules struct {
	BossID        int                       `json:"0"`
	OnlyMyDeck    int                       `json:"1"`
	BPUse         int                       `json:"5"`
	BPUseHalf     int                       `json:"6"`
	Continue      int                       `json:"7"`
	StartRule     int                       `json:"24"`
	EntryLimit    []TeamBattleEntryLimit    `json:"entry_limit"`
	StageType     gamestate.BattleStageType `json:"-"`
	AwakeTakeover battlemodel.WaveTakeover  `json:"-"`
}

type TeamBattleEntryLimit struct {
	Slot1ArthurType  int  `json:"slot1_arthur_type"`
	Slot2ArthurType  int  `json:"slot2_arthur_type"`
	Slot3ArthurType  int  `json:"slot3_arthur_type"`
	Slot4ArthurType  int  `json:"slot4_arthur_type"`
	Slot1PartnerType int8 `json:"slot1_partner_type"`
	Slot2PartnerType int8 `json:"slot2_partner_type"`
	Slot3PartnerType int8 `json:"slot3_partner_type"`
	Slot4PartnerType int8 `json:"slot4_partner_type"`
}

// Solo chooses the first playable matching/free slot for the controlled deck,
// then supplies the other enabled slots in their original order. FOLLOW_DECK
// excludes the controlled player, but still needs a borrowed deck in solo.
func (rules TeamBattleEntryRules) ValidateSoloParty(arthur int8, partners []TeamBattleResultPartner) error {
	if rules.StageType == gamestate.BattleStageAlchemist || rules.StageType == gamestate.BattleStageAlchemistSP {
		if arthur != 5 || len(partners) != 0 {
			return errors.New("alchemist solo requires the player's alchemist deck without partners")
		}
		return nil
	}
	if arthur < 1 || arthur > 4 {
		return errors.New("ordinary solo profession is unavailable")
	}
	types := [4]int{1, 2, 3, 4}
	filters := [4]int8{}
	if len(rules.EntryLimit) != 0 {
		entry := rules.EntryLimit[0]
		types = [4]int{entry.Slot1ArthurType, entry.Slot2ArthurType, entry.Slot3ArthurType, entry.Slot4ArthurType}
		filters = [4]int8{entry.Slot1PartnerType, entry.Slot2PartnerType, entry.Slot3PartnerType, entry.Slot4PartnerType}
	}
	playerSlot := -1
	for slot, required := range types {
		if filters[slot] != 2 && (required == 0 || required == int(arthur)) {
			playerSlot = slot
			break
		}
	}
	if playerSlot < 0 {
		return errors.New("selected profession cannot enter this battle")
	}
	index := 0
	for slot, required := range types {
		if required < 0 || slot == playerSlot {
			continue
		}
		if index == len(partners) {
			return errors.New("selected party is missing an enabled battle slot")
		}
		partner := partners[index]
		if partner.UserID <= 0 || partner.ArthurType < 1 || partner.ArthurType > 4 ||
			(required != 0 && required != int(partner.ArthurType)) {
			return errors.New("partner profession differs from its battle slot")
		}
		if (rules.OnlyMyDeck != 0 || filters[slot] == 1) && !partner.IsSelf {
			return errors.New("this battle slot requires the player's own deck")
		}
		if filters[slot] == 2 && partner.IsSelf {
			return errors.New("this battle slot requires a borrowed deck")
		}
		index++
	}
	if index != len(partners) {
		return errors.New("selected party contains a disabled battle slot")
	}
	return nil
}

func (rules TeamBattleEntryRules) AllowsMultiplayer() bool {
	// DeckTeamSt.OnCreateRm/OnEnterRm reject own-deck-only entries.
	return rules.OnlyMyDeck == 0 &&
		rules.StartRule != 1 && rules.StartRule != 2
}

func (rules TeamBattleEntryRules) AllowsSolo() bool {
	// DeckTeamSt.SetNotification/OnSolo disable solo for MULTI_ONLY.
	return rules.StartRule != 3
}

func (s *Account) TeamBattleEntryRulesForBoss(bossID int) (TeamBattleEntryRules, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	catalog := s.teamBattleSolo
	return s.teamBattleEntryRulesForBossLocked(bossID, catalog)
}

func (s *Account) teamBattleEntryRulesForBossLocked(bossID int, catalog *battleCatalogState) (TeamBattleEntryRules, bool) {
	if s.battlePublicationDisabledBossesLocked()[bossID] {
		return TeamBattleEntryRules{}, false
	}
	if rules, found := catalog.entryRules(bossID); found {
		return rules, true
	}
	if tower, floor, found := s.towerQuestBattleForBossLocked(bossID); found {
		var rules TeamBattleEntryRules
		if json.Unmarshal(floor.Boss, &rules) == nil && rules.BossID == bossID {
			rules.StageType = tower.StageType
			return rules, true
		}
	}
	return TeamBattleEntryRules{}, false
}
