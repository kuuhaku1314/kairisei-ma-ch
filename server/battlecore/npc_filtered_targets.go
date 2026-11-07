package battlecore

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"
)

// c69b4 sees committed main cards, including previous NPCs in this phase.
// It does not include spheres or tentative choices from the current pass.
func (engine *BattleEngine) npcSelectedFeatures(owner int) npcFeatures {
	var features npcFeatures
	for _, p := range engine.playerParty(owner) {
		member := p.MemberType
		for _, action := range engine.turnActions {
			if action.memberType != member || action.cardType < 1 || action.cardType > 10 || action.sphereSlot != 0 {
				continue
			}
			branch, _ := engine.selectCombatSkillVariantsMode(engine.catalog.Skills[action.skill.ID], action, engine.turnActions, battleActionChainCounts(engine.partyActions(engine.turnActions, owner)), false)
			if branch.ID != 0 {
				npcAccumulateFeatures(action.skill, engine.catalog.SkillRoles[branch.FunctionID], &features)
			}
		}
	}
	return features
}

func (engine *BattleEngine) npcHasStatusCode(member int, name string, normalOnly bool) bool {
	if name == "DARKNESS" {
		name = "DARKNESS_APPOINT"
	}
	// Status list identity matters: 44a72 reads normal effects only; 469d8
	// enumerates all seven lists. Presence is not a duration/rate query.
	code, ok := battleBuffCodes[name]
	if !ok {
		code, ok = map[string]int{"UNDERMINE": 300, "SILENCE": 302, "CHARM": 303, "CARD_TRAP_DOT": 310}[name]
	}
	if !ok {
		return false
	}
	for _, effect := range engine.battleMemberEffects(member) {
		if effect.ListType < 0 || effect.ListType > 6 || normalOnly && effect.ListType != 0 {
			continue
		}
		if current, exists := battleBuffCodes[effect.Function]; exists && current == code || effect.Function == name {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) npcHasCurse(member int, attribute string) bool {
	if !engine.playerMember(member) {
		return false
	}
	for _, hold := range engine.player(member).BlessHolds {
		if hold.CardType == 21 && hold.Skill.ID != 0 && damageAttributeMatches(attribute, hold.Skill.Attribute) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) npcSelectorPrecondition(player *battlePlayer, rule npcSelectorRule, state *npcDecisionState) bool {
	if rule.allyFeature > 0 {
		for _, mask := range state.allyFeatures[rule.allyFeature : rule.allyFeature+3] {
			if mask != 0 {
				return false
			}
		}
	}
	switch rule.policy {
	case "cureSelf":
		return engine.npcHasStatusCode(player.MemberType, rule.parameter, false)
	case "curseSelf":
		return engine.npcHasCurse(player.MemberType, rule.parameter)
	case "cureAll":
		for _, p := range engine.playerParty(player.MemberType) {
			if p.MemberType != 0 && p.HP > 0 && !engine.npcHasStatusCode(p.MemberType, rule.parameter, false) {
				return false
			}
		}
	case "curseAll":
		for _, p := range engine.playerParty(player.MemberType) {
			if p.MemberType != 0 && p.HP > 0 && engine.npcHasCurse(p.MemberType, rule.parameter) {
				return true
			}
		}
		return false
	}
	return true
}

func (engine *BattleEngine) npcRandomMember(members []int) (int, bool, error) {
	if len(members) == 0 {
		return 0, false, nil
	}
	return members[int(engine.rng.next()%uint32(len(members)))], true, nil
}

func (engine *BattleEngine) npcMetric(member int, parameter string) int {
	if engine.playerMember(member) {
		return combatStatValue(engine.player(member), parameter)
	}
	return combatStatValue(playerFromEnemy(&engine.enemies[member-5]), parameter)
}

func (engine *BattleEngine) npcHasDOT(member int, parameter string) bool {
	if parameter != "" {
		return engine.npcHasStatusCode(member, parameter, false)
	}
	for _, name := range []string{"POISON", "BURN", "FREEZE", "BLEED", "ELECTRIC", "CARD_TRAP_DOT"} {
		if engine.npcHasStatusCode(member, name, false) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) npcMostAffected(player *battlePlayer, kind string) int {
	switch kind {
	case "DARKNESS":
		return bits.OnesCount32(uint32(playerDarknessMask(player)) & 31)
	case "COST_BLOCK":
		value := 0
		for _, effect := range player.Effects {
			if effect.Function == "COST_BLOCK" && effect.ListType >= 0 && effect.ListType <= 6 {
				value += effect.Value
			}
		}
		return value
	case "CARD_SEAL", "CARD_TRAP_DAMAGE":
		count := 0
		for _, slot := range player.Hand {
			if slot < 1 {
				continue
			}
			var found bool
			if kind == "CARD_SEAL" {
				_, found = playerCardSealEffect(player, player.handSlotCard(slot).CardType)
			} else {
				_, found = playerCardTrapEffect(player, player.handSlotCard(slot).CardType)
			}
			if found {
				count++
			}
		}
		return count
	}
	return 0
}

func (engine *BattleEngine) npcFilteredTarget(player *battlePlayer, choice npcCandidate, rule npcSelectorRule) (int, bool, error) {
	policy := rule.policy
	if policy == "cureSelf" || policy == "curseSelf" || policy == "cureAll" {
		if choice.skill.Target == "ENEMY_ONE" || choice.skill.Target == "ENEMY_ALL" {
			return engine.randomLivingTarget(player.MemberType, true)
		}
		if policy == "cureAll" {
			return engine.npcLivingDefaultTarget(player.MemberType, choice.skill.Target)
		}
		return player.MemberType, true, nil
	}
	if policy == "curseAll" {
		return engine.npcLivingDefaultTarget(player.MemberType, choice.skill.Target)
	}
	if policy == "friendJob" {
		var candidates []int
		for _, p := range engine.playerParty(player.MemberType) {
			if p.MemberType != 0 && p.HP > 0 && p.ArthurType == combatArthurType(rule.parameter) {
				candidates = append(candidates, p.MemberType)
			}
		}
		// JP6dfe8/6e1f0 retain living matching Arthurs; c4ae9 draws even
		// for one result. Do not replace that selection with the first match.
		return engine.npcRandomMember(candidates)
	}
	members := engine.livingSideMembers(player.MemberType, strings.HasPrefix(policy, "enemy"), rule.parts)
	if strings.HasPrefix(policy, "enemyWeakness") {
		first := 0
		for _, member := range members {
			if engine.npcHasStatusCode(member, "WEAKNESS", true) {
				first = member
				break
			}
		}
		if first == 0 {
			return 0, false, nil
		}
		// The original does not retry a second weakened target when the first
		// one fails the additional DOT/already-applied predicate.
		members = []int{first}
	}
	filtered := members[:0]
	for _, member := range members {
		keep := true
		switch policy {
		case "enemyMissing", "enemyWeaknessMissing":
			keep = !engine.npcHasStatusCode(member, rule.parameter, true)
		case "enemyDOT", "enemyWeaknessDOT":
			keep = engine.npcHasDOT(member, rule.parameter)
		case "cureRandom":
			keep = engine.npcHasStatusCode(member, rule.parameter, true)
		case "curseRandom":
			keep = engine.npcHasCurse(member, rule.parameter)
		case "friendLowRate":
			p := engine.player(member)
			keep = p.MaxHP > 0 && p.HP < p.MaxHP
		}
		if keep {
			filtered = append(filtered, member)
		}
	}
	members = filtered
	if len(members) == 0 {
		return 0, false, nil
	}
	switch policy {
	case "enemyWeakness", "enemyWeaknessMissing", "enemyWeaknessDOT":
		return members[0], true, nil
	case "enemyMissing", "enemyDOT", "cureRandom", "curseRandom":
		return engine.npcRandomMember(members)
	case "enemyHigh", "friendHigh", "friendLowHP":
		parameter := rule.parameter
		if policy == "friendLowHP" {
			parameter = "HP"
		}
		sort.Slice(members, func(i, j int) bool {
			a, b := engine.npcMetric(members[i], parameter), engine.npcMetric(members[j], parameter)
			if a == b {
				return members[i] < members[j]
			}
			return a < b
		})
		if policy == "friendLowHP" {
			return members[0], true, nil
		}
		return members[len(members)-1], true, nil
	case "friendLowRate":
		target, best := 0, int64(0x7fffffff)
		for _, member := range members {
			p := engine.player(member)
			value := int64(p.HP) * 100 / int64(p.MaxHP)
			if value < best {
				target, best = member, value
			}
		}
		return target, target != 0, nil
	case "cureMost":
		target, best := 0, 0
		for _, member := range members {
			value := engine.npcMostAffected(engine.player(member), rule.parameter)
			if value > best {
				target, best = member, value
			}
		}
		return target, target != 0, nil
	case "enemyAttributeDefense":
		attributes := splitCombatAttributes(choice.skill.Attribute)
		attribute := "NULL"
		if len(attributes) > 0 {
			attribute = attributes[0]
		}
		bestRate, bestFixed := 0, int(0x7fffffff)
		var selected []int
		for _, member := range members {
			rate, fixed := engine.npcAttributeRate(member, attribute), engine.npcFixedAttributeDefense(member, attribute)
			if rate > bestRate || rate == bestRate && fixed < bestFixed {
				bestRate, bestFixed = rate, fixed
				selected = nil
			}
			if rate == bestRate && fixed == bestFixed {
				selected = append(selected, member)
			}
		}
		return engine.npcRandomMember(selected)
	}
	return 0, false, fmt.Errorf("NPC filtered target %s is not connected", policy)
}

func (engine *BattleEngine) npcAttributeAttack(player *battlePlayer, groups [2][]npcCandidate, parts bool) (npcCandidate, bool, error) {
	// JP selector13 at bd1cd queries6de09, not the side-aware player list.
	// Consequently it has no target in PVP; it must not advance rule priority.
	members := engine.livingMonsterMembers(parts)
	best := 100
	var selected npcCandidate
	for _, group := range groups {
		for _, choice := range group {
			for _, member := range members {
				rate := engine.npcAttributeRate(member, choice.skill.Attribute)
				if rate > best {
					best = rate
					selected = choice
					selected.target = member
				}
			}
		}
	}
	if selected.target == 0 {
		return npcCandidate{}, false, nil
	}
	selected.target = engine.npcRedirectTarget(player.MemberType, selected.target)
	return selected, true, nil
}
