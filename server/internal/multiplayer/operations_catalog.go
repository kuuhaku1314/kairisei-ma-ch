package multiplayer

import (
	"path/filepath"

	"kairisei.local/server/battlecontent"
)

// LoadOperationsEnemyCatalog reuses the combat parsers without loading skills
// or building an engine. Editors must offer real occupied slots, not guess that
// every enemy party has four targets.
func LoadOperationsEnemyCatalog(root string) (map[int]CombatEnemyParty, map[int]CombatEnemyDefinition, error) {
	parties := make(map[int]CombatEnemyParty)
	enemies := make(map[int]CombatEnemyDefinition)
	err := battlecontent.ReadCSV(filepath.Join(root, "enemy_party.csv"), func(row []string) error {
		v, err := battlecontent.ParseEnemyParty(row)
		if err == nil {
			parties[v.ID] = v
		}
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	err = battlecontent.ReadCSV(filepath.Join(root, "enemy.csv"), func(row []string) error {
		v, err := battlecontent.ParseEnemy(row)
		if err == nil {
			enemies[v.ID] = v
		}
		return err
	})
	return parties, enemies, err
}

type OperationsCardInfo struct {
	Prefix      string `json:"prefix,omitempty"`
	LimitCount  int    `json:"limit_count,omitempty"`
	Attribute   string `json:"attribute"`
	Cost        int    `json:"cost"`
	NormalSkill string `json:"normal_skill"`
	ArthurSkill string `json:"arthur_skill"`
	SummonID    int    `json:"-"`
}

func LoadOperationsCardCatalog(cardPath, battleRoot string, alchemistPaths ...string) (map[int]OperationsCardInfo, error) {
	skills := make(map[int]CombatSkillDefinition)
	skillTables := []string{"skill_player.csv"}
	if len(alchemistPaths) > 0 {
		skillTables = append(skillTables, "skill_player_alchemist.csv")
	}
	for _, name := range skillTables {
		if err := battlecontent.ReadCSV(filepath.Join(battleRoot, name), func(row []string) error {
			skill, err := battlecontent.ParseSkill(row)
			if _, exists := skills[skill.ID]; err == nil && !exists {
				skills[skill.ID] = skill
			}
			return err
		}); err != nil {
			return nil, err
		}
	}
	result := make(map[int]OperationsCardInfo)
	for _, path := range append([]string{cardPath}, alchemistPaths...) {
		err := battlecontent.ReadCardCSV(path, func(row []string) error {
			card, err := battlecontent.ParseCard(row)
			if err != nil {
				return err
			}
			presentation, err := battlecontent.ParseCardPresentation(row)
			if err != nil {
				return err
			}
			normal, arthur := skills[card.NormalSkillID], skills[card.ArthurSkillID]
			result[card.ID] = OperationsCardInfo{Prefix: presentation.Prefix, LimitCount: presentation.LimitCount, Attribute: normal.Attribute, Cost: normal.Cost, NormalSkill: normal.Name, ArthurSkill: arthur.Name, SummonID: card.SummonID}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
