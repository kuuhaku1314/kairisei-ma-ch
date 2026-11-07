package battlecore

import "fmt"

func (engine *BattleEngine) loadEncounter(input Encounter) error {
	var enemies [4]battleEnemy
	count := 0
	for index, value := range input.Enemies {
		def, level := value.Definition, value.Level
		if def.ID == 0 {
			continue
		}
		if def.ID < 0 || value.Parent < 0 || value.Parent > 4 || value.Parent == index+1 {
			return fmt.Errorf("invalid direct enemy identity/parent in slot %d", index+1)
		}
		if err := validateEnemyGauge(def.ReactiveGauge); err != nil {
			return fmt.Errorf("enemy %d: %w", def.ID, err)
		}
		if len(def.TribeIDs) > 3 {
			return fmt.Errorf("enemy %d has more than three tribe slots", def.ID)
		}
		tribes := append([]int(nil), def.TribeIDs...)
		if len(tribes) == 0 {
			tribes = []int{0} // Empty legacy definitions represented the null tribe.
		}
		level.Actions = append([]CombatEnemyAction(nil), level.Actions...)
		for i := range level.Actions {
			level.Actions[i] = copyEnemyAction(level.Actions[i])
		}
		for i := range level.GaugeActions {
			if level.GaugeActions[i].SkillID != 0 && def.ReactiveGauge == nil {
				return fmt.Errorf("enemy %d has a transition skill without a reactive gauge", def.ID)
			}
			level.GaugeActions[i] = copyEnemyAction(level.GaugeActions[i])
		}
		enemies[index] = battleEnemy{
			Trance: newEnemyTrance(def), MemberType: 5 + index, EnemyID: def.ID, TribeIDs: tribes, Parent: value.Parent,
			HP: value.HP, MaxHP: value.MaxHP, BaseMaxHP: value.MaxHP,
			Attack: def.Attack, BaseAttack: def.Attack, Magic: def.Magic, BaseMagic: def.Magic,
			Recovery: def.Recovery, BaseRecovery: def.Recovery, Defense: def.Defense, BaseDefense: def.Defense,
			MDefense: def.MagicDefense, BaseMDefense: def.MagicDefense, DamageReduction: def.DamageReduction,
			AttributeFixed: def.AttributeFixed, LimitAttack: initialParameterLimit(engine.alchemist), LimitMagic: initialParameterLimit(engine.alchemist), LimitRecovery: initialParameterLimit(engine.alchemist),
			BaseAttribute: def.Attribute, Attribute: def.Attribute, Level: level, Awake: level.InitialAwake,
		}
		for _, drop := range value.Drops {
			if drop.EnemyIndex != index || !ValidBattleDrop(drop) {
				return fmt.Errorf("invalid direct enemy drop in slot %d", index+1)
			}
			enemies[index].Drops = append(enemies[index].Drops, drop)
		}
		count = index + 1
	}
	if count == 0 {
		return fmt.Errorf("direct encounter contains no enemies")
	}
	for _, enemy := range enemies {
		if enemy.Parent != 0 && enemies[enemy.Parent-1].EnemyID == 0 {
			return fmt.Errorf("direct enemy parent is absent")
		}
	}
	engine.enemies, engine.enemyCount = enemies, count
	return nil
}

func copyEnemyAction(action CombatEnemyAction) CombatEnemyAction {
	if action.Order != nil {
		order := *action.Order
		order.Fields = append([]string(nil), order.Fields...)
		action.Order = &order
	}
	return action
}
