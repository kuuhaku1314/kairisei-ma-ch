package battlecore

import (
	"fmt"
	"strings"
)

const CommandSkill = 50
const CommandCardSkill = 51
const CommandBuffEffect = 53
const CommandSphereSkill = 56
const CommandSphere = 300
const CommandSphereCount = 301
const CommandSphereCost = 302
const CommandCardPlayPlan = 303
const CommandSpherePlay = 304
const CommandSpherePlayPlan = 305
const CommandSphereUpdate = 307
const CommandSphereHand = 308
const CommandSpherePlayCondition = 309
const CommandCardUpdate2 = 314
const CommandSphereUpdate2 = 315
const CommandChaliceSphereReserve = 318
const CommandChaliceSpherePlayable = 319

// sphereHandResult is the fixed three-slot initialization emitted by native
// FUN_000a5776. These values are BATTLE5_SPHR_SLOT enum members, not sphere
// master IDs from the player's account.
func sphereHandResult(memberType int) BattleResult {
	return BattleResult{Command: CommandSphereHand, Args: []int64{int64(memberType), 1, 2, 3}}
}

// sphereUpdateResult is native ResultCmd307. HandsData consumes columns two
// through six as arousal/four-chain flags, execResultCmd reads column seven as
// the displayed power and column eight as the boosted marker. Sphere slots use
// CARD_TYPE_NONE, so the slot itself is column one.
func sphereUpdateResult(memberType int, slot int, power int, marker int) BattleResult {
	return BattleResult{Command: CommandSphereUpdate, Args: []int64{
		int64(memberType), int64(slot), 0, 0, 0, 0, 0, int64(power), int64(marker),
	}}
}

// cardUpdate2Result is native ResultCmd314. The fifth value is zero for an
// ordinary hand card and the concrete append index for CURSE/BLESS cards.
// Marker is FUN_0007982a's selected extension marker, not a server-authored
// boolean; the managed consumer converts any nonzero value to is_boost.
func cardUpdate2Result(memberType int, cardType int, state battleDisplayPower, appendIndex int) BattleResult {
	return BattleResult{Command: CommandCardUpdate2, Args: []int64{
		int64(memberType), int64(cardType), int64(state.Power), int64(state.Marker), int64(appendIndex),
	}}
}

// sphereUpdate2Result is native ResultCmd315. Native compares both cached
// power and extension marker, and transports both in its four-value payload.
func sphereUpdate2Result(memberType int, slot int, power int, marker int) BattleResult {
	return BattleResult{Command: CommandSphereUpdate2, Args: []int64{
		int64(memberType), int64(slot), int64(power), int64(marker),
	}}
}

// buddySlotResult mirrors native FUN_000a8ba6. The five leading values are
// fixed BATTLE5_BUDDY_TYPE slots. Only the leader buddy (BUDDY1) contributes
// the three following release flags, each gated by an actual skill ID and the
// official buddy release level.
func (engine *BattleEngine) buddySlotResult(player *battlePlayer) BattleResult {
	args := []int64{int64(player.MemberType), 1, 2, 3, 4, 5, 0, 0, 0}
	leader := player.Buddies[0]
	definition, exists := engine.catalog.Buddies[leader.BuddyID]
	if !exists {
		return BattleResult{Command: CommandBuddySlot, Args: args}
	}
	for index, skill := range definition.Skills {
		if skill.SkillID != 0 && skill.RequiredLevel <= leader.Level {
			args[6+index] = 1
		}
	}
	return BattleResult{Command: CommandBuddySlot, Args: args}
}

// enemySkillResult is the sole projection owner for native ResultCmd50.
// BattleDirection2D/3D consume the seven columns as actor, outer skill ID,
// concrete target member, skill level, SKILL_TARGET, selected role ID and the
// passive flag. Ordinary/passive inputs use level1; gauge-transition inputs use0.
func enemySkillResult(actorMember int, outerSkillID int, targetMember int, skill CombatSkillDefinition, level int, passive bool) (BattleResult, error) {
	targetCode, ok := combatSkillTargetCode(skill.Target)
	if !ok {
		return BattleResult{}, fmt.Errorf("enemy skill %d has unsupported SKILL_TARGET %q", outerSkillID, skill.Target)
	}
	passiveFlag := int64(0)
	if passive {
		passiveFlag = 1
	}
	return BattleResult{Command: CommandSkill, Args: []int64{
		int64(actorMember),
		int64(outerSkillID),
		int64(targetMember),
		int64(level),
		int64(targetCode),
		int64(skill.FunctionID),
		passiveFlag,
	}}, nil
}

