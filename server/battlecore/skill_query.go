package battlecore

import "fmt"

// SkillBranchIndex is the original standalone branch query, not a hand-card
// preview: it has no target, chain or owning card. It observes committed
// selections but neither permits RANDOM branches nor advances the instance RNG.
// The returned index addresses the caller's variants, including any empty slots.
func (engine *BattleEngine) SkillBranchIndex(member int, variants []CombatSkillDefinition) (int, error) {
	if !engine.playerMember(member) {
		return -1, fmt.Errorf("skill query requires a player member")
	}
	if engine.player(member).MemberType == 0 {
		return -1, nil
	}
	rng := engine.rng
	defer func() { engine.rng = rng }()
	action := battleAction{memberType: member}
	// Preserve sparse wire slots while reusing the execution branch selector.
	var filtered []CombatSkillDefinition
	var indices []int
	for i, variant := range variants {
		if variant.FunctionID != 0 {
			filtered = append(filtered, variant)
			indices = append(indices, i)
		}
	}
	selected, index := engine.selectCombatSkillVariantsMode(filtered, action, engine.turnActions, nil, false)
	if selected.FunctionID == 0 {
		return -1, nil
	}
	return indices[index], nil
}

func (battle *StandaloneBattle) SkillBranchIndex(member int, variants []CombatSkillDefinition) (int, error) {
	return battle.engine.SkillBranchIndex(member, variants)
}

// PreparedSkillQuery observes typed domain inputs, without depending on a
// client's DTO layout. Unlike an executable party, it may be incomplete.
// No opening passives, shuffle, draw or phase transition is performed.
type PreparedSkillQuery struct{ engine *BattleEngine }

func (query *PreparedSkillQuery) SkillBranchIndex(member int, variants []CombatSkillDefinition) (int, error) {
	return query.engine.SkillBranchIndex(member, variants)
}

func NewPreparedSkillQuery(catalog *CombatCatalog, members []Participant, encounter *Encounter, pvp bool) (*PreparedSkillQuery, error) {
	if catalog == nil {
		return nil, fmt.Errorf("skill query requires content")
	}
	engine := &BattleEngine{catalog: catalog, pvp: pvp}
	if pvp {
		engine.opponents = new([4]battlePlayer)
	}
	for _, input := range members {
		if !engine.playerMember(input.MemberType) {
			return nil, fmt.Errorf("invalid query member")
		}
		p := engine.player(input.MemberType)
		if p.MemberType != 0 {
			return nil, fmt.Errorf("duplicate query member")
		}
		*p = battlePlayer{MemberType: input.MemberType, HP: input.HP, MaxHP: input.HP, BaseMaxHP: input.HP,
			Attack: input.Attack, BaseAttack: input.Attack, Magic: input.Magic, BaseMagic: input.Magic,
			Recovery: input.Mind, BaseRecovery: input.Mind, JobType: input.ArthurType, ArthurType: input.ArthurType,
			BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		if input.MaxHP != nil {
			p.MaxHP = *input.MaxHP
			p.BaseMaxHP = *input.MaxHP
		}
		if input.JobType != nil {
			p.JobType = *input.JobType
		}
		if input.InitialBurst != nil {
			p.Burst = *input.InitialBurst
		}
		for _, buddy := range input.DeckBuddies {
			if buddy.BuddyType == 1 && buddy.BuddyID != 0 {
				p.BurstState = burstGaugeNormal
			}
		}
		for _, card := range input.DeckCards {
			if card.CardType < 1 || card.CardType > 10 {
				return nil, fmt.Errorf("invalid query main-card slot")
			}
			p.Deck[card.CardType-1] = card
		}
		for _, card := range input.SupportCards {
			if card.CardType < 11 || card.CardType > 20 {
				return nil, fmt.Errorf("invalid query support-card slot")
			}
			p.SupportDeck[card.CardType-11] = card
		}
	}
	if encounter != nil {
		if pvp {
			return nil, fmt.Errorf("PVP player query cannot also register monster enemies")
		}
		if err := engine.loadEncounter(*encounter); err != nil {
			return nil, err
		}
	}
	return &PreparedSkillQuery{engine: engine}, nil
}
