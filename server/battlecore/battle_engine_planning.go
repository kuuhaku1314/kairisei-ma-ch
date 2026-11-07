package battlecore

import (
	"fmt"
	"sort"
)

// PreviewEnemyPlans uses the original scheduling functions. It deliberately does
// not execute skills or invent player damage: supplied HP/flags are test input.
// Diagnostic probes copy the RNG and never consume the real schedule's draws.
func PreviewEnemyPlans(catalog *CombatCatalog, partyID int, seed uint32, snapshots []EnemyPlanSnapshot) ([]EnemyTurnPlan, error) {
	if catalog == nil {
		return nil, fmt.Errorf("missing planning catalog")
	}
	party, ok := catalog.EnemyParties[partyID]
	if !ok || len(snapshots) == 0 || len(snapshots) > 30 {
		return nil, fmt.Errorf("invalid workshop party or snapshot count")
	}
	engine := &BattleEngine{catalog: catalog, seed: seed, rng: newXorShift128(seed), enemyUses: map[int]int{}}
	if err := engine.loadEnemyParty(party, nil); err != nil {
		return nil, err
	}
	for i := range engine.players {
		engine.players[i] = battlePlayer{MemberType: i + 1, ArthurType: i + 1, HP: 30000, MaxHP: 30000, BaseMaxHP: 30000}
	}
	var result []EnemyTurnPlan
	for _, snap := range snapshots {
		if snap.Turn < 0 || snap.Turn > 999 {
			return nil, fmt.Errorf("turn outside 0..999")
		}
		engine.turn = snap.Turn
		for i := 0; i < engine.enemyCount; i++ {
			if snap.HP[i] < 0 || snap.HP[i] > 100 {
				return nil, fmt.Errorf("HP outside 0..100")
			}
			e := &engine.enemies[i]
			e.HP = int(int64(e.MaxHP) * int64(snap.HP[i]) / 100)
			e.AIFlags = snap.Flags[i]
			e.AIVariables = snap.Variables[i]
			e.AITurn = battleEnemyAITurnStats{}
			e.AITurn.Damage = snap.Damage[i]
			e.ActionConsumed = 0
			e.ChargedActionCount = 0
		}
		trace := EnemyTurnPlan{Turn: snap.Turn, Checks: []EnemyActionPlanTrace{}, Order: []EnemyActionPlanTrace{}}
		usesBefore := map[int]int{}
		for k, v := range engine.enemyUses {
			usesBefore[k] = v
		}
		// Native charge-start visits all owners before ordinary action planning.
		if snap.Turn > 0 {
			for i := 0; i < engine.enemyCount; i++ {
				if engine.enemies[i].HP > 0 {
					engine.buildEnemyChargeStartPlan(&engine.enemies[i])
				}
			}
		}
		ownerPlans := [][]EnemyActionPlanTrace{}
		for i := 0; i < engine.enemyCount; i++ {
			enemy := &engine.enemies[i]
			makeTrace := func(a CombatEnemyAction) EnemyActionPlanTrace {
				return EnemyActionPlanTrace{Actor: i, EnemyID: enemy.EnemyID, Slot: a.Slot, Skill: a.SkillID, Category: a.Category, Priority: a.Priority, Cost: a.ActionCost, MaxUses: a.MaxUses, Uses: usesBefore[enemyActionUseKey(enemy, a)]}
			}
			start := len(trace.Checks)
			for _, a := range enemy.Level.Actions {
				t := makeTrace(a)
				probe := *engine
				t.Condition = probe.enemyActionConditionSatisfied(enemy, a)
				switch {
				case enemy.HP <= 0:
					t.Reason = PlanBroken
				case a.Category == "death":
					t.Reason = PlanDeathOnly
				case snap.Turn == 0 && (a.Category != "skill" || (a.AIConditionID == 0 && a.Order == nil)):
					t.Reason = PlanNotPreemptive
				case a.MaxUses <= 0 || t.Uses >= a.MaxUses:
					t.Reason = PlanExhausted
				case !t.Condition:
					t.Reason = PlanConditionFalse
				case a.ActionCost > enemy.Level.ActionsPerTurn:
					t.Reason = PlanOverBudget
				default:
					t.Reason = PlanEligible
				}
				trace.Checks = append(trace.Checks, t)
			}
			var plan []enemyActionCandidate
			if enemy.HP > 0 {
				if snap.Turn == 0 {
					consumed := 0
					for _, a := range enemy.Level.Actions {
						if a.Category == "skill" && (a.AIConditionID != 0 || a.Order != nil) {
							if candidate, ok := engine.tryScheduleEnemyAction(enemy, a, &consumed); ok {
								plan = append(plan, candidate)
							}
						}
					}
					sortEnemyActionPlan(plan)
				} else {
					plan = engine.buildEnemyActionPlan(enemy)
				}
			}
			ownerPlan := []EnemyActionPlanTrace{}
			for _, selected := range plan {
				t := makeTrace(selected.action)
				t.Condition = true
				t.Selected = 1
				t.Reason = PlanSelected
				ownerPlan = append(ownerPlan, t)
				for j := start; j < len(trace.Checks); j++ {
					if trace.Checks[j].Slot == selected.action.Slot {
						trace.Checks[j].Selected++
						trace.Checks[j].Reason = PlanSelected
					}
				}
			}
			if len(ownerPlan) > 0 {
				ownerPlans = append(ownerPlans, ownerPlan)
			}
		}
		sort.SliceStable(ownerPlans, func(i, j int) bool {
			if ownerPlans[i][0].Priority == ownerPlans[j][0].Priority {
				return ownerPlans[i][0].Actor < ownerPlans[j][0].Actor
			}
			return ownerPlans[i][0].Priority < ownerPlans[j][0].Priority
		})
		for _, plan := range ownerPlans {
			trace.Order = append(trace.Order, plan...)
		}
		result = append(result, trace)
	}
	return result, nil
}
