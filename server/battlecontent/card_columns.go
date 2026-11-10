package battlecontent

import (
	"errors"

	"kairisei.local/server/battlemodel"
)

// ParseCard reads the unified 113-column card definition and its optional
// six display/development fields.
func ParseCard(row []string) (battlemodel.CombatCardDefinition, error) {
	switch len(row) {
	case 113, 119:
	case 114, 120:
		if row[len(row)-1] != "" {
			return battlemodel.CombatCardDefinition{}, errors.New("modern card has an unknown trailing field")
		}
	default:
		return battlemodel.CombatCardDefinition{}, errors.New("unsupported modern card column count")
	}
	id, err := RequiredInt(row, 0, "card ID")
	if err != nil {
		return battlemodel.CombatCardDefinition{}, err
	}
	card := battlemodel.CombatCardDefinition{
		ID: id, BaseCardID: OptionalInt(row, 1), Rarity: Field(row, 7),
		MaxLevel: OptionalInt(row, 35), LoveMax: OptionalInt(row, 36),
		NormalSkillID: OptionalInt(row, 38), ArthurSkillID: OptionalInt(row, 39),
		SupportSkillIDs: [4]int{OptionalInt(row, 40), OptionalInt(row, 41), OptionalInt(row, 42), OptionalInt(row, 43)},
		CallSkillID:     OptionalInt(row, 44), PassiveSkillID: OptionalInt(row, 45),
		ArthurPassiveSkillID: OptionalInt(row, 46), SummonID: OptionalInt(row, 49),
		HasParameterFamilies: true, RarityPlus: cardRarityPlus(Field(row, 8)),
		HPBase: OptionalInt(row, 10), HPGrowth: OptionalInt(row, 11),
		AttackBase: OptionalInt(row, 13), AttackGrowth: OptionalInt(row, 14),
		MagicBase: OptionalInt(row, 16), MagicGrowth: OptionalInt(row, 17),
		RecoveryBase: OptionalInt(row, 19), RecoveryGrowth: OptionalInt(row, 20),
	}
	for parameter := 0; parameter < 4; parameter++ {
		for kind := 0; kind < 3; kind++ {
			card.AlchemistParameters[kind][parameter] = OptionalInt(row, 22+parameter*3+kind)
		}
	}
	for index := range card.ProfileTags {
		if tag := OptionalInt(row, 68+index); tag != -1 {
			card.ProfileTags[index] = tag
		}
	}
	return card, nil
}

// RARITYPLUS is a named enum at column8. Column34 is the independent
// limit_count; its numeric value does not describe the card's rarity family.
func cardRarityPlus(value string) int {
	switch value {
	case "PURPLE":
		return 1
	case "WHITE":
		return 2
	case "RED":
		return 3
	case "GREEN":
		return 4
	default:
		return 0
	}
}
