package battlecore

import (
	"fmt"
	"math"
	"slices"
	"strconv"
)

func copyEnemyGauge(source *EnemyGaugeDefinition) *EnemyGaugeDefinition {
	if source == nil {
		return nil
	}
	value := *source
	value.Reactions = append([]EnemyGaugeReaction(nil), source.Reactions...)
	for i := range value.Reactions {
		value.Reactions[i].Parameters = append([]string(nil), value.Reactions[i].Parameters...)
		value.Reactions[i].Targets = append([]string(nil), value.Reactions[i].Targets...)
	}
	return &value
}

func validateEnemyGauge(value *EnemyGaugeDefinition) error {
	if value == nil {
		return nil
	}
	if value.InitialState < 0 || value.InitialState > 3 || value.InitialRate < math.MinInt32 || value.InitialRate > math.MaxInt32 || len(value.Reactions) > 50 {
		return fmt.Errorf("invalid enemy gauge initialization")
	}
	for _, reaction := range value.Reactions {
		if len(reaction.Parameters) > 8 || len(reaction.Targets) > 5 || reaction.NormalRate < math.MinInt32 || reaction.NormalRate > math.MaxInt32 || reaction.TranceRate < math.MinInt32 || reaction.TranceRate > math.MaxInt32 {
			return fmt.Errorf("enemy gauge reaction exceeds its source bounds")
		}
		switch reaction.Kind {
		case "SKILL_KIND", "COST_FIXED", "COST_RANGE", "ATTR", "ARTHUR_TYPE", "PLAY_TAG", "BURST_SKILL", "NONE":
		default:
			return fmt.Errorf("unsupported enemy gauge reaction %q", reaction.Kind)
		}
		for _, target := range reaction.Targets {
			switch target {
			case "NONE", "ALL", "CARD", "HOLD_CARD", "CURSE", "BLESS", "SPHR", "TOKEN":
			default:
				return fmt.Errorf("unsupported enemy gauge reaction target %q", target)
			}
		}
		for _, parameter := range reaction.Parameters {
			switch reaction.Kind {
			case "COST_FIXED", "COST_RANGE", "PLAY_TAG":
				if _, err := strconv.ParseInt(parameter, 10, 32); err != nil {
					return fmt.Errorf("invalid %s gauge parameter %q", reaction.Kind, parameter)
				}
			case "SKILL_KIND":
				if !slices.Contains([]string{"NULL", "ATTACK", "SORCERY", "RECOVERY", "SUPPORT", "DEFENSE", "JAMMING", "SPECIAL"}, parameter) {
					return fmt.Errorf("invalid gauge skill kind %q", parameter)
				}
			case "ATTR":
				if parameter != "NULL" {
					for _, component := range splitCombatAttributes(parameter) {
						if combatAttributeCode(component) == 0 {
							return fmt.Errorf("invalid gauge attribute %q", parameter)
						}
					}
				}
			case "ARTHUR_TYPE":
				// Native fixed slots can retain -1 inside the valid-count
				// prefix when a later slot is populated. It matches no job.
				if !slices.Contains([]string{"-1", "NULL", "MERCENARY", "MILLIONAIRE", "THIEF", "SINGER", "ALCHEMIST"}, parameter) {
					return fmt.Errorf("invalid gauge profession %q", parameter)
				}
			}
		}
	}
	return nil
}

// The reactive contract checks fullness after both ordinary and reserved
// actions on that side. NORMAL fills; TRANCE starts full and depletes.
func (engine *BattleEngine) advanceReactiveTranceStates() ([]BattleResult, error) {
	var results []BattleResult
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		t := &enemy.Trance
		if engine.endType != 0 || t.Reactive == nil || enemy.HP <= 0 || t.limit() <= 0 ||
			!(t.State == 1 && t.Value >= t.NormalLimit || t.State == 2 && t.Value <= 0) {
			continue
		}
		results = append(results, BattleResult{Command: 501, Args: []int64{int64(enemy.MemberType)}})
		results = append(results, changeEnemyTrance(enemy, t.State+1)...)
		rows, err := engine.executeGaugeStateSkill(enemy)
		if err != nil {
			return nil, err
		}
		results = append(results, rows...)
	}
	return results, nil
}

