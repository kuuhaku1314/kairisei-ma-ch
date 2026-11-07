package battlecore

import (
	"fmt"
	"strings"
)

func npcHealReverse(player *battlePlayer) bool {
	for _, effect := range player.Effects {
		if effect.Function == "HEAL_REVERSE" && effect.ListType >= 0 && effect.ListType <= 6 {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) npcAuxiliaryTarget(player *battlePlayer, choice npcCandidate, selector npcSelectorRule) (int, bool, error) {
	policy := selector.policy
	switch policy {
	case "self":
		return player.MemberType, true, nil
	case "default":
		target, err := engine.npcDefaultTarget(player.MemberType, choice.skill.Target)
		return target, err == nil, err
	case "defaultAlive":
		return engine.npcLivingDefaultTarget(player.MemberType, choice.skill.Target)
	case "friendRandom":
		return engine.randomLivingTarget(player.MemberType, false)
	case "enemyRandom":
		return engine.randomLivingTarget(player.MemberType, true)
	case "enemyMissing", "enemyWeakness", "enemyWeaknessMissing", "enemyDOT", "enemyWeaknessDOT", "enemyHigh", "friendHigh", "friendLowHP", "friendLowRate", "friendJob", "cureSelf", "cureAll", "cureRandom", "cureMost", "curseSelf", "curseRandom", "curseAll", "enemyAttributeDefense":
		return engine.npcFilteredTarget(player, choice, selector)
	case "healSelf", "healLowHP", "healLowRate", "healAll", "regenerateSelf", "regenerateLowHP", "regenerateLowRate", "regenerateAll":
		// Shared healing selection below.
	default:
		return 0, false, fmt.Errorf("NPC target policy %s is not connected", policy)
	}
	regenerate := strings.HasPrefix(policy, "regenerate")
	applyHeal := func(member int) bool {
		i := member - 1
		if engine.npcSimulatedHP[i] >= engine.npcSimulatedMaxHP[i] {
			return false
		}
		engine.npcSimulatedHP[i] = min(engine.npcSimulatedMaxHP[i], engine.npcSimulatedHP[i]+choice.power)
		return true
	}
	// ab657 redirects healing roles on a hostile skill header. It is not the
	// ordinary default target helper: a friendly header has no such fallback.
	hostileFallback := func() (int, bool, error) {
		if choice.skill.Target == "ENEMY_ONE" || choice.skill.Target == "ENEMY_ALL" {
			return engine.randomLivingTarget(player.MemberType, true)
		}
		return 0, false, nil
	}
	if strings.HasSuffix(policy, "Self") {
		if npcHealReverse(player) {
			return 0, false, nil
		}
		if regenerate {
			if target, ok, err := hostileFallback(); ok || err != nil {
				return target, ok, err
			}
			return player.MemberType, true, nil
		}
		if choice.power > 0 && applyHeal(player.MemberType) {
			return player.MemberType, true, nil
		}
		return hostileFallback()
	}
	if strings.HasSuffix(policy, "All") {
		healed := false
		for i := range engine.playerParty(player.MemberType) {
			p := &engine.playerParty(player.MemberType)[i]
			if p.MemberType == 0 || p.HP <= 0 {
				continue
			}
			if npcHealReverse(p) {
				return 0, false, nil
			}
			if !regenerate && p.MaxHP > 0 && p.HP < p.MaxHP && applyHeal(p.MemberType) {
				healed = true
			}
		}
		if regenerate {
			if target, ok, err := hostileFallback(); ok || err != nil {
				return target, ok, err
			}
			return engine.npcLivingDefaultTarget(player.MemberType, choice.skill.Target)
		}
		if healed && choice.power > 0 {
			return engine.npcLivingDefaultTarget(player.MemberType, choice.skill.Target)
		}
		return hostileFallback()
	}
	// Native chooses the lowest *actual* injured HP (or truncated percentage)
	// first, then rejects if its simulated HP was filled by an earlier rule.
	// It does not retry the next injured member in that evaluation.
	target, best := 0, int64(0x7fffffff)
	for i := range engine.playerParty(player.MemberType) {
		p := &engine.playerParty(player.MemberType)[i]
		if p.MemberType == 0 || p.HP <= 0 || p.HP >= p.MaxHP || npcHealReverse(p) {
			continue
		}
		metric := int64(p.HP)
		if strings.HasSuffix(policy, "LowRate") {
			if p.MaxHP <= 0 {
				continue
			}
			metric = metric * 100 / int64(p.MaxHP)
		}
		if metric < best {
			target, best = p.MemberType, metric
		}
	}
	if target == 0 {
		return 0, false, nil
	}
	if regenerate {
		return target, true, nil
	}
	return target, choice.power > 0 && applyHeal(target), nil
}

func (engine *BattleEngine) npcLivingDefaultTarget(member int, target string) (int, bool, error) {
	switch target {
	case "USER_ONE", "USER_ONE_INVOLVE_DEAD":
		return engine.randomLivingTarget(member, false)
	case "ENEMY_ONE", "ENEMY_ONE_INVOLVE_DEAD":
		return engine.randomLivingTarget(member, true)
	default:
		value, err := engine.npcDefaultTarget(member, target)
		return value, err == nil, err
	}
}
