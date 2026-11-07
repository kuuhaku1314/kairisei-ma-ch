package battlecore

import (
	"errors"
	"strings"
)

const (
	CommandTurn              = 1
	CommandEnd               = 2
	CommandHP                = 3
	CommandBaseParam         = 5
	CommandBattleParam       = 6
	CommandWaitAndSee        = 7
	CommandStoryEnd          = 8
	CommandContinue          = 9
	CommandContinueWait      = 10
	CommandCost              = 12
	CommandEnableTarget      = 13
	CommandCostBlock         = 14
	CommandCardDeal          = 20
	CommandCardDeck          = 21
	CommandCardPlay          = 22
	CommandCardIdentity      = 23
	CommandCardWeak          = 24
	CommandCardUpdate        = 25
	CommandCardCost          = 27
	CommandCardPass          = 28
	CommandCardState         = 29
	CommandStun              = 31
	CommandRewrite           = 32
	CommandRevive            = 11
	CommandBuff              = 62
	CommandPassiveBuff       = 69
	CommandHPCut             = 64
	CommandDebuffFailed      = 65
	CommandBuffRelease       = 66
	CommandBuffReleaseFailed = 67
	CommandSkillRevive       = 68
	CommandBuffPartition     = 70
	CommandBuffLostOne       = 71
	CommandChargeStart       = 74
	CommandPartsBreak        = 80
	CommandEnemyBreak        = 81
	CommandGameOver          = 82
	CommandEnemyFirstAttack  = 85
	CommandAppendTurnAdd     = 206
	CommandCardNeedCostUp    = 209
	CommandHoldSet           = 310
	CommandHoldRemainder     = 311
	CommandHoldMax           = 312
	CommandHoldStore         = 313
	CommandHoldLost          = 316
	CommandAppendRelease     = 317
	CommandBurstCondition    = 320
	CommandBuddy             = 321
	CommandBuddySlot         = 322
	CommandBurstDiscard      = 323
	CommandAddCardBuff       = 324
	CommandCardBuff          = 325
	CommandCardPlayReset     = 326
	CommandSummonCondition   = 328
	CommandSummon            = 701
	CommandAttackPartition   = 502
	CommandLimitTurn         = 503
	CommandBurstStateChange  = 504
	CommandBurstState        = 505
	CommandBurstGaugeState   = 506
	CommandEnemyDebuff       = 507
	CommandFixedAttrDefense  = 508
	CommandBurstSkill        = 700
	CommandEnemyAttackSign   = 86
	CommandEnemyAIVariables  = 94
	CommandPartsBreakDrop    = 83
	CommandEnemyBreakDrop    = 84
	CommandPlayLogState      = 602
	CommandHoldSkill         = 58
	CommandHoldSkillEnd      = 59
	CommandResumeTurn        = 100
	CommandResumeCardHand    = 102
	CommandResumeCardDeck    = 103
	CommandResumeBuff        = 104
	CommandResumeEnemy       = 106
	CommandResumeGameOver    = 107
	CommandResumeEnemyDrop   = 108
	CommandResumeHaveDrop    = 109
	CommandResumeHold        = 110
)

var battleBuffCodes = map[string]int{
	"ATK_UP_FIXED": 0, "ATK_UP_BY_SELF_PARAM": 1, "CRITICAL_UP": 10,
	"ATK_UP_FIXED_ENEMY": 58, "DEF_UP_FIXED_ENEMY": 59,
	"ATK_BREAK_CUT": 60, "GUARD_BREAK_CUT": 61, "ATTR_DEF_DOWN_BOOST": 63,
	"PHYSICS_DAMAGE_BOOST_COUNT": 217, "MAGIC_DAMAGE_BOOST_COUNT": 218,
	"ATK_BREAK_FIXED_ENEMY": 123, "GUARD_BREAK_FIXED_ENEMY": 124,
	"ATK_UP_BY_NOW_TURN_DAMAGE": 8, "ATK_UP_BY_TARGET_PARAM": 1,
	"DAMAGE_BOOST": 22, "ATK_UP_BOOST": 23, "DEF_UP_BOOST": 24, "ATK_BREAK_BOOST": 25,
	"GUARD_BREAK_BOOST": 26, "HEAL_BOOST": 27, "CRITICAL_BOOST": 28, "DAMAGE_CUT2": 29,
	"DEF_UP_FIXED": 4, "DEF_UP_BY_SELF_PARAM": 5,
	"DAMAGE_UP": 2, "DAMAGE_CUT": 3,
	"ATTR_DEF_UP": 19, "ENCHANT": 20, "PARAM_LIMIT_BREAK_FIXED": 32,
	"ATK_BREAK_FIXED": 100, "ATK_BREAK_BY_SELF_PARAM": 101,
	"ATK_BREAK_BY_NOW_TURN_DAMAGE": 109, "ATK_BREAK_BY_TARGET_PARAM": 115,
	"GUARD_BREAK_FIXED": 102, "GUARD_BREAK_BY_SELF_PARAM": 103, "GUARD_BREAK_BY_TARGET_PARAM": 104,
	"GUARD_BREAK_BY_NOW_TURN_DAMAGE": 110, "ATTR_SEE": 107, "CARD_TRAP_DAMAGE": 112,
	"ATTR_DEF_DOWN": 113, "CRITICAL_DOWN": 114, "CARD_SEAL_REGIST": 203,
	"ATTACK_BARRIER_APPOINT_ATTR": 205, "DARKNESS_REGIST": 207, "REFLECTION": 208,
	"GUTS": 209, "COVERING": 206, "STAN": 301, "POISON": 304,
	"BURN": 305, "FREEZE": 306, "BLEED": 307, "WEAKNESS": 308,
	"ELECTRIC": 309, "COST_BLOCK": 311, "DEAL_BONUS": 400,
	"DEAL_PENALTY": 408, "ENDURE": 409, "ENEMY_CURSE": 410,
	"BLESS": 411, "CARD_SEAL": 106, "DARKNESS_APPOINT": 111,
	"DARKNESS_RANDOM": 111, "ATTR_HIDE": 204, "ATTACK_BARRIER": 205,
	"HEAL_REVERSE": 108, "HEAL_CUT": 312, "DEBUFF_REGIST": 14, "DAMAGE_DOWN": 105, "REGENERATE_FIXED": 200,
	"REGENERATE_BY_SELF_PARAM": 200, "REWRITE": 202, "DEAL_PENALTY_TURN_APPOINT": 408,
	"CRITICAL_DAMAGE_BOOST": 216,
	"ATTR_CUT_ATTACK":       210, "ATTR_DRAIN_ATTACK": 211,
	"ATTR_CUT_ENCHANT": 212, "ATTR_DRAIN_ENCHANT": 213,
	"ATTR_CUT_DOT": 214, "ATTR_DRAIN_DOT": 215,
	"BEGINNING_DRAW": 412, "NEED_COST_DOWN": 15,
}

