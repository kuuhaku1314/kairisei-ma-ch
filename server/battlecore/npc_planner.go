package battlecore

import (
	"fmt"
	"sort"
	"strings"
)

type npcCandidate struct {
	card         BattleCard
	sphereSlot   int
	level        int
	skill        CombatSkillDefinition
	roles        []CombatSkillRole
	cost, target int
	power        int
	arthur       bool
	summon       bool
}

// SubmitNPC owns the original NPC decision's RNG and normal selection commit
// as one operation. Unsupported selectors fail without advancing live state.
func (engine *BattleEngine) SubmitNPC(member, enemyType int, program NPCProgram) ([]BattleResult, error) {
	var rows []BattleResult
	err := engine.WithCardSelections(func(batch *CardSelectionBatch) error {
		selection, err := batch.view.npcSubmission(member, enemyType, program)
		if err != nil {
			return err
		}
		rows, err = batch.Submit(member, selection)
		return err
	})
	return rows, err
}

func (engine *BattleEngine) npcSubmission(member, enemyType int, program NPCProgram) (cardPlaySubmission, error) {
	if engine.phase != PhaseUser || engine.continuePending || !engine.playerMember(member) {
		return cardPlaySubmission{}, fmt.Errorf("NPC selection is unavailable")
	}
	if err := engine.resetCardSubmission(member); err != nil {
		return cardPlaySubmission{}, err
	}
	return engine.planNPC(member, enemyType, program)
}