// playerCardSkillResult mirrors native ResultCmd51. The cut-in flag describes
// whether CardSkill selected the card's profession-matched Arthur skill rather
// than its normal skill. branchIndex is FUN_000d7578's zero-based selected
// extend slot; the managed direction layer treats nonzero as boosted display.
func (engine *BattleEngine) playerCardSkillResult(action battleAction, chainCount int) (BattleResult, error) {
	targetCode, ok := combatSkillTargetCode(action.skill.Target)
	if !ok {
		return BattleResult{}, fmt.Errorf("player skill %d has unsupported SKILL_TARGET %q", action.skill.ID, action.skill.Target)
	}
	card, ok := engine.catalog.Cards[action.cardID]
	if !ok && !isTokenCardType(action.cardType) && !action.helper {
		return BattleResult{}, fmt.Errorf("combat card %d is unavailable", action.cardID)
	}
	cutin := int64(0)
	// Some official cards share the same normal/Arthur skill ID with Job
	// NULL. Identity alone does not mean the Arthur branch was selected.
	if action.skill.ID == card.ArthurSkillID && engine.playerMember(action.memberType) &&
		engine.catalog.cardUsesArthurSkill(action.cardID, engine.player(action.memberType).ArthurType) {
		cutin = 1
	}
	modified := false
	if engine.playerMember(action.memberType) && action.cardType > 0 && action.cardType < len(engine.player(action.memberType).CardBurstSkills) {
		modified = len(engine.player(action.memberType).CardBurstSkills[action.cardType]) > 0
	}
	return BattleResult{Command: CommandCardSkill, Args: []int64{
		int64(action.memberType),
		int64(action.cardType),
		int64(action.skill.ID),
		int64(action.target),
		int64(action.cardLevel),
		int64(targetCode),
		cutin,
		int64(chainCount),
		int64(action.skill.FunctionID),
		int64(action.branchIndex),
		0,
		boolInt64(modified),
	}}, nil
}

// playerSphereSkillResult is native FUN_0007adb0 case 5. It uses the same
// direction columns as CARD_SKILL through function ID, but column one is the
// BATTLE5_SPHR_SLOT and the native row ends after the counter flag.
func playerSphereSkillResult(action battleAction, chainCount int) (BattleResult, error) {
	targetCode, ok := combatSkillTargetCode(action.skill.Target)
	if !ok {
		return BattleResult{}, fmt.Errorf("sphere skill %d has unsupported SKILL_TARGET %q", action.skill.ID, action.skill.Target)
	}
	return BattleResult{Command: CommandSphereSkill, Args: []int64{
		int64(action.memberType),
		int64(action.sphereSlot),
		int64(action.skill.ID),
		int64(action.target),
		int64(action.cardLevel),
		int64(targetCode),
		0,
		int64(chainCount),
		int64(action.skill.FunctionID),
		int64(action.branchIndex),
		0,
	}}, nil
}

// buffEffectResult mirrors native FUN_0007adb0 case 2. It starts a standalone
// BUFF_EFFECT direction group with the acting member and concrete target. The
// group's damage/heal/buff rows follow as ordinary child commands.
func buffEffectResult(ownerMember int, targetMember int) BattleResult {
	return BattleResult{Command: CommandBuffEffect, Args: []int64{
		int64(ownerMember), int64(targetMember),
	}}
}

// buffStatusEffectResult is the native status-trigger specialization used by
// DOT, card trap, GUTS and similar effects outside a skill action. With no
// active role direction to identify the presentation, libbattle5 appends the
// concrete BATTLE_BUFF code after the target duplicated as owner and target.
func buffStatusEffectResult(targetMember int, buffCode int) BattleResult {
	return BattleResult{Command: CommandBuffEffect, Args: []int64{
		int64(targetMember), int64(targetMember), int64(buffCode),
	}}
}

func combatSkillTargetCode(target string) (int, bool) {
	switch strings.ToUpper(strings.TrimSpace(target)) {
	case "", "0", "SELF":
		return 0, true
	case "USER_ONE":
		return 1, true
	case "USER_ALL":
		return 2, true
	case "ENEMY_ONE":
		return 3, true
	case "ENEMY_ALL":
		return 4, true
	case "DEAD_ENEMY_ONE":
		return 5, true
	case "DEAD_ENEMY_ALL":
		return 6, true
	case "ALL":
		return 7, true
	case "SELF_INVOLVE_DEAD":
		return 8, true
	case "USER_ONE_INVOLVE_DEAD":
		return 9, true
	case "USER_ALL_INVOLVE_DEAD":
		return 10, true
	case "ENEMY_ONE_INVOLVE_DEAD":
		return 11, true
	case "ENEMY_ALL_INVOLVE_DEAD":
		return 12, true
	case "MERCENARY":
		return 13, true
	case "MILLIONAIRE":
		return 14, true
	case "THIEF":
		return 15, true
	case "SINGER":
		return 16, true
	case "HIGH_ATK_USER":
		return 17, true
	case "HIGH_INT_USER":
		return 18, true
	case "HIGH_DEF_USER":
		return 19, true
	case "HIGH_MDEF_USER":
		return 20, true
	case "PARENT":
		return 21, true
	case "HIGH_HP_ENEMY":
		return 22, true
	case "HIGH_DEF_ENEMY":
		return 23, true
	case "HIGH_MDEF_ENEMY":
		return 24, true
	case "LOW_HP_ENEMY":
		return 25, true
	case "LOW_DEF_ENEMY":
		return 26, true
	case "LOW_MDEF_ENEMY":
		return 27, true
	case "HAND_SELECT":
		return 28, true
	case "HAND_ALL":
		return 29, true
	default:
		return 0, false
	}
}