func (engine *BattleEngine) executeGaugeStateSkill(enemy *battleEnemy) ([]BattleResult, error) {
	if enemy.Trance.Reactive == nil || enemy.Trance.State < 1 || enemy.Trance.State > 3 {
		return nil, nil
	}
	action := enemy.Level.GaugeActions[enemy.Trance.State-1]
	if action.SkillID == 0 {
		return nil, nil
	}
	return engine.executeEnemyActionCandidate(enemy, enemyActionCandidate{action: action})
}

func (engine *BattleEngine) reactiveGaugeSkillStart(action battleAction, origin string) []BattleResult {
	if !action.helper && (action.memberType < 1 || action.memberType > 8 ||
		(action.memberType > 4 && origin != "CURSE" && origin != "BLESS")) {
		return nil // Enemy-owned append cards react when their player holder executes them.
	}
	var results []BattleResult
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		t := &enemy.Trance
		if enemy.HP <= 0 || t.State == 3 || t.State == 2 && t.Value <= 0 {
			continue
		}
		// Native still publishes the retained value when no reactions exist,
		// including a disabled gauge's zero value.
		var reactions []EnemyGaugeReaction
		if t.Reactive != nil {
			reactions = t.Reactive.Reactions
		}
		var rate int32
		for _, reaction := range reactions {
			if (slices.Contains(reaction.Targets, "ALL") || slices.Contains(reaction.Targets, origin)) && engine.gaugeReactionMatches(reaction, action) {
				if t.State == 1 {
					rate += int32(reaction.NormalRate)
				} else if t.State == 2 {
					rate -= int32(reaction.TranceRate)
				}
			}
		}
		limit, maximum := int32(t.limit()), int32(t.limit())
		if t.State == 1 {
			maximum *= 3
		}
		t.Value = int(max(int32(0), min(maximum, int32(t.Value)+limit*rate/100)))
		results = append(results, BattleResult{Command: 88, Args: []int64{int64(enemy.MemberType), int64(t.Value)}})
	}
	return results
}

func (engine *BattleEngine) gaugeReactionMatches(reaction EnemyGaugeReaction, action battleAction) bool {
	if reaction.Kind == "NONE" {
		return true
	}
	if reaction.Kind == "BURST_SKILL" {
		// Original JP reacts to the active Burst itself, independently of
		// parameter slots. A subsequently modified card is still a card.
		return action.burst
	}
	if reaction.Kind == "COST_RANGE" {
		return len(reaction.Parameters) >= 2 && action.skill.Cost >= combatParameterInt(reaction.Parameters[0]) && action.skill.Cost <= combatParameterInt(reaction.Parameters[1])
	}
	for _, parameter := range reaction.Parameters {
		switch reaction.Kind {
		case "SKILL_KIND":
			if parameter == action.skill.Kind {
				return true
			}
		case "COST_FIXED":
			if combatParameterInt(parameter) == action.skill.Cost {
				return true
			}
		case "ATTR":
			// Unlike damage filters, NULL has no attribute bit and is not a
			// wildcard here, even against an untyped skill.
			if parameter != "NULL" && damageAttributeMatches(parameter, action.skill.Attribute) {
				return true
			}
		case "ARTHUR_TYPE":
			// Native tests compare the card's profession, not its caster's.
			if parameter == action.skill.Job {
				return true
			}
		case "PLAY_TAG":
			if combatCardHasProfileTag(engine.catalog.Cards[action.cardID], combatParameterInt(parameter)) {
				return true
			}
		}
	}
	return false
}