func (engine *BattleEngine) planNPC(member, enemyType int, program NPCProgram) (cardPlaySubmission, error) {
	player := engine.player(member)
	result := cardPlaySubmission{Automatic: true}
	if player.HP <= 0 {
		return result, fmt.Errorf("NPC selection on a dead member is not connected")
	}
	// JP3660c takes the fixed story input before consulting NPC rules.
	// It submits the lowest remaining deck slot in hand with target0; the ordinary
	// commit resolves that target and owns any resulting RNG consumption.
	if engine.deckShuffleOff {
		for _, slot := range player.Hand {
			if slot != 0 {
				cardType := player.handSlotCard(slot).CardType
				if result.CardTypes[0] == 0 || cardType < result.CardTypes[0] {
					result.CardTypes[0] = cardType
				}
			}
		}
		return result, nil
	}
	priorityIndex := player.JobType - 1
	if player.JobType == 0 {
		priorityIndex = 5
	}
	if priorityIndex < 0 || priorityIndex >= 6 {
		return result, fmt.Errorf("NPC job %d is unsupported", player.JobType)
	}
	rules := append([]NPCRule(nil), program.Rules...)
	sort.SliceStable(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	remaining := player.Cost
	used := [33]bool{}
	state := npcDecisionState{allyFeatures: engine.npcSelectedFeatures(member)}
	selected := 0
	sphereIndex := -1
	var selectedChoices [6]npcCandidate
	for selected < len(selectedChoices) {
		pool, err := engine.npcCardPool(player, remaining, used)
		if err != nil {
			return result, err
		}
		if sphereIndex < 0 {
			spheres, err := engine.npcSpherePool(player, remaining)
			if err != nil {
				return result, err
			}
			pool = append(pool, spheres...)
		}
		if len(pool) == 0 {
			break
		}
		priority := -1
		var choices []npcCandidate
		for _, rule := range rules {
			jobPriority := rule.Priority[priorityIndex]
			// JP b8924 skips zero priorities for ordinary actors; b8f2e's
			// summoned-knight path keeps them in the signed priority comparison.
			if rule.EnemyType != enemyType || jobPriority == 0 && player.SummonSource == 0 || jobPriority < priority {
				continue
			}
			allowed, err := engine.npcCondition(player, rule, &state)
			if err != nil {
				return result, err
			}
			if !allowed {
				continue
			}
			choice, ok, err := engine.npcRuleCandidate(player, rule, pool, &state)
			if err != nil {
				return result, fmt.Errorf("NPC rule %d: %w", rule.ID, err)
			}
			if !ok {
				continue
			}
			if jobPriority > priority {
				choices = nil
				priority = jobPriority
			}
			if len(choices) < 204 {
				choices = append(choices, choice)
			}
		}
		if len(choices) == 0 {
			if selected == 0 && (!engine.alchemist || player.SummonSource != 0) {
				fallback, ok, err := engine.npcFallback(player, remaining, used)
				if err != nil {
					return result, err
				}
				if ok {
					selectedChoices[0] = fallback
				}
			}
			break
		}
		sort.SliceStable(choices, func(i, j int) bool { return choices[i].cost > choices[j].cost })
		top := 1
		for top < len(choices) && choices[top].cost >= choices[0].cost {
			top++
		}
		choices = choices[:top]
		// Native b8bbe ->783c9 reads already committed actors. Unlike CALL
		// preview, it does not seed this NPC as a matching participant.
		var chained []npcCandidate
		for _, choice := range choices {
			if choice.card.CardType != 0 && battleActionChainCount(engine.partyActions(engine.turnActions, member), choice.skill.Attribute, 0) > 0 {
				chained = append(chained, choice)
			}
		}
		if len(chained) > 0 {
			choices = chained
		}
		choice := choices[int(engine.rng.next()%uint32(len(choices)))]
		selectedChoices[selected] = choice
		if choice.sphereSlot != 0 {
			sphereIndex = selected
		} else {
			used[choice.card.CardType] = true
		}
		remaining -= choice.cost
		selected++
		if choice.summon {
			state.summons++
			continue
		}
		// c6a79 reclassifies the chosen skill against committed actions,
		// without add4a's temporary one-card submission. It updates both the
		// own-choice buffer and the party buffer used by release suppression.
		action := battleAction{enemySkill: player.tokenUsesEnemySkill(choice.card.CardType), memberType: member, cardID: choice.card.CardID, cardType: choice.card.CardType, sphereSlot: choice.sphereSlot, cardLevel: choice.level, skill: choice.skill, roles: choice.roles}
		branch, _ := engine.selectCombatSkillVariantsMode(engine.catalog.Skills[action.skill.ID], action, engine.turnActions, engine.appendChainCounts(member, choice.skill), false)
		if branch.ID != 0 {
			roles := engine.catalog.SkillRoles[branch.FunctionID]
			npcAccumulateFeatures(choice.skill, roles, &state.ownFeatures)
			npcAccumulateFeatures(choice.skill, roles, &state.allyFeatures)
			if state.maxHate < choice.skill.HateRatio {
				state.maxHate = choice.skill.HateRatio
			}
		}
	}
	// 4110e normalizes a COPY for its input log, but passes the original six
	// choices to 40908. Preserve that result order instead of moving SPHR_PLAY.
	cardIndex := 0
	for _, choice := range selectedChoices {
		if choice.sphereSlot != 0 {
			result.SphereSlot, result.SphereTarget, result.SpherePosition = choice.sphereSlot, choice.target, cardIndex+1
		} else if choice.card.CardType != 0 {
			result.CardTypes[cardIndex], result.Targets[cardIndex] = choice.card.CardType, choice.target
			if choice.summon {
				result.SummonsCardMask |= 1 << cardIndex
			}
			cardIndex++
		}
	}
	return result, nil
}

func (engine *BattleEngine) npcCardPool(player *battlePlayer, remaining int, used [33]bool) ([]npcCandidate, error) {
	var result []npcCandidate
	for _, slot := range player.playableCardSlots() {
		if slot == 0 {
			continue
		}
		card := player.handSlotCard(slot)
		if used[card.CardType] {
			continue
		}
		if _, sealed := playerCardSealEffect(player, card.CardType); sealed {
			continue
		}
		skill, roles, err := engine.handCardSkill(player, card)
		if err != nil {
			return nil, err
		}
		cost := engine.effectiveCardCost(player, card.CardType, skill.Cost)
		if cost > remaining && player.SummonSource == 0 {
			continue
		}
		action := battleAction{enemySkill: player.tokenUsesEnemySkill(card.CardType), memberType: player.MemberType, cardID: card.CardID, cardType: card.CardType, cardLevel: card.ExecutionLevel(skill.ID), skill: skill, roles: roles}
		// add4a temporarily submits this card alone for the native branch
		// lookup. Earlier choices in this NPC pass only affect ownFeatures.
		actions := append(append([]battleAction(nil), engine.turnActions...), action)
		branch, _ := engine.selectCombatSkillVariantsMode(engine.catalog.Skills[action.skill.ID], action, actions, battleActionChainCounts(engine.partyActions(actions, player.MemberType)), false)
		if branch.ID != 0 {
			roles = engine.catalog.SkillRoles[branch.FunctionID]
		} else {
			roles = nil
		}
		view := *engine
		view.turnActions = actions
		display, err := view.cardDisplayStateMode(player, card, true, false)
		if err != nil {
			return nil, err
		}
		// JP dff1d stores effective cost in the candidate's fourth word;
		// b8b4e ranks that word and b8daa debits the same planning cost.
		summon := engine.alchemist && player.SummonSource == 0 && card.CardType <= len(engine.summons) && engine.summons[card.CardType-1].Input.ID != 0
		result = append(result, npcCandidate{card: card, level: card.ExecutionLevel(skill.ID), skill: skill, roles: roles, cost: cost, power: display.Power, arthur: engine.catalog.cardUsesArthurSkill(card.CardID, player.ArthurType), summon: summon})
	}
	return result, nil
}

func npcAttackFeatures(skill CombatSkillDefinition, roles []CombatSkillRole, combo bool) [6]uint32 {
	var features [6]uint32
	var options uint32
	for _, role := range roles {
		index := role.RoleIndex
		if index > 0 && combo {
			index--
		}
		bit := uint32(1) << uint(index&31)
		if role.Function == "ROLE_OP_COMBO" {
			combo = true
			continue
		}
		attack := role.Function == "ATTACK_AA" || role.Function == "ATTACK_AP" || role.Function == "ATTACK_PA" || role.Function == "ATTACK_PP"
		option := role.Function == "ATK_OP_DRAIN" || role.Function == "ATK_OP_REVENGE" || role.Function == "ATK_OP_PIERCING" || role.Function == "ATK_OP_DAMAGE_INCREASE" || role.Function == "ATK_OP_ATTR_RATE_DOWN_INVALID" || role.Function == "ATK_OP_DRAIN_ALL"
		if !attack && !option {
			continue
		}
		if option {
			options |= bit
		}
		single := skill.Target == "ENEMY_ONE" && role.Target == "SELECT"
		all := skill.Target == "ENEMY_ALL" && role.Target == "SELECT" || role.Target == "ENEMY_ALL"
		if single {
			features[0] |= bit | options
		}
		if all {
			features[1] |= bit | options
		}
		if attack {
			physics := role.Parameters[8]
			if physics == "PHYSICS" || physics == "ALL" {
				features[2] |= bit | options
				if single {
					features[4] |= bit | options
				}
			}
			if physics == "MAGIC" || physics == "ALL" {
				features[3] |= bit | options
				if single {
					features[5] |= bit | options
				}
			}
		}
	}
	return features
}

func (engine *BattleEngine) npcRuleCandidate(player *battlePlayer, rule NPCRule, pool []npcCandidate, state *npcDecisionState) (npcCandidate, bool, error) {
	feature := 0
	selector, auxiliary := npcAuxiliarySelectors[rule.Selector]
	if auxiliary {
		feature = selector.feature
		if !engine.npcSelectorPrecondition(player, selector, state) {
			return npcCandidate{}, false, nil
		}
	} else {
		switch rule.Selector {
		case "NULL":
			return npcCandidate{}, false, nil
		case "SUMMONS":
			choice, ok := engine.npcSummonCandidate(pool, state)
			return choice, ok, nil
		case "ATTACK_RANDOM", "ATTACK_LOW_HP_ENEMY", "ATTACK_HIGH_HP_ENEMY", "ATTACK_PARENT", "ATTACK_RANDOM_PARTS", "ATTACK_LOW_HP_PARTS", "ATTACK_HIGH_HP_PARTS", "ATTACK_WEAKNESS", "ATTACK_ENEMY_BACK":
		case "ATTACK_ALL":
			feature = 1
		case "ATTACK_LOW_DEF_ENEMY", "ATTACK_LOW_DEF_PARTS":
			feature = 4
		case "ATTACK_LOW_MDEF_ENEMY", "ATTACK_LOW_MDEF_PARTS":
			feature = 5
		default:
			return npcCandidate{}, false, fmt.Errorf("selector %s is not connected", rule.Selector)
		}
	}
	groups := npcCandidateGroups(pool, feature)
	if auxiliary && selector.policy == "attackAttribute" {
		return engine.npcAttributeAttack(player, groups, selector.parts)
	}
	items := groups[0]
	if len(items) == 0 {
		items = groups[1]
	}
	if len(items) == 0 {
		return npcCandidate{}, false, nil
	}
	choice := items[int(engine.rng.next()%uint32(len(items)))]
	var target int
	var ok bool
	var err error
	if auxiliary {
		target, ok, err = engine.npcAuxiliaryTarget(player, choice, selector)
	} else {
		target, ok, err = engine.npcAttackTarget(player, choice, rule.Selector)
	}
	if err != nil || !ok {
		return npcCandidate{}, false, err
	}
	choice.target = engine.npcRedirectTarget(player.MemberType, target)
	return choice, true, nil
}

func (engine *BattleEngine) npcSummonCandidate(pool []npcCandidate, state *npcDecisionState) (npcCandidate, bool) {
	if !engine.alchemist {
		return npcCandidate{}, false
	}
	occupied := state.summons
	for _, player := range engine.players[1:] {
		if player.SummonSource != 0 && player.HP > 0 {
			occupied++
		}
	}
	if occupied >= 3 {
		return npcCandidate{}, false
	}
	// ba4b5 uses the first eligible source in hand order, without a selector
	// draw or ranking its summoned parameters. The outer planner draws rules.
	for _, candidate := range pool {
		if candidate.summon && candidate.skill.AppendTrigger == "" {
			return candidate, true
		}
	}
	return npcCandidate{}, false
}

func npcCandidateGroups(pool []npcCandidate, feature int) [2][]npcCandidate {
	type group struct {
		items  []npcCandidate
		cost   int
		arthur bool
	}
	var groups [2]group
	var features npcFeatures
	for _, candidate := range pool {
		if candidate.summon {
			continue
		}
		if candidate.sphereSlot == 0 && candidate.skill.AppendTrigger != "" {
			continue
		}
		// add4a clears only the requested feature between candidates. Other
		// slots (notably COMBO) retain their previous classification state.
		features[feature] = 0
		npcAccumulateFeatures(candidate.skill, candidate.roles, &features)
		mask := features[feature]
		if mask == 0 {
			continue
		}
		index := 1
		if mask&1 != 0 {
			index = 0
		}
		g := &groups[index]
		// Preserve add4a's original eligibility switch: once its Arthur flag
		// is set, later flagged candidates are skipped; unflagged ones may
		// still replace the group by raw skill cost. Do not reinterpret it.
		if g.arthur && candidate.arthur {
			continue
		}
		if !g.arthur && candidate.arthur {
			g.items = nil
			g.cost = 0
			g.arthur = true
		}
		if candidate.skill.Cost < g.cost {
			continue
		}
		if candidate.skill.Cost > g.cost {
			g.items = nil
			g.cost = candidate.skill.Cost
		}
		if len(g.items) < 8 {
			g.items = append(g.items, candidate)
		}
	}
	return [2][]npcCandidate{groups[0].items, groups[1].items}
}

func (engine *BattleEngine) npcAttackTarget(player *battlePlayer, choice npcCandidate, selector string) (int, bool, error) {
	if selector == "ATTACK_ALL" {
		target, err := engine.npcDefaultTarget(player.MemberType, choice.skill.Target)
		return target, err == nil, err
	}
	parts := strings.HasSuffix(selector, "_PARTS")
	parent := selector == "ATTACK_PARENT"
	hasParts := false
	for i := 0; i < engine.enemyCount; i++ {
		if engine.enemies[i].Parent > 0 {
			hasParts = true
		}
	}
	members := engine.livingSideMembers(player.MemberType, true, parts)
	if parent && !hasParts {
		return 0, false, nil
	}
	if parent {
		kept := members[:0]
		for _, m := range members {
			if engine.enemies[m-5].Parent == 0 {
				kept = append(kept, m)
			}
		}
		members = kept
	}
	if len(members) == 0 || parent && !hasParts {
		return 0, false, nil
	}
	if selector == "ATTACK_ENEMY_BACK" {
		target := members[0]
		for _, m := range members[1:] {
			target = maxInt(target, m)
		}
		return target, true, nil
	}
	if selector == "ATTACK_WEAKNESS" {
		for _, m := range members {
			if engine.npcHasStatusCode(m, "WEAKNESS", true) {
				return m, true, nil
			}
		}
		return 0, false, nil
	}
	if strings.Contains(selector, "RANDOM") || parent {
		return members[int(engine.rng.next()%uint32(len(members)))], true, nil
	}
	sort.SliceStable(members, func(i, j int) bool {
		a, b := members[i], members[j]
		switch {
		case strings.Contains(selector, "LOW_DEF"):
			return engine.npcMetric(a, "DEF") < engine.npcMetric(b, "DEF")
		case strings.Contains(selector, "LOW_MDEF"):
			return engine.npcMetric(a, "MDEF") < engine.npcMetric(b, "MDEF")
		case strings.Contains(selector, "HIGH_HP"):
			av, bv := engine.npcMetric(a, "HP"), engine.npcMetric(b, "HP")
			if av == bv {
				return a > b
			}
			return av > bv
		default:
			return engine.npcMetric(a, "HP") < engine.npcMetric(b, "HP")
		}
	})
	return members[0], true, nil
}

func (engine *BattleEngine) npcRedirectTarget(actor, target int) int {
	if engine.pvp {
		if !engine.playerMember(target) || engine.samePlayerParty(actor, target) {
			return target
		}
	} else if target < 5 || target > 8 {
		return target
	}
	var members []int
	if engine.pvp {
		members = engine.livingSideMembers(target, false, false)
	} else {
		members = engine.livingSideMembers(target, true, false)
	}
	var covering []int
	for _, member := range members {
		for _, effect := range engine.battleMemberEffects(member) {
			if effect.Function == "COVERING" && effect.ListType >= 0 && effect.ListType <= 6 {
				covering = append(covering, member)
				break
			}
		}
	}
	for _, member := range covering {
		if member == target {
			return target
		}
	}
	if len(covering) > 0 {
		return covering[int(engine.rng.next()%uint32(len(covering)))]
	}
	return target
}

func (engine *BattleEngine) npcDefaultTarget(member int, target string) (int, error) {
	code, ok := combatSkillTargetCode(target)
	if !ok {
		return 0, fmt.Errorf("unknown NPC fallback target %s", target)
	}
	if code == 0 || code == 8 {
		return member, nil
	}
	var candidates []int
	switch code {
	case 1, 9:
		for _, p := range engine.playerParty(member) {
			if p.MemberType != 0 && (code == 9 || p.HP > 0) {
				candidates = append(candidates, p.MemberType)
			}
		}
	case 3, 11:
		if engine.pvp {
			for _, p := range engine.opposingParty(member) {
				if p.MemberType != 0 && (code == 11 || p.HP > 0) {
					candidates = append(candidates, p.MemberType)
				}
			}
			break
		}
		for i := 0; i < engine.enemyCount; i++ {
			e := engine.enemies[i]
			if e.MemberType != 0 && (code == 11 || e.HP > 0) {
				candidates = append(candidates, e.MemberType)
			}
		}
	default:
		return 0, nil
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	return candidates[int(engine.rng.next()%uint32(len(candidates)))], nil
}

func (engine *BattleEngine) npcFallback(player *battlePlayer, remaining int, used [33]bool) (npcCandidate, bool, error) {
	if player.SummonSource == 0 {
		for _, slot := range player.Hand {
			if slot == 0 {
				return npcCandidate{}, false, nil
			}
		}
	}
	pool, err := engine.npcCardPool(player, remaining, used)
	if err != nil {
		return npcCandidate{}, false, err
	}
	if len(pool) == 0 {
		return npcCandidate{}, false, nil
	}
	sort.SliceStable(pool, func(i, j int) bool { return pool[i].skill.PriorityPVE > pool[j].skill.PriorityPVE })
	choice := pool[0]
	choice.target, err = engine.npcDefaultTarget(player.MemberType, choice.skill.Target)
	return choice, err == nil, err
}
