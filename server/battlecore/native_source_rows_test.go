package battlecore

import (
	"errors"
	"kairisei.local/server/battlecontent"
)

// Captured native calibration receipts retain the original source columns.
// Only tests read these immutable receipts; live content uses the unified tables.
func parseNativeSourceCard(row []string) (CombatCardDefinition, error) {
	if len(row) < 28 {
		return CombatCardDefinition{}, errors.New("card row is too short")
	}
	id, err := battlecontent.RequiredInt(row, 0, "card ID")
	if err != nil {
		return CombatCardDefinition{}, err
	}
	card := CombatCardDefinition{
		ID: id, BaseCardID: battlecontent.OptionalInt(row, 1), Rarity: battlecontent.Field(row, 7),
		MaxLevel: battlecontent.OptionalInt(row, 23), NormalSkillID: battlecontent.OptionalInt(row, 26),
		ArthurSkillID: battlecontent.OptionalInt(row, 27), CallSkillID: battlecontent.OptionalInt(row, 32), PassiveSkillID: battlecontent.OptionalInt(row, 33),
		LoveMax:         battlecontent.OptionalInt(row, 24),
		SupportSkillIDs: [4]int{battlecontent.OptionalInt(row, 28), battlecontent.OptionalInt(row, 29), battlecontent.OptionalInt(row, 30), battlecontent.OptionalInt(row, 31)},
		HPBase:          battlecontent.OptionalInt(row, 10),
		HPGrowth:        battlecontent.OptionalInt(row, 11), AttackBase: battlecontent.OptionalInt(row, 13),
		AttackGrowth: battlecontent.OptionalInt(row, 14), MagicBase: battlecontent.OptionalInt(row, 16),
		MagicGrowth: battlecontent.OptionalInt(row, 17), RecoveryBase: battlecontent.OptionalInt(row, 19),
		RecoveryGrowth: battlecontent.OptionalInt(row, 20),
	}
	for index := range card.ProfileTags {
		if tag := battlecontent.OptionalInt(row, 54+index); tag != -1 {
			card.ProfileTags[index] = tag
		}
	}
	return card, nil
}

func parseNativeSourceEnemy(row []string) (CombatEnemyDefinition, error) {
	if len(row) < 25 {
		return CombatEnemyDefinition{}, errors.New("enemy row is too short")
	}
	id, err := battlecontent.RequiredInt(row, 0, "enemy ID")
	if err != nil {
		return CombatEnemyDefinition{}, err
	}
	tribes := []int{battlecontent.OptionalInt(row, 1)}
	return CombatEnemyDefinition{
		ID: id, TribeIDs: tribes, ModelID: battlecontent.Field(row, 2),
		ImageID: battlecontent.OptionalInt(row, 4), Live2DName: battlecontent.Field(row, 5), Name: battlecontent.Field(row, 6),
		Attribute: battlecontent.Field(row, 7), HP: battlecontent.OptionalInt(row, 8), Attack: battlecontent.OptionalInt(row, 9),
		Magic: battlecontent.OptionalInt(row, 10), Recovery: battlecontent.OptionalInt(row, 11),
		Defense: battlecontent.OptionalInt(row, 12), MagicDefense: battlecontent.OptionalInt(row, 13),
		DamageReduction: battlecontent.OptionalInt(row, 14), Size: battlecontent.Field(row, 24),
		AttributeFixed: [5]int{battlecontent.OptionalInt(row, 15), battlecontent.OptionalInt(row, 16), battlecontent.OptionalInt(row, 17), battlecontent.OptionalInt(row, 18), battlecontent.OptionalInt(row, 19)},
		TranceLimit:    battlecontent.OptionalInt(row, 20), OverheatLimit: battlecontent.OptionalInt(row, 21),
		OverheatTurns: battlecontent.OptionalInt(row, 22), OverheatResist: battlecontent.OptionalInt(row, 23),
	}, nil
}

func parseNativeSourceEnemyLevel(row []string) (CombatEnemyLevel, error) {
	if len(row) < 313 {
		return CombatEnemyLevel{}, errors.New("source enemy level row is too short")
	}
	// Original captures predate the three gauge-transition action slots.
	current := make([]string, len(row)+21)
	copy(current, row[:30])
	copy(current[51:], row[30:])
	return battlecontent.ParseEnemyLevel(current)
}