// BattleResult retains the existing adapter name while consumers move to the core.
type BattleResult = Result

// battleEndResult mirrors FUN_00061d60's reachable TeamBattle END rows.
// 74d97 -> 4a944 formats signed 64-bit arguments. The zero following the
// end type in x86 decompilation is its high word, not another CSV field.
func battleEndResult(endType int) BattleResult {
	return BattleResult{Command: CommandEnd, Args: []int64{int64(endType)}}
}

// battleGameOverResult mirrors battle5_api_gameover's ResultCmd82 row.
func battleGameOverResult(memberType int) BattleResult {
	return BattleResult{Command: CommandGameOver, Args: []int64{int64(memberType)}}
}

func (engine *BattleEngine) CardPlayPlanResults(memberType int, submission cardPlaySubmission) ([]BattleResult, error) {
	if memberType < 1 || memberType > maxRoomMembers {
		return nil, errors.New("card play plan member type is invalid")
	}
	if engine == nil {
		return nil, errors.New("card play plan battle engine is unavailable")
	}
	results := make([]BattleResult, 0, len(submission.CardTypes))
	for index, cardType := range submission.CardTypes {
		if cardType == 0 {
			continue
		}
		darkness := engine.playerCardDarkness(memberType, cardType)
		state := 0
		if _, sealed := playerCardSealEffect(engine.player(memberType), cardType); sealed {
			state |= 1
		}
		if _, trapped := playerCardTrapEffect(engine.player(memberType), cardType); trapped {
			state |= 2
		}
		results = append(results, BattleResult{Command: 303, Args: []int64{
			int64(memberType), int64(cardType), int64(submission.Targets[index]), 0, 0, int64(darkness), int64(state),
		}})
	}
	if submission.SphereSlot != 0 {
		action, err := engine.validateSphereSubmission(memberType, submission)
		if err != nil {
			return nil, err
		}
		results = append(results, BattleResult{Command: CommandSpherePlayPlan, Args: []int64{
			int64(memberType), int64(action.sphereSlot), int64(action.target),
		}})
	}
	if len(results) == 0 {
		// The original 303 consumer clears the teammate's previous selection
		// when cardType is zero. Silence would leave cancelled cards visible.
		results = append(results, BattleResult{Command: CommandCardPlayPlan, Args: []int64{
			int64(memberType), 0, 0, 0, 0, 0, 0,
		}})
	}
	return results, nil
}

func (engine *BattleEngine) playerCardDarkness(memberType int, cardType int) int {
	if !engine.playerMember(memberType) {
		return 0
	}
	player := engine.player(memberType)
	slot := -1
	for index, deckSlot := range player.Hand {
		if deckSlot != 0 && player.handSlotCard(deckSlot).CardType == cardType {
			slot = index
			break
		}
	}
	if slot < 0 {
		return 0
	}
	return playerDarknessMask(player) >> (4 - slot) & 1
}

func playerDarknessMask(player *battlePlayer) int {
	mask := 0
	for _, effect := range player.Effects {
		if strings.HasPrefix(effect.Function, "DARKNESS") && effect.Remaining > 0 {
			mask |= effect.Mask
		}
	}
	return mask
}
