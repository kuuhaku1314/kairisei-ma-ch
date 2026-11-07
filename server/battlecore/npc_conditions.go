package battlecore

import (
	"fmt"
	"strconv"
)

// ownFeatures describe choices already made by this NPC's current planning
// pass. They are not allies' committed cards or the previous turn's actions.
type npcDecisionState struct {
	ownFeatures  npcFeatures
	maxHate      int
	allyFeatures npcFeatures
	summons      int
}

func npcNumber(value string) int64 {
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}

func npcHasStatus(player *battlePlayer, good bool, name string) bool {
	wanted := combatTriggerDebuffKind(name)
	if good {
		wanted = combatTriggerBuffKind(name)
	}
	if wanted <= 0 {
		return false
	}
	// aa/ab condition helpers all enumerate the seven retained status lists,
	// unlike the narrower skill-branch predicates in branchHasStatus.
	for _, effect := range player.Effects {
		if branchStatusKind(effect, good) == wanted {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) npcCondition(player *battlePlayer, rule NPCRule, state *npcDecisionState) (bool, error) {
	p := rule.Parameters
	threshold := npcNumber(p[0])
	matchHP := func(hp, maxHP int, percent bool) bool {
		limit := threshold
		if percent {
			limit = int64(maxHP) * threshold / 100
		}
		return int64(hp) <= limit
	}
	switch rule.Situation {
	case "ANYTIME":
		return true, nil
	case "SELF_HP_DOWN":
		return matchHP(player.HP, player.MaxHP, false), nil
	case "SELF_HP_DOWN_PER":
		return matchHP(player.HP, player.MaxHP, true), nil
	case "OTHER_MEMBER_HP_DOWN", "OTHER_MEMBER_HP_DOWN_PER", "MULTI_MEMBER_HP_DOWN", "MULTI_MEMBER_HP_DOWN_PER":
		other := rule.Situation == "OTHER_MEMBER_HP_DOWN" || rule.Situation == "OTHER_MEMBER_HP_DOWN_PER"
		percent := rule.Situation == "OTHER_MEMBER_HP_DOWN_PER" || rule.Situation == "MULTI_MEMBER_HP_DOWN_PER"
		count := int64(0)
		for i := range engine.playerParty(player.MemberType) {
			v := &engine.playerParty(player.MemberType)[i]
			if v.MemberType != 0 && v.HP > 0 && (!other || v.MemberType != player.MemberType) && matchHP(v.HP, v.MaxHP, percent) {
				count++
			}
		}
		if other {
			return count > 0, nil
		}
		return count >= npcNumber(p[1]), nil
	case "ENEMY_HP_DOWN", "ENEMY_HP_DOWN_PER":
		for _, member := range engine.livingSideMembers(player.MemberType, true, false) {
			hp, maxHP, _ := engine.battleMemberHP(member)
			if matchHP(hp, maxHP, rule.Situation == "ENEMY_HP_DOWN_PER") {
				return true, nil
			}
		}
		return false, nil
	case "TURN":
		turn := engine.turn % 10
		if turn == 0 {
			turn = 10
		}
		for _, v := range p {
			if int64(turn) == npcNumber(v) {
				return true, nil
			}
		}
		return false, nil
	case "SELECT_SKILL_ROLE_ATTACK":
		if state.ownFeatures[0] == 0 && state.ownFeatures[1] == 0 {
			return false, nil
		}
		switch p[0] {
		case "PHYSICS":
			return state.ownFeatures[2] != 0, nil
		case "MAGIC":
			return state.ownFeatures[3] != 0, nil
		case "ALL":
			return true, nil
		default:
			return false, nil
		}
	case "SELECT_SKILL_HATE_RATE_OVER":
		return int64(state.maxHate) >= threshold, nil
	case "SELF_HATE_RANK_OVER", "SELF_HATE_RANK_UNDER":
		aliveEnemy := len(engine.livingSideMembers(player.MemberType, true, false)) > 0
		if !aliveEnemy {
			return false, nil
		}
		// 57d90 starts at rank 1; equal hate puts the smaller member first.
		rank := int64(1)
		hate := engine.playerHate(player.MemberType)
		for i := range engine.playerParty(player.MemberType) {
			v := &engine.playerParty(player.MemberType)[i]
			if v.MemberType == 0 || v.HP <= 0 || v.MemberType == player.MemberType {
				continue
			}
			other := engine.playerHate(v.MemberType)
			if other > hate || other == hate && v.MemberType < player.MemberType {
				rank++
			}
		}
		if rule.Situation == "SELF_HATE_RANK_OVER" {
			return rank <= threshold, nil
		}
		return rank >= threshold, nil
	case "SELF_BUFF", "SELF_DEBUFF":
		return npcHasStatus(player, rule.Situation == "SELF_BUFF", p[0]), nil
	case "OTHER_MEMBER_BUFF", "OTHER_MEMBER_DEBUFF", "MULTI_MEMBER_MEMBER_BUFF", "MULTI_MEMBER_MEMBER_DEBUFF":
		other := rule.Situation == "OTHER_MEMBER_BUFF" || rule.Situation == "OTHER_MEMBER_DEBUFF"
		good := rule.Situation == "OTHER_MEMBER_BUFF" || rule.Situation == "MULTI_MEMBER_MEMBER_BUFF"
		kind := combatTriggerDebuffKind(p[0])
		if good {
			kind = combatTriggerBuffKind(p[0])
		}
		if kind <= 0 {
			return false, nil
		}
		count := int64(0)
		for i := range engine.playerParty(player.MemberType) {
			v := &engine.playerParty(player.MemberType)[i]
			if v.MemberType != 0 && v.HP > 0 && (!other || v.MemberType != player.MemberType) && npcHasStatus(v, good, p[0]) {
				count++
			}
		}
		if other {
			return count > 0, nil
		}
		return count >= npcNumber(p[1]), nil
	default:
		return false, fmt.Errorf("unknown NPC situation %q", rule.Situation)
	}
}
