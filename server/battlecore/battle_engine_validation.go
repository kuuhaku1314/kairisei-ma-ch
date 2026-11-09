package battlecore

import (
	"errors"
	"fmt"
)

func (engine *BattleEngine) validateTarget(targetKind string, memberType int, target int) error {
	switch targetKind {
	case "SELF", "USER_ALL", "ENEMY_ALL":
		// Native7adb0 builds these target lists without the submitted scalar.
		// Keep the selection's raw value for its echo; execution owns the list.
		if target != 0 {
			if _, _, exists := engine.battleMemberHP(target); !exists {
				return errors.New("skill target member is invalid")
			}
		}
	case "USER_ONE":
		if !engine.playerMember(target) || !engine.samePlayerParty(memberType, target) {
			return errors.New("single ally skill target is invalid")
		}
	case "ENEMY_ONE":
		if engine.pvp && (!engine.playerMember(target) || engine.samePlayerParty(memberType, target) || engine.player(target).HP <= 0) || !engine.pvp && !engine.livingEnemyTarget(target) {
			return errors.New("single enemy skill target is invalid")
		}
	default:
		return fmt.Errorf("unsupported combat target kind %q", targetKind)
	}
	return nil
}

func (engine *BattleEngine) livingEnemyTarget(target int) bool {
	if target == 0 {
		return engine.enemyCount > 0 && engine.enemies[0].HP > 0
	}
	if target < 5 || target >= 5+engine.enemyCount {
		return false
	}
	return engine.enemies[target-5].HP > 0
}

func (player *battlePlayer) cardInHand(cardType int) (BattleCard, bool) {
	if player.SummonSource != 0 && cardType == 1 {
		return player.Deck[0], true
	}
	for _, deckSlot := range player.Hand {
		if deckSlot == 0 {
			continue
		}
		card := player.handSlotCard(deckSlot)
		if card.CardType == cardType {
			return card, true
		}
	}
	return BattleCard{}, false
}

func initialParameterLimit(alchemist bool) int {
	if alchemist {
		return 999999
	}
	return 99999
}

func baseParameterArgsWithLimit(memberType, hp, attack, magic, recovery, defense, magicDefense, limit int) []int64 {
	return []int64{int64(memberType), int64(hp), int64(attack), int64(magic), int64(recovery), int64(defense), int64(magicDefense), int64(limit), int64(limit), int64(limit)}
}

func battleParameterArgs(memberType int, hp int, maxHP int, attack int, magic int, recovery int, defense int, magicDefense int, limitAttack int, limitMagic int, limitRecovery int) []int64 {
	return []int64{
		int64(memberType), int64(hp), int64(maxHP), int64(attack), int64(magic), int64(recovery), int64(defense), int64(magicDefense),
		int64(activeParameterLimit(limitAttack)), int64(activeParameterLimit(limitMagic)), int64(activeParameterLimit(limitRecovery)),
	}
}

// Manual single-target zero aliases and NPC selection share the living pool.
// The native selection draws even when only one member is available.
func (engine *BattleEngine) randomLivingTarget(member int, enemy bool) (int, bool, error) {
	var members []int
	if engine.pvp {
		party := engine.playerParty(member)
		if enemy {
			party = engine.opposingParty(member)
		}
		for _, p := range party {
			if p.MemberType != 0 && p.HP > 0 {
				members = append(members, p.MemberType)
			}
		}
	} else if enemy {
		for i := 0; i < engine.enemyCount; i++ {
			p := &engine.enemies[i]
			if p.MemberType != 0 && p.HP > 0 {
				members = append(members, p.MemberType)
			}
		}
	} else {
		for _, p := range engine.players {
			if p.MemberType != 0 && p.HP > 0 {
				members = append(members, p.MemberType)
			}
		}
	}
	if len(members) == 0 {
		return 0, false, nil
	}
	return members[int(engine.rng.next()%uint32(len(members)))], true, nil
}
