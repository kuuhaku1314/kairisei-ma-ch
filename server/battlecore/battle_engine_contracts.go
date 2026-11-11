package battlecore

import (
	"errors"
	"fmt"
	"strings"
)

// ValidateCoreContracts is a deterministic in-memory differential
// gate for contracts recovered from the official CN managed/native client. It
// performs no network or emulator activity and intentionally exercises domain
// state separately from its ResultCmd transport projection.
func ValidateCoreContracts(catalog *CombatCatalog, codec ContractResultCodec) error {
	if codec.Row == nil || codec.Group == nil {
		return errors.New("battle contract result codec is unavailable")
	}
	if catalog == nil {
		return errors.New("battle engine contract catalog is unavailable")
	}
	if err := validateXorShift128Contracts(); err != nil {
		return err
	}
	if err := validateHandProjectionContracts(codec.Row); err != nil {
		return err
	}
	if err := validateBattleDropContracts(codec.Row); err != nil {
		return err
	}
	if err := validateBattleCardIdentityContracts(codec.Row); err != nil {
		return err
	}
	if err := validateBattlePartitionContracts(codec.Row); err != nil {
		return err
	}
	if err := validateBattlePhaseControlContracts(codec.Row); err != nil {
		return err
	}
	if err := validateSphereContracts(catalog); err != nil {
		return err
	}
	if err := ValidateEnemyPassiveDefinitions(catalog); err != nil {
		return err
	}
	if err := validateEnemyPassiveSimulation(catalog); err != nil {
		return err
	}
	if err := validateEnemyFixedHealLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemySelfParameterLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyReviveLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyFixedRegenerateLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyAttackLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyAttackOptionLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyAttackBarrierLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyDOTLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyRetainedLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyReleaseLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyAllDebuffReleaseContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyAllBuffReleaseContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyRewriteContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyZeroGrowthLevelContracts(catalog); err != nil {
		return err
	}
	if err := validateBattleInputTransportContracts(catalog); err != nil {
		return err
	}
	if err := validateParentDamageContracts(); err != nil {
		return err
	}
	if err := validateWholeBattleContract(catalog); err != nil {
		return err
	}
	for conditionID, condition := range catalog.EnemyAIOrders {
		if !ValidEnemyAIOrderFields(condition.Fields) {
			return fmt.Errorf("official enemy AI condition %d has incomplete fields: %d", conditionID, len(condition.Fields))
		}
		trigger := strings.ToUpper(condition.Fields[29])
		if !SupportsEnemyAITrigger(trigger) {
			return fmt.Errorf("official enemy AI condition %d trigger %q is unsupported", conditionID, trigger)
		}

	}
	for enemyLevelID, level := range catalog.EnemyLevels {

		for slot, callSkillID := range level.CallSkillIDs {
			if callSkillID == 0 {
				continue
			}
			variants := catalog.Skills[callSkillID]
			if len(variants) == 0 {
				return fmt.Errorf("official enemy level %d call-skill slot %d references missing skill %d", enemyLevelID, slot, callSkillID)
			}
			for _, variant := range variants {
				if err := ValidateAppendSkillDefinition("enemy call", variant); err != nil {
					return err
				}
			}
		}
		for _, action := range level.Actions {
			slotValid := action.Category == "normal" && action.Slot == 0 ||
				action.Category == "skill" && action.Slot >= 1 && action.Slot <= 14 ||
				action.Category == "special" && action.Slot >= 15 && action.Slot <= 19 ||
				action.Category == "death" && action.Slot == 20
			if !slotValid {
				return fmt.Errorf("official enemy level %d action %d has invalid category/slot %q/%d", enemyLevelID, action.SkillID, action.Category, action.Slot)
			}
			// Native compares xor128()%10000 with exec_rate*100 without
			// clamping. The official charge rows deliberately use values such
			// as 1000, which therefore mean unconditional success.
			if action.ActionCost < 0 || action.Rate < 0 {
				return fmt.Errorf("official enemy level %d action %d has invalid scheduler cost/rate %d/%d", enemyLevelID, action.SkillID, action.ActionCost, action.Rate)
			}
			effectiveTarget := strings.ToUpper(strings.TrimSpace(action.Target))
			if effectiveTarget == "" || effectiveTarget == "NULL" {
				if skill, _, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(action.SkillID); ok {
					effectiveTarget = strings.ToUpper(strings.TrimSpace(skill.Target))
				}
			}
			if !SupportsEnemyActionTarget(effectiveTarget) {
				return fmt.Errorf("official enemy level %d action %d target %q is unsupported", enemyLevelID, action.SkillID, effectiveTarget)
			}
			if effectiveTarget == "USER_DEBUFF" {
				foundDebuff := false
				for _, parameter := range action.TargetParams {
					if strings.TrimSpace(parameter) == "" {
						continue
					}
					foundDebuff = true
					if combatTriggerDebuffKind(parameter) == 0 {
						return fmt.Errorf("official enemy level %d action %d USER_DEBUFF parameter %q is unsupported", enemyLevelID, action.SkillID, parameter)
					}
				}
				if !foundDebuff {
					return fmt.Errorf("official enemy level %d action %d USER_DEBUFF has no selector parameter", enemyLevelID, action.SkillID)
				}
			}
			if !strings.EqualFold(action.Target, "TRIGGER_TARGET") {
				continue
			}
			condition, exists := catalog.EnemyAIOrders[action.AIConditionID]
			if !exists || len(condition.Fields) < 30 {
				return fmt.Errorf("official enemy level %d TRIGGER_TARGET action %d has no AI condition", enemyLevelID, action.SkillID)
			}
			trigger := strings.ToUpper(condition.Fields[29])
			if !enemyAITriggerRetainsTarget(trigger) {
				return fmt.Errorf("official enemy level %d action %d uses TRIGGER_TARGET with non-retaining trigger %q", enemyLevelID, action.SkillID, trigger)
			}
		}
	}
	for skillID, variants := range catalog.EnemySkillPrograms() {
		for _, variant := range variants {
			if _, ok := combatSkillTargetCode(variant.Target); !ok {
				return fmt.Errorf("official enemy skill %d has unsupported SKILL_TARGET %q", skillID, variant.Target)
			}
		}
	}
	if err := validateEnemyAIConditionContracts(); err != nil {
		return err
	}
	for skillID, variants := range catalog.EnemySkillPrograms() {
		for _, variant := range variants {
			for _, condition := range []string{variant.BranchCondition, variant.BranchCondition2} {
				if !SupportsEnemyBranchCondition(condition) {
					return fmt.Errorf("official enemy skill %d extension condition %q is unsupported", skillID, condition)
				}
			}
		}
	}
	for skillID, variants := range catalog.PlayerSkillPrograms() {
		for _, variant := range variants {
			if _, ok := combatSkillTargetCode(variant.Target); !ok {
				return fmt.Errorf("official player skill %d has unsupported SKILL_TARGET %q", skillID, variant.Target)
			}
			for _, condition := range []string{variant.BranchCondition, variant.BranchCondition2} {
				if !SupportsPlayerBranchCondition(condition) {
					return fmt.Errorf("official player skill %d extension condition %q is unsupported", skillID, condition)
				}
			}
			roles := catalog.SkillRoles[variant.FunctionID]
			for _, role := range roles {
				if role.Function == "BLESS" {
					if err := ValidateAppendSkillDefinition("player blessing", variant); err != nil {
						return err
					}
					break
				}
			}
		}
	}
	projectionCatalog := &CombatCatalog{
		Cards:  map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 100, ArthurSkillID: 101}},
		Skills: map[int][]CombatSkillDefinition{101: {{ID: 101, Job: "MERCENARY", ArthurJob: "MERCENARY"}}},
	}
	projectionEngine := &BattleEngine{catalog: projectionCatalog}
	projectionEngine.players[0].ArthurType = 1
	normalProjection, err := projectionEngine.playerCardSkillResult(battleAction{
		memberType: 1, cardID: 1, cardType: 3, cardLevel: 60, target: 5, branchIndex: 0,
		skill: CombatSkillDefinition{ID: 100, Target: "ENEMY_ONE", FunctionID: 200},
	}, 2)
	if err != nil {
		return err
	}
	agreeProjection, err := projectionEngine.playerCardSkillResult(battleAction{
		memberType: 1, cardID: 1, cardType: 3, cardLevel: 60, target: 0, branchIndex: 2,
		skill: CombatSkillDefinition{ID: 101, Target: "USER_ALL", FunctionID: 201},
	}, 3)
	if err != nil {
		return err
	}
	if normalProjection.Command != CommandCardSkill || len(normalProjection.Args) != 12 || normalProjection.Args[5] != 3 ||
		normalProjection.Args[6] != 0 || normalProjection.Args[7] != 2 || normalProjection.Args[8] != 200 || normalProjection.Args[9] != 0 ||
		agreeProjection.Command != CommandCardSkill || len(agreeProjection.Args) != 12 || agreeProjection.Args[5] != 2 ||
		agreeProjection.Args[6] != 1 || agreeProjection.Args[7] != 3 || agreeProjection.Args[8] != 201 || agreeProjection.Args[9] != 2 {
		return fmt.Errorf("player CARD_SKILL projections are normal=%+v agree=%+v", normalProjection, agreeProjection)
	}
	buffProjection := buffEffectResult(5, 6)
	if buffProjection.Command != CommandBuffEffect || len(buffProjection.Args) != 2 || buffProjection.Args[0] != 5 ||
		buffProjection.Args[1] != 6 {
		return fmt.Errorf("BUFF_EFFECT projection is %+v", buffProjection)
	}
	buffStatusProjection := buffStatusEffectResult(1, battleBuffCodes["GUTS"])
	if buffStatusProjection.Command != CommandBuffEffect || len(buffStatusProjection.Args) != 3 ||
		buffStatusProjection.Args[0] != 1 || buffStatusProjection.Args[1] != 1 || buffStatusProjection.Args[2] != 209 {
		return fmt.Errorf("BUFF_EFFECT status projection is %+v", buffStatusProjection)
	}
	buddyDefinition, exists := catalog.Buddies[1000010]
	if !exists || buddyDefinition.Skills[0].SkillID != 65000001 || buddyDefinition.Skills[0].GaugeCost != 45 ||
		buddyDefinition.Skills[0].RechargeTurn != 3 {
		return fmt.Errorf("official buddy 1000010 burst contract is %+v", buddyDefinition)
	}
	buddyEngine := &BattleEngine{catalog: catalog}
	buddyPlayer := battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1}
	buddyPlayer.Buddies[0] = BattleBuddy{BuddyType: 1, BuddyID: 1000010, Level: 80}
	buddyProjection := buddyEngine.buddySlotResult(&buddyPlayer)
	if buddyProjection.Command != CommandBuddySlot || len(buddyProjection.Args) != 9 ||
		buddyProjection.Args[0] != 1 || buddyProjection.Args[1] != 1 || buddyProjection.Args[5] != 5 ||
		buddyProjection.Args[6] != 1 || buddyProjection.Args[7] != 0 || buddyProjection.Args[8] != 0 {
		return fmt.Errorf("BUDDY_SLOT projection is %+v", buddyProjection)
	}
	// battle5_api_resume_data_get projects an authoritative RESUME_* snapshot.
	// It must be deterministic and read-only: replaying an attack/event history
	// here would consume RNG or apply damage twice after a socket reconnect.
	resumeEngine := &BattleEngine{
		catalog: catalog, phase: PhaseUser, turn: 3, enemyCount: 1,
		rng: newXorShift128(602), holdMax: 5,
	}
	for index := range resumeEngine.players {
		resumeEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
			MemberType: index + 1, ArthurType: index + 1,
			HP: 10000 + index, MaxHP: 12000 + index, BaseMaxHP: 12000 + index,
			Attack: 1000, BaseAttack: 1000, Magic: 900, BaseMagic: 900,
			Recovery: 800, BaseRecovery: 800, Defense: 700, BaseDefense: 700,
			MDefense: 600, BaseMDefense: 600,
			LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999,
			Cost: 5, DrawCount: 10, DrawIndex: 2,
		}
	}
	resumePlayer := &resumeEngine.players[0]
	resumePlayer.Deck[0] = BattleCard{CardType: 1, CardID: 10000010, Level: 30}
	resumePlayer.Hand[0] = 1
	resumePlayer.Buddies[0] = BattleBuddy{BuddyType: 1, BuddyID: 1000010, Level: 80}
	resumePlayer.Burst = 55
	resumePlayer.BurstState = burstGaugeNormal
	resumePlayer.Effects = []battleEffect{
		{Function: "COST_BLOCK", Value: 3, Parameters: [4]int{3}, Kind: 2, Remaining: 2, RoleIndex: 7},
		{Function: "ENCHANT", Value: 300, Attribute: "FIRE", Parameters: [4]int{300}, Kind: 1, Remaining: 2, RoleIndex: 8},
	}
	resumePlayer.BlessHolds = []battleBlessHold{{
		AppendIndex: 2, CardType: 22, Skill: CombatSkillDefinition{ID: 11107792},
		CardLevel: 60, Remaining: 3, Repeat: true, Power: 456,
	}}
	resumeEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 5, HP: 50000, MaxHP: 50000, BaseMaxHP: 50000,
		Attack: 2000, BaseAttack: 2000, Magic: 1500, BaseMagic: 1500,
		Recovery: 1000, BaseRecovery: 1000, Defense: 900, BaseDefense: 900,
		MDefense: 800, BaseMDefense: 800,
		LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999,
	}
	rngBefore := resumeEngine.rng
	resumeResults, err := resumeEngine.ResumeResults()
	if err != nil {
		return fmt.Errorf("project comeback resume snapshot: %w", err)
	}
	resumeCSV, err := codec.Group(resumeResults)
	if err != nil {
		return fmt.Errorf("encode comeback resume snapshot: %w", err)
	}
	secondResumeResults, err := resumeEngine.ResumeResults()
	if err != nil {
		return err
	}
	secondResumeCSV, err := codec.Group(secondResumeResults)
	if err != nil {
		return err
	}
	if resumeCSV != secondResumeCSV || resumeEngine.rng != rngBefore || resumePlayer.HP != 10000 || resumePlayer.DrawIndex != 2 ||
		len(resumePlayer.Effects) != 2 || len(resumePlayer.BlessHolds) != 1 {
		return errors.New("comeback resume snapshot mutated authoritative battle state")
	}
	foundResumeHand, foundResumeDeck, foundResumeBuff, foundResumeEnemy, foundResumeHold := false, false, false, false, false
	foundBuddy, foundBurstState := false, false
	if len(resumeResults) == 0 || resumeResults[0].Command != CardIdentityResult(1, resumePlayer.Deck[0]).Command {
		return errors.New("comeback resume must rebuild card identities before state")
	}
	for _, result := range resumeResults {
		switch result.Command {
		case CommandResumeTurn:
			if !equalBattleArgs(result.Args, []int64{3, 0}) {
				return fmt.Errorf("comeback resume turn is %+v", result)
			}
		case CommandResumeCardHand:
			if len(result.Args) == 11 && result.Args[0] == 1 && result.Args[1] == 1 {
				foundResumeHand = true
			}
		case CommandResumeCardDeck:
			if len(result.Args) == 2 && result.Args[0] == 1 && result.Args[1] == 8 {
				foundResumeDeck = true
			}
		case CommandResumeBuff:
			if len(result.Args) == 11 && result.Args[0] == 1 && result.Args[2] == int64(battleBuffCodes["ENCHANT"]) && result.Args[3] == 2 {
				foundResumeBuff = true
			}
		case CommandResumeEnemy:
			if len(result.Args) == 7 && result.Args[0] == 5 {
				foundResumeEnemy = true
			}
		case CommandResumeHold:
			if len(result.Args) == 8 && result.Args[0] == 1 && result.Args[1] == 22 && result.Args[6] == 2 && result.Args[7] == 60 {
				foundResumeHold = true
			}
		case CommandBuddy:
			foundBuddy = true
		case CommandBurstState:
			foundBurstState = true
		case 60, 61, CommandHPCut:
			return fmt.Errorf("comeback resume snapshot replayed damage/heal command %+v", result)
		}
	}
	if !foundResumeHand || !foundResumeDeck || !foundResumeBuff || !foundResumeEnemy || !foundResumeHold || !foundBuddy || !foundBurstState {
		return fmt.Errorf("comeback resume snapshot is incomplete: hand=%t deck=%t buff=%t enemy=%t hold=%t buddy=%t burst=%t",
			foundResumeHand, foundResumeDeck, foundResumeBuff, foundResumeEnemy, foundResumeHold, foundBuddy, foundBurstState)
	}
	// Local no-continue policy retires KO players once. The native 82 row
	// contains one int64 argument; retirement is not a synonym for HP <= 0.
	gameOverEngine := &BattleEngine{}
	for index := range gameOverEngine.players {
		gameOverEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 100}
	}
	gameOverEngine.players[0].HP = 0
	gameOverEngine.players[2].HP = 0
	gameOverRows := gameOverEngine.appendNewPlayerGameOver(nil)
	if len(gameOverRows) != 2 || gameOverRows[0].Command != CommandGameOver ||
		len(gameOverRows[0].Args) != 1 || gameOverRows[0].Args[0] != 1 ||
		gameOverRows[1].Command != CommandGameOver ||
		len(gameOverRows[1].Args) != 1 || gameOverRows[1].Args[0] != 3 ||
		len(gameOverEngine.appendNewPlayerGameOver(nil)) != 0 {
		return fmt.Errorf("native player game-over projection is %+v", gameOverRows)
	}
	gameOverEngine.players[0].HP = 100
	if len(gameOverEngine.appendNewPlayerGameOver(nil)) != 0 || !gameOverEngine.players[0].GameOver {
		return errors.New("HP update silently cleared formal game-over state")
	}
	gameOverEngine.players[0].HP = 0
	repeatedGameOver := gameOverEngine.appendNewPlayerGameOver(nil)
	if len(repeatedGameOver) != 0 {
		return fmt.Errorf("repeated post-revive game-over projection is %+v", repeatedGameOver)
	}
	// Official leader buddy 1000010 skill 65000001 selects one ATTACK card,
	// spends 45 gauge and gives that card a one-turn cost reduction. Assert the
	// exact libbattle5 ResultCmd family and order consumed by JP 8.0.7.
	burstSkillEngine := &BattleEngine{catalog: catalog, phase: PhaseUser}
	burstSkillEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 1, ArthurType: 1, HP: 10000, MaxHP: 10000,
		Burst: 100, BurstState: burstGaugeBurst,
	}
	burstSkillEngine.players[0].Buddies[0] = BattleBuddy{BuddyType: 1, BuddyID: 1000010, Level: 80}
	burstSkillEngine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 10000010, Level: 30}
	burstSkillEngine.players[0].Hand[0] = 1
	burstSkillResults, err := burstSkillEngine.ExecuteBurst(1, burstSkillSubmission{CardTypes: [5]int{1}, SkillSlot: 1})
	if err != nil {
		return fmt.Errorf("simulate official burst skill 65000001: %w", err)
	}
	wantBurstRows := []string{
		"506,1,55",
		"700,1,1,65000001,0,80,28,0,0,65000151,0,0",
		"325,1,0,1",
		"324,1,1,65000001,0",
		"27,1,1,2,1,0",
		"306,1,1,0",
		"306,1,2,0",
		"306,1,3,0",
		"306,1,4,0",
		"306,1,5,0",
		"29,1,1,4,0",
		"25,1,1,1,0,0,0,0,2106,0,0",
		"502",
		"320,1,1,0,3,45",
	}
	if len(burstSkillResults) != len(wantBurstRows) {
		return fmt.Errorf("official burst simulation returned %d results, want %d: %+v", len(burstSkillResults), len(wantBurstRows), burstSkillResults)
	}
	for index, result := range burstSkillResults {
		row, rowErr := codec.Row(result)
		if rowErr != nil {
			return rowErr
		}
		if row != wantBurstRows[index] {
			return fmt.Errorf("official burst result %d is %q, want %q", index, row, wantBurstRows[index])
		}
	}
	if burstSkillEngine.players[0].Burst != 55 || burstSkillEngine.players[0].BurstRecharge[0] != 3 ||
		burstSkillEngine.players[0].CardCostDown[1] != 1 {
		return fmt.Errorf("official burst state is gauge=%d recharge=%d cost-down=%d", burstSkillEngine.players[0].Burst,
			burstSkillEngine.players[0].BurstRecharge[0], burstSkillEngine.players[0].CardCostDown[1])
	}

	// HAND_SELECT modifiers are stored by outer burst skill ID and joined to
	// the selected official card only when UserAttack resolves its normal role
	// set. This keeps multistage/piercing out of unrelated cards in the hand.
	multistageSkill, multistageRoles, err := catalog.CardSkill(10000010, 1)
	if err != nil {
		return err
	}
	multistageEngine := &BattleEngine{catalog: catalog}
	multistageEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 1}
	multistageEngine.players[0].CardBurstSkills[1] = []battleCardBurstSkill{{SkillID: 65000420}}
	multistageAction := battleAction{memberType: 1, cardType: 1, cardLevel: 30, skill: multistageSkill, roles: multistageRoles}
	if err := multistageEngine.attachBurstCardModifiers(&multistageAction); err != nil {
		return err
	}
	multistageHits := 0
	for _, role := range multistageAction.roles {
		if role.Function == "ATTACK_AA" {
			multistageHits = combatParameterInt(role.Parameters[4])
		}
	}
	if multistageHits != 2 {
		return fmt.Errorf("official burst multistage hit count is %d, want 2", multistageHits)
	}
	piercingSkill, piercingRoles, err := catalog.CardSkill(10000046, 1)
	if err != nil {
		return err
	}
	piercingEngine := &BattleEngine{catalog: catalog}
	piercingEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 1}
	piercingEngine.players[0].CardBurstSkills[1] = []battleCardBurstSkill{{SkillID: 65000094}}
	piercingAction := battleAction{memberType: 1, cardType: 1, cardLevel: 30, skill: piercingSkill, roles: piercingRoles}
	if err := piercingEngine.attachBurstCardModifiers(&piercingAction); err != nil {
		return err
	}
	var piercingAttack CombatSkillRole
	for _, role := range piercingAction.roles {
		if role.Function == "ATTACK_AA" {
			piercingAttack = role
			break
		}
	}
	piercingRate := collectPlayerAttackModifiers(piercingAction, piercingAttack, &piercingEngine.players[0], 1, false).piercingRate
	if piercingRate != 80 {
		return fmt.Errorf("official burst piercing rate is %d, want 80", piercingRate)
	}

	// Official buddy 1000230 slot two keeps one ICE SUPPORT card, discards the
	// other four and redraws to five in the same BurstSkillExec result group.
	discardEngine := &BattleEngine{catalog: catalog, phase: PhaseUser}
	discardPlayer := &discardEngine.players[0]
	*discardPlayer = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 1, HP: 10000, MaxHP: 10000, Cost: 10, Burst: 100, BurstState: burstGaugeBurst}
	discardPlayer.Buddies[0] = BattleBuddy{BuddyType: 1, BuddyID: 1000230, Level: 80}
	for index := 0; index < len(discardPlayer.Deck); index++ {
		discardPlayer.Deck[index] = BattleCard{CardType: index + 1, CardID: 10001014, Level: 30}
		discardPlayer.DeckOrder[index] = index
		if index < len(discardPlayer.Hand) {
			discardPlayer.Hand[index] = index + 1
		}
	}
	discardPlayer.DrawCount = len(discardPlayer.Deck)
	discardPlayer.DrawIndex = len(discardPlayer.Hand)
	discardPlayer.Effects = []battleEffect{
		{Function: "CARD_TRAP_DAMAGE", CardType: 1, Remaining: 3, Kind: 2, RoleIndex: 4},
		{Function: "CARD_TRAP_DAMAGE", CardType: 2, Remaining: 3, Kind: 2, Source: 2, SourceSkillID: 222, RoleIndex: 5},
		{Function: "CARD_SEAL", CardType: 3, Remaining: 3, Kind: 2, Source: 3, SourceSkillID: 333, RoleIndex: 6},
	}
	discardResults, err := discardEngine.ExecuteBurst(1, burstSkillSubmission{CardTypes: [5]int{1}, SkillSlot: 2})
	if err != nil {
		return fmt.Errorf("simulate official burst discard-draw skill 65000380: %w", err)
	}
	// JP resets the picker with 326 before the position-preserving 323 list.
	if len(discardResults) < 11 || discardResults[9].Command != CommandCardPlayReset || discardResults[10].Command != CommandBurstDiscard ||
		discardResults[10].Args[0] != 1 || discardResults[10].Args[1] != 0 || discardResults[10].Args[2] != 2 ||
		discardResults[10].Args[3] != 3 || discardResults[10].Args[4] != 4 || discardResults[10].Args[5] != 5 {
		return fmt.Errorf("official burst discard-draw prefix is %+v", discardResults)
	}
	// 7ee5e -> 72a18 -> 47190 keeps the grouped trap status because card 1
	// remains trapped. Discarding the only sealed card still releases its kind.
	wantDiscardRelease := []int{CommandBuffLostOne, CommandBattleParam, 72, CommandBuffLostOne, CommandBattleParam}
	if len(discardResults) < 11+len(wantDiscardRelease) {
		return fmt.Errorf("official burst discard-draw omitted card-bound release rows: %+v", discardResults)
	}
	for index, command := range wantDiscardRelease {
		if discardResults[index+11].Command != command {
			return fmt.Errorf("official burst discard release command %d is %d, want %d: %+v", index, discardResults[index+11].Command, command, discardResults)
		}
	}
	if discardResults[11].Args[1] != int64(battleBuffCodes["CARD_TRAP_DAMAGE"]) || discardResults[11].Args[2] != 222 ||
		discardResults[13].Args[2] != int64(battleBuffCodes["CARD_SEAL"]) ||
		discardResults[14].Args[1] != int64(battleBuffCodes["CARD_SEAL"]) || discardResults[14].Args[2] != 333 {
		return fmt.Errorf("official burst discard release identities are %+v", discardResults[11:17])
	}
	dealt := 0
	for _, result := range discardResults {
		if result.Command == CommandCardDeal {
			dealt++
		}
	}
	if dealt != 4 || playerHandCount(discardPlayer) != 5 || discardPlayer.Burst != 10 ||
		discardPlayer.BurstRecharge[1] != 4 || len(discardPlayer.Discard) != 4 || len(discardPlayer.Effects) != 1 ||
		discardPlayer.Effects[0].CardType != 1 || discardPlayer.Effects[0].Function != "CARD_TRAP_DAMAGE" {
		return fmt.Errorf("official burst discard-draw state is dealt=%d hand=%d gauge=%d recharge=%d discard=%d",
			dealt, playerHandCount(discardPlayer), discardPlayer.Burst, discardPlayer.BurstRecharge[1], len(discardPlayer.Discard))
	}

	// Buddy 1000110 passive 64100270 gives DEF while BURST is active and adds
	// five percent per WIND card in the main deck. Ten official WIND cards turn
	// its level-80 base 11264 into 16896, then the BREAK transition removes it.
	passiveEngine := &BattleEngine{catalog: catalog, turn: 1}
	passivePlayer := &passiveEngine.players[0]
	*passivePlayer = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 1, HP: 10000, MaxHP: 10000, Burst: 100, BurstState: burstGaugeNormal}
	passivePlayer.Buddies[0] = BattleBuddy{BuddyType: 1, BuddyID: 1000110, Level: 80}
	for index := range passivePlayer.Deck {
		passivePlayer.Deck[index] = BattleCard{CardType: index + 1, CardID: 10000030, Level: 30}
	}
	passiveResults, err := passiveEngine.transitionBurstStates()
	if err != nil {
		return err
	}
	// 7ea92: first11264 + floor(11264*5/100)*10 = 16894, not floor(11264*1.5).
	if passivePlayer.BurstState != burstGaugeBurst || passivePlayer.Defense != 16894 || len(passivePlayer.Effects) != 1 ||
		!passivePlayer.Effects[0].BurstPassive || len(passiveResults) < 4 || passiveResults[0].Command != CommandBurstStateChange ||
		passiveResults[1].Command != CommandBurstSkill {
		return fmt.Errorf("official burst passive entry is state=%d defense=%d effects=%+v results=%+v",
			passivePlayer.BurstState, passivePlayer.Defense, passivePlayer.Effects, passiveResults)
	}
	passivePlayer.Burst = 0
	releasePassiveResults, err := passiveEngine.transitionBurstStates()
	if err != nil {
		return err
	}
	if passivePlayer.BurstState != burstGaugeBreak || passivePlayer.Defense != 0 || len(passivePlayer.Effects) != 0 ||
		len(releasePassiveResults) != 4 || releasePassiveResults[0].Command != 72 || releasePassiveResults[0].Args[1] != 6 ||
		releasePassiveResults[1].Command != CommandBaseParam || releasePassiveResults[2].Command != CommandBattleParam ||
		releasePassiveResults[3].Command != CommandBurstStateChange {
		return fmt.Errorf("official burst passive release is state=%d defense=%d effects=%+v results=%+v",
			passivePlayer.BurstState, passivePlayer.Defense, passivePlayer.Effects, releasePassiveResults)
	}
	if err := validateAppendConditionContracts(catalog); err != nil {
		return err
	}
	if err := validateEnemyAppendCardContracts(catalog); err != nil {
		return err
	}
	engine := &BattleEngine{turn: 1}
	engine.catalog = catalog
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, ArthurType: index + 1, HP: 100, MaxHP: 100}
	}
	// Official card 10131011 (skill 12502222) declares USER_ALL at the skill
	// level and SELECT in each role. Use its exact zero-based role index for the
	// retained lifecycle as well as the group target contract below; the former
	// hand-written SELF role projected index 2 instead of the official index 1.
	skill, roles, err := catalog.CardSkill(10131011, 2)
	if err != nil {
		return fmt.Errorf("load official draw contract card: %w", err)
	}
	var drawRole *CombatSkillRole
	for index := range roles {
		if roles[index].Function == "DEAL_BONUS" {
			drawRole = &roles[index]
			break
		}
	}
	if skill.Target != "USER_ALL" || drawRole == nil || drawRole.Target != "SELECT" ||
		drawRole.RoleIndex != 1 || drawRole.Parameters[0] != "1" {
		return fmt.Errorf("official draw contract card target/role shape changed: skill=%+v role=%+v", skill, drawRole)
	}
	role := *drawRole
	results, err := engine.executeDealChange(battleAction{memberType: 1, target: 1}, role, false)
	if err != nil {
		return err
	}
	if len(results) != 1 {
		return fmt.Errorf("deal bonus simulation returned %d results, want 1", len(results))
	}
	row, err := codec.Row(results[0])
	if err != nil {
		return err
	}
	if row != "62,1,1,0,400,3,0,1,1,0,0,0" {
		return fmt.Errorf("deal bonus ResultCmd projection is %q", row)
	}
	engine.turn = 2
	release, tickErr := expireForEffectContract(engine)
	if tickErr != nil {
		return tickErr
	}
	if len(release) != 0 {
		return errors.New("deal bonus emitted a premature release on its active UserPhase")
	}
	if value := playerDrawEffectValue(&engine.players[0], battleBuffCodes["DEAL_BONUS"]); value != 1 {
		return fmt.Errorf("deal bonus active value is %d, want 1", value)
	}
	engine.turn = 3
	release, tickErr = expireForEffectContract(engine)
	if tickErr != nil {
		return tickErr
	}
	if len(release) != 1 || release[0].Command != 72 || release[0].Args[2] != int64(battleBuffCodes["DEAL_BONUS"]) {
		return errors.New("deal bonus lost its natural-expiry release")
	}
	if value := playerDrawEffectValue(&engine.players[0], battleBuffCodes["DEAL_BONUS"]); value != 0 {
		return fmt.Errorf("expired deal bonus value is %d, want 0", value)
	}

	player := battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1}
	base := battleEffect{Function: "DEAL_BONUS", Value: 1, Remaining: 2, AppliedTurn: 1}
	if !applyPlayerDrawEffect(&player, base, battleBuffCodes["DEAL_BONUS"]) {
		return errors.New("initial deal bonus was rejected")
	}
	if applyPlayerDrawEffect(&player, base, battleBuffCodes["DEAL_BONUS"]) {
		return errors.New("native replacement gate accepted an equal active deal bonus")
	}
	stronger := base
	stronger.Value = 2
	if !applyPlayerDrawEffect(&player, stronger, battleBuffCodes["DEAL_BONUS"]) || len(player.Effects) != 1 || player.Effects[0].Value != 2 {
		return errors.New("native replacement gate failed to replace deal bonus with stronger value")
	}

	// Native resolves the official SELECT role to the already-selected skill
	// target; target zero therefore means all users, not an empty selection.
	for index := range engine.players {
		engine.players[index].Effects = nil
	}
	group, err := engine.executePlayerRole(battleAction{memberType: 2, cardID: 10131011, cardLevel: 60, target: 0, skill: skill, roles: roles}, *drawRole, 1)
	if err != nil {
		return err
	}
	if len(group) != maxRoomMembers*2 {
		return fmt.Errorf("official USER_ALL draw simulation returned %d results, want %d", len(group), maxRoomMembers*2)
	}
	for index := range engine.players {
		if playerDrawEffectValue(&engine.players[index], battleBuffCodes["DEAL_BONUS"]) != 1 {
			return fmt.Errorf("official USER_ALL draw simulation omitted member %d", index+1)
		}
		if group[index*2].Command != CommandBuff || group[index*2].Args[0] != int64(index+1) ||
			group[index*2+1].Command != CommandBattleParam || group[index*2+1].Args[0] != int64(index+1) {
			return fmt.Errorf("official USER_ALL draw omitted per-target parameter snapshot: %+v", group)
		}
	}

	// The 13 active enemy DEAL_BONUS rows use the same retained draw owner but
	// enter through an enemy USER_ALL skill whose role target is SELECT. Exercise
	// the complete outer-target inheritance and next-UserPhase card consumer,
	// rather than treating the player card contract above as side-independent.
	enemyDrawVariants := catalog.Skills[44188006]
	var enemyDrawRole *CombatSkillRole
	for index := range catalog.SkillRoles[44188006] {
		candidate := &catalog.SkillRoles[44188006][index]
		if candidate.Function == "DEAL_BONUS" {
			enemyDrawRole = candidate
			break
		}
	}
	if len(enemyDrawVariants) != 1 || enemyDrawVariants[0].Target != "USER_ALL" ||
		enemyDrawRole == nil || enemyDrawRole.Target != "SELECT" || enemyDrawRole.Parameters[0] != "1" {
		return fmt.Errorf("official enemy DEAL_BONUS target/value changed: skill=%+v role=%+v", enemyDrawVariants, enemyDrawRole)
	}
	newDrawConsumerEngine := func() *BattleEngine {
		value := &BattleEngine{catalog: catalog, turn: 1, phase: PhaseTurn, enemyCount: 1, rng: newXorShift128(1)}
		value.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Attack: 1000}
		for playerIndex := range value.players {
			player := &value.players[playerIndex]
			*player = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: playerIndex + 1, ArthurType: 2, HP: 10000, MaxHP: 10000, DrawCount: 10, DrawIndex: 1}
			for deckIndex := range player.Deck {
				player.Deck[deckIndex] = BattleCard{CardType: deckIndex + 1, CardID: 10131011, Level: 60}
				player.DeckOrder[deckIndex] = deckIndex
			}
			player.Hand[0] = 1
		}
		return value
	}
	enemyDrawEngine := newDrawConsumerEngine()
	enemyDrawResults, err := enemyDrawEngine.executeEnemyActionCandidate(
		&enemyDrawEngine.enemies[0],
		enemyActionCandidate{action: CombatEnemyAction{SkillID: 44188006, Target: "USER_ALL"}},
	)
	if err != nil {
		return err
	}
	enemyDrawBuffRows := 0
	for _, result := range enemyDrawResults {
		if result.Command == CommandBuff && len(result.Args) >= 4 &&
			result.Args[1] == int64(enemyDrawRole.RoleIndex) &&
			result.Args[3] == int64(battleBuffCodes["DEAL_BONUS"]) {
			enemyDrawBuffRows++
		}
	}
	if enemyDrawBuffRows != maxRoomMembers {
		return fmt.Errorf("official enemy DEAL_BONUS projected %d buff rows, want %d: %+v", enemyDrawBuffRows, maxRoomMembers, enemyDrawResults)
	}
	for index := range enemyDrawEngine.players {
		player := &enemyDrawEngine.players[index]
		var drawEffect *battleEffect
		for effectIndex := range player.Effects {
			if player.Effects[effectIndex].Function == "DEAL_BONUS" {
				drawEffect = &player.Effects[effectIndex]
				break
			}
		}
		if drawEffect == nil || drawEffect.Value != 1 || drawEffect.Remaining != 2 ||
			drawEffect.Source != 5 || drawEffect.Kind != 1 {
			return fmt.Errorf("official enemy DEAL_BONUS member %d state is %+v", index+1, *player)
		}
	}
	enemyDrawEngine.turn = 2
	if release, tickErr := expireForEffectContract(enemyDrawEngine); tickErr != nil || len(release) != 0 {
		return fmt.Errorf("official enemy DEAL_BONUS next-turn tick is release=%+v err=%v", release, tickErr)
	}
	enemyDrawEngine.phase = PhaseTurn
	enemyDrawEngine.prepareTurnDraw()
	enemyDrawUserPhase, err := enemyDrawEngine.UserPhase()
	if err != nil {
		return err
	}
	dealtByMember := [maxRoomMembers]int{}
	for _, result := range enemyDrawUserPhase {
		if result.Command == CommandCardDeal && len(result.Args) >= 1 && result.Args[0] >= 1 && result.Args[0] <= maxRoomMembers {
			dealtByMember[result.Args[0]-1]++
		}
	}
	for index := range enemyDrawEngine.players {
		player := &enemyDrawEngine.players[index]
		drawRemaining := 0
		for _, effect := range player.Effects {
			if effect.Function == "DEAL_BONUS" {
				drawRemaining = effect.Remaining
			}
		}
		if dealtByMember[index] != 2 || player.DrawIndex != 3 || drawRemaining != 1 {
			return fmt.Errorf("official enemy DEAL_BONUS member %d drew=%d draw_index=%d effects=%+v", index+1, dealtByMember[index], player.DrawIndex, player.Effects)
		}
	}

	// Finish the ordinary draw-change family with both penalty entry sides.
	// Player skill 13603322 targets enemies for its parameter breaks but owns a
	// separate SELF draw penalty. Enemy skill 32001120 attacks one selected user
	// and applies the ordinary penalty to that same concrete target.
	playerPenaltyVariants := catalog.Skills[13603322]
	var playerPenaltyRole *CombatSkillRole
	for index := range catalog.SkillRoles[13603322] {
		candidate := &catalog.SkillRoles[13603322][index]
		if candidate.Function == "DEAL_PENALTY" {
			playerPenaltyRole = candidate
			break
		}
	}
	if len(playerPenaltyVariants) != 1 || playerPenaltyVariants[0].Target != "ENEMY_ALL" ||
		playerPenaltyRole == nil || playerPenaltyRole.Target != "SELF" || playerPenaltyRole.Parameters[0] != "3" {
		return fmt.Errorf("official player DEAL_PENALTY target/value changed: skill=%+v role=%+v", playerPenaltyVariants, playerPenaltyRole)
	}
	playerPenaltyEngine := newDrawConsumerEngine()
	playerPenaltyResults, err := playerPenaltyEngine.executePlayerRole(
		battleAction{memberType: 1, cardID: 10163009, cardLevel: 60, target: 0, skill: playerPenaltyVariants[0], roles: catalog.SkillRoles[13603322]},
		*playerPenaltyRole,
		1,
	)
	if err != nil {
		return err
	}
	if len(playerPenaltyResults) != 2 || playerPenaltyResults[0].Command != CommandBuff ||
		len(playerPenaltyResults[0].Args) < 8 || playerPenaltyResults[0].Args[0] != 1 ||
		playerPenaltyResults[0].Args[1] != int64(playerPenaltyRole.RoleIndex) ||
		playerPenaltyResults[0].Args[3] != int64(battleBuffCodes["DEAL_PENALTY"]) ||
		playerPenaltyResults[0].Args[7] != 3 || playerPenaltyResults[1].Command != CommandBattleParam ||
		!equalBattleArgs(playerPenaltyResults[1].Args, battleParameterArgs(1, 10000, 10000, 0, 0, 0, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("official player DEAL_PENALTY projection is %+v", playerPenaltyResults)
	}
	playerPenaltyEffect := playerPenaltyEngine.players[0].Effects[0]
	if playerPenaltyEffect.Function != "DEAL_PENALTY" || playerPenaltyEffect.Value != 3 ||
		playerPenaltyEffect.Remaining != 2 || playerPenaltyEffect.Source != 1 || playerPenaltyEffect.Kind != 2 {
		return fmt.Errorf("official player DEAL_PENALTY state is %+v", playerPenaltyEngine.players[0])
	}
	playerPenaltyEngine.turn = 2
	if release, tickErr := expireForEffectContract(playerPenaltyEngine); tickErr != nil || len(release) != 0 {
		return fmt.Errorf("official player DEAL_PENALTY next-turn tick is release=%+v err=%v", release, tickErr)
	}
	playerPenaltyEngine.phase = PhaseTurn
	playerPenaltyEngine.prepareTurnDraw()
	playerPenaltyUserPhase, err := playerPenaltyEngine.UserPhase()
	if err != nil {
		return err
	}
	playerPenaltyDeals := [maxRoomMembers]int{}
	for _, result := range playerPenaltyUserPhase {
		if result.Command == CommandCardDeal && len(result.Args) >= 1 && result.Args[0] >= 1 && result.Args[0] <= maxRoomMembers {
			playerPenaltyDeals[result.Args[0]-1]++
		}
	}
	if playerPenaltyDeals != [maxRoomMembers]int{0, 1, 1, 1} || playerPenaltyEngine.players[0].DrawIndex != 1 ||
		playerPenaltyEngine.players[0].Effects[0].Remaining != 1 {
		return fmt.Errorf("official player DEAL_PENALTY draws=%v player=%+v", playerPenaltyDeals, playerPenaltyEngine.players[0])
	}

	enemyPenaltyVariants := catalog.Skills[32001120]
	var enemyPenaltyRole *CombatSkillRole
	for index := range catalog.SkillRoles[32001120] {
		candidate := &catalog.SkillRoles[32001120][index]
		if candidate.Function == "DEAL_PENALTY" {
			enemyPenaltyRole = candidate
			break
		}
	}
	if len(enemyPenaltyVariants) != 1 || enemyPenaltyVariants[0].Target != "USER_ONE" ||
		enemyPenaltyRole == nil || enemyPenaltyRole.Target != "SELECT" || enemyPenaltyRole.Parameters[0] != "1" {
		return fmt.Errorf("official enemy DEAL_PENALTY target/value changed: skill=%+v role=%+v", enemyPenaltyVariants, enemyPenaltyRole)
	}
	enemyPenaltyEngine := newDrawConsumerEngine()
	enemyPenaltyResults, err := enemyPenaltyEngine.executeEnemyActionCandidate(
		&enemyPenaltyEngine.enemies[0],
		enemyActionCandidate{action: CombatEnemyAction{SkillID: 32001120, Target: "USER_ONE"}},
	)
	if err != nil {
		return err
	}
	penalizedMember := 0
	penaltyBuffRows := 0
	for _, result := range enemyPenaltyResults {
		if result.Command == CommandBuff && len(result.Args) >= 4 &&
			result.Args[1] == int64(enemyPenaltyRole.RoleIndex) &&
			result.Args[3] == int64(battleBuffCodes["DEAL_PENALTY"]) {
			penalizedMember = int(result.Args[0])
			penaltyBuffRows++
		}
	}
	if penaltyBuffRows != 1 || penalizedMember < 1 || penalizedMember > maxRoomMembers {
		return fmt.Errorf("official enemy DEAL_PENALTY target rows=%d member=%d results=%+v", penaltyBuffRows, penalizedMember, enemyPenaltyResults)
	}
	penaltyEffect := enemyPenaltyEngine.players[penalizedMember-1].Effects[0]
	if penaltyEffect.Function != "DEAL_PENALTY" || penaltyEffect.Value != 1 || penaltyEffect.Remaining != 2 ||
		penaltyEffect.Source != 5 || penaltyEffect.Kind != 2 {
		return fmt.Errorf("official enemy DEAL_PENALTY state is %+v", enemyPenaltyEngine.players[penalizedMember-1])
	}
	enemyPenaltyEngine.turn = 2
	if release, tickErr := expireForEffectContract(enemyPenaltyEngine); tickErr != nil || len(release) != 0 {
		return fmt.Errorf("official enemy DEAL_PENALTY next-turn tick is release=%+v err=%v", release, tickErr)
	}
	enemyPenaltyEngine.phase = PhaseTurn
	enemyPenaltyEngine.prepareTurnDraw()
	enemyPenaltyUserPhase, err := enemyPenaltyEngine.UserPhase()
	if err != nil {
		return err
	}
	enemyPenaltyDeals := [maxRoomMembers]int{}
	for _, result := range enemyPenaltyUserPhase {
		if result.Command == CommandCardDeal && len(result.Args) >= 1 && result.Args[0] >= 1 && result.Args[0] <= maxRoomMembers {
			enemyPenaltyDeals[result.Args[0]-1]++
		}
	}
	for index := range enemyPenaltyDeals {
		want := 1
		if index == penalizedMember-1 {
			want = 0
		}
		if enemyPenaltyDeals[index] != want {
			return fmt.Errorf("official enemy DEAL_PENALTY member %d drew=%d, want %d; selected=%d", index+1, enemyPenaltyDeals[index], want, penalizedMember)
		}
	}
	if enemyPenaltyEngine.players[penalizedMember-1].DrawIndex != 1 ||
		enemyPenaltyEngine.players[penalizedMember-1].Effects[0].Remaining != 1 {
		return fmt.Errorf("official enemy DEAL_PENALTY selected state is %+v", enemyPenaltyEngine.players[penalizedMember-1])
	}

	deck := make([]BattleCard, 10)
	for index := range deck {
		deck[index] = BattleCard{CardType: index + 1, CardID: 10131011, Level: 60}
	}
	members := make([]Participant, maxRoomMembers)
	for index := range members {
		members[index] = Participant{MemberType: index + 1, ArthurType: index + 1, HP: 100000, Attack: 10000, Magic: 10000, Mind: 10000, DeckCards: append([]BattleCard(nil), deck...)}
	}
	bossEngine, err := NewBattleEngine(catalog, Config{EnemyPartyID: 30830102, Seed: 1, CostInitial: 3, HoldMax: 5}, members)
	if err != nil {
		return fmt.Errorf("load official special-boss simulation: %w", err)
	}
	if bossEngine.enemyCount != 2 || bossEngine.enemies[0].EnemyID != 30830121 || bossEngine.enemies[1].EnemyID != 30830122 {
		return errors.New("official special-boss party shape changed")
	}
	bossEngine.turn = 1
	bossEngine.enemies[1].HP = 0
	bossEngine.enemies[1].Broken = true
	action, target, found := bossEngine.selectEnemyAction(&bossEngine.enemies[0])
	if !found || action.SkillID != 35101102 || target != bossEngine.enemies[1].MemberType {
		return fmt.Errorf("special-boss broken-part selection is skill %d target %d found %t", action.SkillID, target, found)
	}
	roles = catalog.SkillRoles[action.SkillID]
	for _, enemyRole := range roles {
		if _, err := bossEngine.executeEnemyRole(&bossEngine.enemies[0], target, enemyRole, roles); err != nil {
			return fmt.Errorf("simulate official special-boss role %s: %w", enemyRole.Function, err)
		}
	}
	wantedReviveHP := bossEngine.enemies[1].MaxHP * 600 / 1000
	if bossEngine.enemies[1].HP != wantedReviveHP || bossEngine.enemies[1].Broken {
		return fmt.Errorf("special-boss revive state is hp %d broken %t, want hp %d", bossEngine.enemies[1].HP, bossEngine.enemies[1].Broken, wantedReviveHP)
	}
	foundDarkDefenseDown := false
	for _, effect := range bossEngine.enemies[0].Effects {
		if effect.Function == "ATTR_DEF_DOWN" && effect.Attribute == "DARK" && effect.Value == 5000 && effect.Remaining == 99 {
			foundDarkDefenseDown = true
		}
	}
	if !foundDarkDefenseDown || !bossEngine.enemies[0].hasAIFlag(1) {
		return fmt.Errorf("special-boss follow-up state is dark-defense-down %t flags %b", foundDarkDefenseDown, bossEngine.enemies[0].AIFlags)
	}
	baseDarkRate := enemyAttributeRate(&bossEngine.enemies[0], "DARK")
	if rate := bossEngine.enemyAttributeRateWithEffects(&bossEngine.enemies[0], "DARK"); rate != baseDarkRate {
		return fmt.Errorf("special-boss fixed dark-defense-down changed elemental rate to %d, want %d", rate, baseDarkRate)
	}
	if primary, fixed := attributeDefenseAdjustment(bossEngine.enemies[0].Effects, "DARK"); primary != 0 || fixed != 5000 {
		return fmt.Errorf("special-boss dark-defense-down aggregate is primary=%d fixed=%d, want 0/5000", primary, fixed)
	}

	// Native DARKNESS_APPOINT packs p1..p5 into bit4..bit0. Official skill
	// 31901114 requests slots 2 and 4, hence 01010b, and a repeated status
	// must not reapply already-darkened slots.
	darkEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	for index := range darkEngine.players {
		darkEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	var darknessRole *CombatSkillRole
	for index := range catalog.SkillRoles[31901114] {
		candidate := &catalog.SkillRoles[31901114][index]
		if candidate.Function == "DARKNESS_APPOINT" {
			darknessRole = candidate
			break
		}
	}
	if darknessRole == nil || darknessRole.Parameters[1] != "0" || darknessRole.Parameters[2] != "1" || darknessRole.Parameters[3] != "0" || darknessRole.Parameters[4] != "1" || darknessRole.Parameters[5] != "0" {
		return errors.New("official appointed-darkness contract changed")
	}
	darkActor := &battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5}
	darkResults, err := darkEngine.executeEnemyPersistentEffect(darkActor, 1, *darknessRole)
	if err != nil {
		return err
	}
	if len(darkResults) != 1 || len(darkEngine.players[0].Effects) != 1 || darkEngine.players[0].Effects[0].Mask != 0b01010 {
		return fmt.Errorf("appointed darkness did not produce native mask 01010b")
	}
	for slot := 0; slot < 5; slot++ {
		darkEngine.players[0].Deck[slot] = BattleCard{CardType: slot + 1, CardID: 10131011, Level: 60}
		darkEngine.players[0].Hand[slot] = slot + 1
	}
	planRows, err := darkEngine.CardPlayPlanResults(1, cardPlaySubmission{CardTypes: [5]int{2, 3}, Targets: [5]int{5, 5}})
	if err != nil {
		return err
	}
	darkPlan, err := codec.Group(planRows)
	if err != nil {
		return err
	}
	if darkPlan != "303,1,2,5,0,0,1,0\n303,1,3,5,0,0,0,0" {
		return fmt.Errorf("appointed darkness CARD_PLAY_PLAN projection is %q", darkPlan)
	}
	repeatedDarkness, err := darkEngine.executeEnemyPersistentEffect(darkActor, 1, *darknessRole)
	if err != nil {
		return err
	}
	if len(repeatedDarkness) != 1 || repeatedDarkness[0].Command != CommandDebuffFailed || len(repeatedDarkness[0].Args) != 4 ||
		repeatedDarkness[0].Args[0] != 1 || repeatedDarkness[0].Args[1] != int64(darknessRole.RoleIndex) ||
		repeatedDarkness[0].Args[2] != int64(battleBuffCodes["DARKNESS_APPOINT"]) || repeatedDarkness[0].Args[3] != 2 ||
		len(darkEngine.players[0].Effects) != 1 {
		return fmt.Errorf("appointed darkness duplicate projection/state is results=%+v effects=%+v", repeatedDarkness, darkEngine.players[0].Effects)
	}

	// DARKNESS_RANDOM has a distinct native RNG contract. FUN_000a3812 rolls
	// p1..p2 even for an equal bound before FUN_0008fb80 rolls resistance, then
	// the consumer samples ascending bit positions by replacing the chosen item
	// with the last candidate. Seed 1 and official fixed-count role 32201316
	// therefore select bits 1 and 2 (mask 00110b) and leave the fifth xor128
	// value as the next room draw.
	var randomDarknessRole *CombatSkillRole
	for index := range catalog.SkillRoles[32201316] {
		candidate := &catalog.SkillRoles[32201316][index]
		if candidate.Function == "DARKNESS_RANDOM" {
			randomDarknessRole = candidate
			break
		}
	}
	if randomDarknessRole == nil || randomDarknessRole.Parameters[0] != "5" || randomDarknessRole.Parameters[1] != "2" || randomDarknessRole.Parameters[2] != "2" {
		return errors.New("official random-darkness contract changed")
	}
	randomDarkEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	randomDarkEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000}
	randomDarkResults, err := randomDarkEngine.executeEnemyPersistentEffect(darkActor, 1, *randomDarknessRole)
	if err != nil {
		return err
	}
	if len(randomDarkResults) != 1 || randomDarkResults[0].Command != CommandBuff ||
		len(randomDarkEngine.players[0].Effects) != 1 || randomDarkEngine.players[0].Effects[0].Mask != 0b00110 ||
		randomDarkEngine.players[0].Effects[0].Remaining != 5 {
		return fmt.Errorf("random darkness native sampling is results=%+v effects=%+v", randomDarkResults, randomDarkEngine.players[0].Effects)
	}
	if next := randomDarkEngine.rng.next(); next != 394756926 {
		return fmt.Errorf("random darkness consumed the wrong xor128 draws; next=%d, want 394756926", next)
	}
	// FRIEND_ALL still has one producer action record. Count is drawn once,
	// then each target independently consumes resistance and swap-last samples.
	var allDarknessRole *CombatSkillRole
	for index := range catalog.SkillRoles[38001111] {
		candidate := &catalog.SkillRoles[38001111][index]
		if candidate.Function == "DARKNESS_RANDOM" {
			allDarknessRole = candidate
			break
		}
	}
	if allDarknessRole == nil || allDarknessRole.Target != "FRIEND_ALL" || allDarknessRole.Parameters[1] != "2" || allDarknessRole.Parameters[2] != "2" {
		return errors.New("official all-player random-darkness contract changed")
	}
	allDarkEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	for index := range allDarkEngine.players {
		allDarkEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	allDarkResults, err := allDarkEngine.executeEnemyPersistentEffect(darkActor, 1, *allDarknessRole)
	if err != nil {
		return err
	}
	wantDarkMasks := [...]int{0b00110, 0b01100, 0b01001, 0b01100}
	if len(allDarkResults) != len(wantDarkMasks) {
		return fmt.Errorf("all-player random darkness projected %d results, want %d", len(allDarkResults), len(wantDarkMasks))
	}
	for index, wantMask := range wantDarkMasks {
		if len(allDarkEngine.players[index].Effects) != 1 || allDarkEngine.players[index].Effects[0].Mask != wantMask {
			return fmt.Errorf("all-player random darkness member %d is %+v, want mask %05b", index+1, allDarkEngine.players[index].Effects, wantMask)
		}
	}
	if next := allDarkEngine.rng.next(); next != 1587371241 {
		return fmt.Errorf("all-player random darkness re-rolled producer count; next=%d, want 1587371241", next)
	}

	// Native CARD_TRAP_DAMAGE binds the status to selected hand cards. Damage
	// is consumed exactly once when that card is played; CARD_STATE(29)
	// publishes remaining duration, not damage. UserPhase alone does no damage.
	trapEngine := &BattleEngine{catalog: catalog, turn: 1, phase: PhaseTurn, rng: newXorShift128(1)}
	for index := range trapEngine.players {
		trapEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, ArthurType: 2, HP: 10000, MaxHP: 10000}
	}
	for index := 0; index < 5; index++ {
		trapEngine.players[0].Deck[index] = BattleCard{CardType: index + 1, CardID: 10131011, Level: 60}
		trapEngine.players[0].Hand[index] = index + 1
	}
	var trapRole *CombatSkillRole
	for index := range catalog.SkillRoles[32201107] {
		candidate := &catalog.SkillRoles[32201107][index]
		if candidate.Function == "CARD_TRAP_DAMAGE" {
			trapRole = candidate
			break
		}
	}
	if trapRole == nil || trapRole.Parameters[1] != "1" || trapRole.Parameters[2] != "1" || trapRole.Parameters[3] != "1000" {
		return errors.New("official card-trap contract changed")
	}
	trapActor := &battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5}
	trapExecutionRole := *trapRole
	trapExecutionRole.SourceSkillID = 32201107
	if _, err := trapEngine.executeEnemyPersistentEffect(trapActor, 1, trapExecutionRole); err != nil {
		return err
	}
	trappedCard := 0
	for _, effect := range trapEngine.players[0].Effects {
		if effect.Function == "CARD_TRAP_DAMAGE" {
			if trappedCard != 0 || effect.Value != 1000 || effect.CardType < 1 || effect.CardType > 5 {
				return errors.New("card trap did not bind exactly one official hand card for 1000 damage")
			}
			trappedCard = effect.CardType
		}
	}
	if trappedCard != 2 {
		return fmt.Errorf("card trap native producer/sample selected card %d, want 2", trappedCard)
	}
	if next := trapEngine.rng.next(); next != 2618378627 {
		return fmt.Errorf("card trap consumed the wrong xor128 draws; next=%d, want 2618378627", next)
	}
	hpBeforeUserPhase := trapEngine.players[0].HP
	userPhaseResults, err := trapEngine.UserPhase()
	if err != nil {
		return err
	}
	if trapEngine.players[0].HP != hpBeforeUserPhase {
		return errors.New("card trap incorrectly dealt damage at UserPhase")
	}
	foundCardState := false
	for _, result := range userPhaseResults {
		if result.Command == CommandCardState && len(result.Args) >= 4 && result.Args[0] == 1 && result.Args[1] == int64(trappedCard) {
			foundCardState = result.Args[2]&2 != 0 && result.Args[3] == int64(combatParameterInt(trapRole.Parameters[0]))
		}
	}
	if !foundCardState {
		return errors.New("card trap CARD_STATE projection omitted state bit or remaining duration")
	}
	triggered := trapEngine.triggerCardTrap(1, trappedCard)
	wantTrapCommands := []int{CommandBuffEffect, 60, 72, CommandBuffLostOne, CommandBattleParam}
	if len(triggered) != len(wantTrapCommands) || triggered[0].Command != CommandBuffEffect || len(triggered[0].Args) != 3 ||
		triggered[0].Args[0] != 1 || triggered[0].Args[1] != 1 ||
		triggered[0].Args[2] != int64(battleBuffCodes["CARD_TRAP_DAMAGE"]) ||
		triggered[2].Args[0] != 1 || triggered[2].Args[2] != int64(battleBuffCodes["CARD_TRAP_DAMAGE"]) ||
		triggered[3].Args[0] != 1 || triggered[3].Args[1] != int64(battleBuffCodes["CARD_TRAP_DAMAGE"]) ||
		triggered[3].Args[2] != 32201107 ||
		trapEngine.players[0].HP != hpBeforeUserPhase-1000 {
		return fmt.Errorf("playing trapped card did not complete native one-shot lifecycle: %+v", triggered)
	}
	for index, command := range wantTrapCommands {
		if triggered[index].Command != command {
			return fmt.Errorf("card-trap command %d is %d, want %d: %+v", index, triggered[index].Command, command, triggered)
		}
	}
	if repeated := trapEngine.triggerCardTrap(1, trappedCard); len(repeated) != 0 || trapEngine.players[0].HP != hpBeforeUserPhase-1000 {
		return errors.New("consumed card trap triggered more than once")
	}

	// Native FUN_000851e0/FUN_00087210 consume one xor128 draw even for a
	// 100% ALL/ONE release. ALL projects the fixed all-buff/all-debuff
	// sentinel, while ONE projects the requested kinds that existed before
	// removal (including a duplicated p2/p3 kind).
	var allBuffRelease *CombatSkillRole
	for index := range catalog.SkillRoles[11107252] {
		candidate := &catalog.SkillRoles[11107252][index]
		if candidate.Function == "BUFF_RELEASE" {
			allBuffRelease = candidate
			break
		}
	}
	if allBuffRelease == nil || allBuffRelease.Parameters[0] != "100" {
		return errors.New("official all-buff-release contract changed")
	}
	allBuffEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	allBuffPlayer := battlePlayer{Effects: []battleEffect{{Function: "CRITICAL_UP", Kind: 1, Remaining: 2}}}
	allBuffRemoved := allBuffEngine.releasePlayerEffects(&allBuffPlayer, *allBuffRelease, 1)
	allBuffResult := battleBuffReleaseResult(1, *allBuffRelease, allBuffRemoved)
	if len(allBuffRemoved) != 1 || len(allBuffResult.Args) != 8 || allBuffResult.Args[2] != 84 {
		return fmt.Errorf("all-buff release did not project native sentinel 84: removed=%+v result=%+v", allBuffRemoved, allBuffResult)
	}
	if next := allBuffEngine.rng.next(); next != 442046446 {
		return fmt.Errorf("100%% all-buff release skipped xor128; next=%d, want 442046446", next)
	}

	var allDebuffRelease *CombatSkillRole
	for index := range catalog.SkillRoles[30901113] {
		candidate := &catalog.SkillRoles[30901113][index]
		if candidate.Function == "DEBUFF_RELEASE" {
			allDebuffRelease = candidate
			break
		}
	}
	if allDebuffRelease == nil || allDebuffRelease.Parameters[0] != "100" {
		return errors.New("official all-debuff-release contract changed")
	}
	allDebuffEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	allDebuffPlayer := battlePlayer{Effects: []battleEffect{{Function: "BURN", Kind: 2, Remaining: 2}}}
	allDebuffRemoved := allDebuffEngine.releasePlayerEffects(&allDebuffPlayer, *allDebuffRelease, 0)
	allDebuffResult := battleBuffReleaseResult(1, *allDebuffRelease, allDebuffRemoved)
	if len(allDebuffRemoved) != 1 || len(allDebuffResult.Args) != 8 || allDebuffResult.Args[5] != 33 {
		return fmt.Errorf("all-debuff release did not project native sentinel 33: removed=%+v result=%+v", allDebuffRemoved, allDebuffResult)
	}
	if next := allDebuffEngine.rng.next(); next != 442046446 {
		return fmt.Errorf("100%% all-debuff release skipped xor128; next=%d, want 442046446", next)
	}

	var duplicateOneRelease *CombatSkillRole
	for index := range catalog.SkillRoles[99990118] {
		candidate := &catalog.SkillRoles[99990118][index]
		if candidate.Function == "DEBUFF_RELEASE_ONE" && candidate.Parameters[2] == "WEAKNESS" && candidate.Parameters[3] == "WEAKNESS" {
			duplicateOneRelease = candidate
			break
		}
	}
	if duplicateOneRelease == nil {
		return errors.New("official duplicate one-debuff-release contract changed")
	}
	duplicateOneEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	duplicateOnePlayer := battlePlayer{Effects: []battleEffect{{Function: "WEAKNESS", Kind: 2, Remaining: 2}}}
	duplicateOneRemoved := duplicateOneEngine.releasePlayerEffects(&duplicateOnePlayer, *duplicateOneRelease, 1)
	duplicateOneResult := battleBuffReleaseResult(1, *duplicateOneRelease, duplicateOneRemoved)
	if len(duplicateOneRemoved) != 1 || len(duplicateOneResult.Args) != 8 || duplicateOneResult.Args[5] != 19 || duplicateOneResult.Args[6] != 19 {
		return fmt.Errorf("duplicated one-debuff kind was not preserved: removed=%+v result=%+v", duplicateOneRemoved, duplicateOneResult)
	}
	if next := duplicateOneEngine.rng.next(); next != 442046446 {
		return fmt.Errorf("100%% one-debuff release skipped xor128; next=%d, want 442046446", next)
	}

	// Close both active sides of DEBUFF_RELEASE_ONE through their real target
	// domains and child lifecycle. The earlier duplicate-kind probe only tested
	// the selector helper and did not prove player SELF or enemy ENEMY_ALL
	// execution through ResultCmd66 -> 72 -> 71 -> 6.

	var playerSelfSealRelease *CombatSkillRole
	for index := range catalog.SkillRoles[11101312] {
		candidate := &catalog.SkillRoles[11101312][index]
		if candidate.Function == "DEBUFF_RELEASE_ONE" {
			playerSelfSealRelease = candidate
			break
		}
	}
	playerSelfReleaseSkills := catalog.Skills[11101312]
	if playerSelfSealRelease == nil || playerSelfSealRelease.RoleIndex != 1 || playerSelfSealRelease.Target != "SELF" ||
		playerSelfSealRelease.Parameters[2] != "CARD_SEAL" || len(playerSelfReleaseSkills) == 0 ||
		playerSelfReleaseSkills[0].Target != "ENEMY_ALL" {
		return errors.New("official player SELF card-seal release contract changed")
	}
	playerSelfReleaseEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	for index := range playerSelfReleaseEngine.players {
		playerSelfReleaseEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	playerSelfReleaseEngine.players[0].Effects = []battleEffect{
		{Function: "CARD_SEAL", CardType: 3, Kind: 2, Remaining: 3, RoleIndex: 7, Source: 5, SourceSkillID: 555},
		{Function: "POISON", Kind: 2, Remaining: 2, RoleIndex: 8, Source: 5},
	}
	playerSelfReleaseEngine.players[1].Effects = []battleEffect{{Function: "CARD_SEAL", CardType: 2, Kind: 2, Remaining: 3, RoleIndex: 9, Source: 5}}
	playerSelfReleaseResults, err := playerSelfReleaseEngine.executePlayerRole(
		battleAction{memberType: 1, target: 5, cardID: 10114001, cardLevel: 50, skill: playerSelfReleaseSkills[0], roles: catalog.SkillRoles[11101312]},
		*playerSelfSealRelease,
		1,
	)
	if err != nil {
		return fmt.Errorf("official player SELF card-seal release failed: %w", err)
	}
	if len(playerSelfReleaseResults) != 4 || playerSelfReleaseResults[0].Command != CommandBuffRelease ||
		playerSelfReleaseResults[1].Command != 72 || playerSelfReleaseResults[2].Command != CommandBuffLostOne ||
		playerSelfReleaseResults[3].Command != CommandBattleParam || len(playerSelfReleaseResults[0].Args) != 8 ||
		playerSelfReleaseResults[0].Args[0] != 1 || playerSelfReleaseResults[0].Args[1] != 1 ||
		playerSelfReleaseResults[0].Args[5] != 15 || playerSelfReleaseResults[1].Args[2] != int64(battleBuffCodes["CARD_SEAL"]) ||
		playerSelfReleaseResults[2].Args[0] != 1 || playerSelfReleaseResults[2].Args[1] != int64(battleBuffCodes["CARD_SEAL"]) ||
		playerSelfReleaseResults[2].Args[2] != 555 || len(playerSelfReleaseEngine.players[0].Effects) != 1 ||
		playerSelfReleaseEngine.players[0].Effects[0].Function != "POISON" ||
		len(playerSelfReleaseEngine.players[1].Effects) != 1 || playerSelfReleaseEngine.players[1].Effects[0].Function != "CARD_SEAL" {
		return fmt.Errorf("official player SELF card-seal release lifecycle is results=%+v players=%+v", playerSelfReleaseResults, playerSelfReleaseEngine.players[:2])
	}
	if next := playerSelfReleaseEngine.rng.next(); next != 442046446 {
		return fmt.Errorf("official player SELF card-seal release consumed wrong xor128 count: next=%d", next)
	}

	enemyAllBreakSkill, enemyAllBreakRoles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(32101114)
	if !ok || enemyAllBreakSkill.Target != "ENEMY_ALL" || len(enemyAllBreakRoles) != 1 ||
		enemyAllBreakRoles[0].Function != "DEBUFF_RELEASE_ONE" || enemyAllBreakRoles[0].Target != "SELECT" ||
		enemyAllBreakRoles[0].Parameters[0] != "1000" || enemyAllBreakRoles[0].Parameters[2] != "ATK_BREAK_BY_ATK" ||
		enemyAllBreakRoles[0].Parameters[3] != "ATK_BREAK_BY_INT" {
		return fmt.Errorf("official enemy all-member break release contract changed: skill=%+v roles=%+v ok=%t", enemyAllBreakSkill, enemyAllBreakRoles, ok)
	}
	enemyAllBreakEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 2}
	enemyAllBreakEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "ATK_BREAK_FIXED", Parameter: "ATK", Delta: -100, Kind: 2, Remaining: 2, RoleIndex: 7, Source: 1},
	}}
	enemyAllBreakEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "ATK_BREAK_FIXED", Parameter: "INT", Delta: -100, Kind: 2, Remaining: 2, RoleIndex: 8, Source: 1},
	}}
	enemyAllBreakResults, err := enemyAllBreakEngine.executeEnemyActionCandidate(
		&enemyAllBreakEngine.enemies[0],
		enemyActionCandidate{action: CombatEnemyAction{Slot: 1, Category: "skill", SkillID: 32101114, Target: "ENEMY_ALL", ActionCost: 1, MaxUses: 1, Rate: 100}},
	)
	if err != nil {
		return fmt.Errorf("official enemy all-member break release failed: %w", err)
	}
	wantEnemyAllReleaseCommands := []int{CommandSkill, CommandBuffRelease, 72, CommandBuffLostOne, CommandBattleParam, CommandBuffRelease, 72, CommandBuffLostOne, CommandBattleParam}
	if len(enemyAllBreakResults) != len(wantEnemyAllReleaseCommands) || len(enemyAllBreakResults[0].Args) != 7 ||
		enemyAllBreakResults[0].Args[0] != 5 || enemyAllBreakResults[0].Args[1] != 32101114 || enemyAllBreakResults[0].Args[2] != 0 ||
		enemyAllBreakResults[0].Args[4] != 4 || enemyAllBreakResults[0].Args[5] != 32101114 ||
		enemyAllBreakResults[1].Args[0] != 5 || enemyAllBreakResults[1].Args[5] != 1 ||
		enemyAllBreakResults[5].Args[0] != 6 || enemyAllBreakResults[5].Args[5] != 2 ||
		len(enemyAllBreakEngine.enemies[0].Effects) != 0 || len(enemyAllBreakEngine.enemies[1].Effects) != 0 {
		return fmt.Errorf("official enemy all-member break release lifecycle is results=%+v enemies=%+v", enemyAllBreakResults, enemyAllBreakEngine.enemies[:2])
	}
	for index, command := range wantEnemyAllReleaseCommands {
		if enemyAllBreakResults[index].Command != command {
			return fmt.Errorf("official enemy all-member break release command %d is %d, want %d: %+v", index, enemyAllBreakResults[index].Command, command, enemyAllBreakResults)
		}
	}
	if next := enemyAllBreakEngine.rng.next(); next != 2618378627 {
		return fmt.Errorf("official enemy all-member break release consumed wrong xor128 count: next=%d", next)
	}

	// Official BUFF_RELEASE_ONE_NUM 37701113 separates release kind selection
	// from its per-kind maximum count (p4). Only ATK_UP_BY_ATK may be removed;
	// an unrelated DEF buff must survive. BUFF_RELEASE_OLD 44258004 attempts
	// exactly the two oldest eligible buffs in insertion order.
	var oneNumRelease *CombatSkillRole
	for index := range catalog.SkillRoles[37701113] {
		candidate := &catalog.SkillRoles[37701113][index]
		if candidate.Function == "BUFF_RELEASE_ONE_NUM" && candidate.Parameters[2] == "ATK_UP_BY_ATK" {
			oneNumRelease = candidate
			break
		}
	}
	if oneNumRelease == nil || oneNumRelease.Parameters[0] != "100" || oneNumRelease.Parameters[4] != "100" {
		return errors.New("official one-num buff-release contract changed")
	}
	releaseEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	releaseEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999, Effects: []battleEffect{
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Delta: 100, Remaining: 2, AppliedTurn: 1},
		{Function: "DEF_UP_FIXED", Parameter: "DEF", Kind: 1, Delta: 200, Remaining: 2, AppliedTurn: 1},
	}}
	releaseEngine.players[0].Attack = 1100
	releaseEngine.players[0].Defense = 1200
	releaseActor := &battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5}
	oneNumResults, err := releaseEngine.executeEnemyRelease(releaseActor, 1, *oneNumRelease)
	if err != nil {
		return err
	}
	if len(oneNumResults) != 4 || oneNumResults[0].Command != CommandBuffRelease || oneNumResults[1].Command != 72 ||
		oneNumResults[2].Command != CommandBuffLostOne || oneNumResults[3].Command != CommandBattleParam ||
		len(releaseEngine.players[0].Effects) != 1 || releaseEngine.players[0].Effects[0].Function != "DEF_UP_FIXED" ||
		releaseEngine.players[0].Attack != 1000 || releaseEngine.players[0].Defense != 1200 {
		return errors.New("one-num buff release removed the wrong official buff kind")
	}
	if len(oneNumResults[0].Args) != 8 || oneNumResults[0].Args[2] != 1 {
		return fmt.Errorf("one-num buff release projected the wrong requested kind: %+v", oneNumResults[0])
	}
	if next := releaseEngine.rng.next(); next != 442046446 {
		return fmt.Errorf("100%% one-num buff release skipped xor128; next=%d, want 442046446", next)
	}
	var oldRelease *CombatSkillRole
	for index := range catalog.SkillRoles[44258004] {
		candidate := &catalog.SkillRoles[44258004][index]
		if candidate.Function == "BUFF_RELEASE_OLD" {
			oldRelease = candidate
			break
		}
	}
	if oldRelease == nil || oldRelease.Parameters[0] != "100" || oldRelease.Parameters[2] != "2" {
		return errors.New("official old-buff-release contract changed")
	}
	oldEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	oldEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999, Effects: []battleEffect{
		{Function: "DEF_UP_FIXED", Parameter: "DEF", Kind: 1, Delta: 200, Remaining: 2, AppliedTurn: 1},
		{Function: "CRITICAL_UP", Kind: 1, Remaining: 2, AppliedTurn: 2},
		{Function: "ATTR_DEF_UP", Attribute: "DARK", Kind: 1, Remaining: 2, AppliedTurn: 3},
	}}
	oldEngine.players[0].Defense = 1200
	oldResults, err := oldEngine.executeEnemyRelease(releaseActor, 1, *oldRelease)
	if err != nil {
		return err
	}
	if len(oldResults) != 6 || oldResults[0].Command != CommandBuffRelease || oldResults[1].Command != 72 ||
		oldResults[2].Command != 72 || oldResults[3].Command != CommandBuffLostOne ||
		oldResults[4].Command != CommandBuffLostOne || oldResults[5].Command != CommandBattleParam ||
		len(oldEngine.players[0].Effects) != 1 || oldEngine.players[0].Effects[0].Function != "ATTR_DEF_UP" ||
		oldEngine.players[0].Defense != 1000 {
		return errors.New("old buff release did not remove the two oldest effects")
	}
	if next := oldEngine.rng.next(); next != 2618378627 {
		return fmt.Errorf("old buff release did not consume one xor128 draw per attempt; next=%d, want 2618378627", next)
	}
	emptyOldEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	emptyOldEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000}
	failedRelease, err := emptyOldEngine.executeEnemyRelease(releaseActor, 1, *oldRelease)
	if err != nil {
		return err
	}
	if len(failedRelease) != 1 || failedRelease[0].Command != CommandBuffReleaseFailed || len(failedRelease[0].Args) != 2 ||
		failedRelease[0].Args[0] != 1 || failedRelease[0].Args[1] != int64(oldRelease.RoleIndex) {
		return fmt.Errorf("empty official buff-release failure projection is %+v", failedRelease)
	}
	if next := emptyOldEngine.rng.next(); next != 3718467011 {
		return fmt.Errorf("empty old buff release consumed xor128; next=%d, want 3718467011", next)
	}

	// Official CARD_SEAL 31101314 targets exactly one cost-5 card for three
	// turns. Other hand cards remain playable; this is not a party-wide lock.
	var sealRole *CombatSkillRole
	for index := range catalog.SkillRoles[31101314] {
		candidate := &catalog.SkillRoles[31101314][index]
		if candidate.Function == "CARD_SEAL" {
			sealRole = candidate
			break
		}
	}
	if sealRole == nil || sealRole.Parameters[0] != "3" || sealRole.Parameters[2] != "1" || sealRole.Parameters[6] != "5" || sealRole.Parameters[7] != "5" {
		return errors.New("official card-seal contract changed")
	}
	sealEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	sealEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 2, HP: 10000, MaxHP: 10000}
	sealEngine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 10000052, Level: 50} // Skill cost 5.
	sealEngine.players[0].Deck[1] = BattleCard{CardType: 2, CardID: 10131011, Level: 60} // Skill cost 2.
	sealEngine.players[0].Hand[0] = 1
	sealEngine.players[0].Hand[1] = 2
	sealActor := &battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5}
	sealResults, err := sealEngine.executeEnemyPersistentEffect(sealActor, 1, *sealRole)
	if err != nil {
		return err
	}
	sealed, hasSeal := playerCardSealEffect(&sealEngine.players[0], 1)
	_, wrongSeal := playerCardSealEffect(&sealEngine.players[0], 2)
	if len(sealResults) != 1 || !hasSeal || wrongSeal || sealed.Remaining != 3 {
		return fmt.Errorf("card seal result=%d effects=%+v has=%t wrong=%t remaining=%d", len(sealResults), sealEngine.players[0].Effects, hasSeal, wrongSeal, sealed.Remaining)
	}
	if next := sealEngine.rng.next(); next != 3316842567 {
		return fmt.Errorf("card seal consumed the wrong producer/selection/resistance/duration draws; next=%d, want 3316842567", next)
	}
	// The official masters contain intentionally descending hand-status ranges.
	// Native leaves the upper field unchanged and does not consume xor128; it
	// never normalizes them by swapping the two endpoints.
	var reversedTrapRole *CombatSkillRole
	for index := range catalog.SkillRoles[38001613] {
		candidate := &catalog.SkillRoles[38001613][index]
		if candidate.Function == "CARD_TRAP_DAMAGE" {
			reversedTrapRole = candidate
			break
		}
	}
	if reversedTrapRole == nil {
		return errors.New("official descending card-trap role is missing")
	}
	reversedEngine := &BattleEngine{rng: newXorShift128(1)}
	reversedEffect := persistentBattleEffect(*reversedTrapRole, 0, combatParameterInt(reversedTrapRole.Parameters[0]), 2, 1, 5, 0)
	reversedEngine.prepareRandomHandRole(*reversedTrapRole, &reversedEffect)
	if reversedTrapRole.Parameters[1] != "4" || reversedTrapRole.Parameters[2] != "2" || reversedEffect.Parameters[0] != 2 {
		return fmt.Errorf("descending official card-trap count is role=%+v effect=%+v", reversedTrapRole.Parameters, reversedEffect.Parameters)
	}
	if next := reversedEngine.rng.next(); next != 3718467011 {
		return fmt.Errorf("descending card-trap count consumed xor128; next=%d, want 3718467011", next)
	}
	reversedDurationEngine := &BattleEngine{rng: newXorShift128(1)}
	if duration, rolled := reversedDurationEngine.nativeInclusive(5, 4); duration != 4 || rolled {
		return fmt.Errorf("descending card-seal duration is value=%d rolled=%t, want 4/false", duration, rolled)
	}
	if next := reversedDurationEngine.rng.next(); next != 3718467011 {
		return fmt.Errorf("descending card-seal duration consumed xor128; next=%d, want 3718467011", next)
	}
	sealEngine.phase = PhaseUser
	sealEngine.players[0].Cost = 10
	if _, err := sealEngine.Submit(1, cardPlaySubmission{CardTypes: [5]int{1}, Targets: [5]int{0}}); err == nil {
		return errors.New("sealed card remained playable")
	}
	sealEngine.selectedPlays = make(map[int]cardPlaySubmission, maxRoomMembers)
	if _, err := sealEngine.Submit(1, cardPlaySubmission{CardTypes: [5]int{2}, Targets: [5]int{0}}); err != nil {
		return fmt.Errorf("unsealed card was blocked by another card seal: %w", err)
	}
	findPlayerStatusRole := func(skillID int, function string) (*CombatSkillRole, error) {
		for index := range catalog.SkillRoles[skillID] {
			role := &catalog.SkillRoles[skillID][index]
			if role.Function == function {
				return role, nil
			}
		}
		return nil, fmt.Errorf("official player skill %d role %q is missing", skillID, function)
	}
	sealResist100Role, err := findPlayerStatusRole(12500792, "CARD_SEAL_REGIST")
	if err != nil {
		return err
	}
	if sealResist100Role.Target != "SELECT" || sealResist100Role.Parameters[0] != "1" ||
		sealResist100Role.Parameters[1] != "100" || sealResist100Role.Parameters[2] != "0" {
		return fmt.Errorf("official 100-percent card-seal resistance row changed: %+v", sealResist100Role)
	}
	resistSealEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	resistSealEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 2, HP: 10000, MaxHP: 10000}
	resistSealEngine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 10000052, Level: 50}
	resistSealEngine.players[0].Hand[0] = 1
	sealResist100Apply, err := resistSealEngine.executePersistentEffect(
		battleAction{memberType: 1, target: 1, cardLevel: 60, roles: []CombatSkillRole{*sealResist100Role}},
		*sealResist100Role, 60, 1,
	)
	if err != nil {
		return err
	}
	if len(sealResist100Apply) != 1 || sealResist100Apply[0].Command != CommandBuff ||
		len(sealResist100Apply[0].Args) != 11 || sealResist100Apply[0].Args[0] != 1 ||
		sealResist100Apply[0].Args[3] != int64(battleBuffCodes["CARD_SEAL_REGIST"]) ||
		len(resistSealEngine.players[0].Effects) != 1 || resistSealEngine.players[0].Effects[0].Function != "CARD_SEAL_REGIST" ||
		resistSealEngine.players[0].Effects[0].Value != 100 || resistSealEngine.players[0].Effects[0].Remaining != 1 ||
		resistSealEngine.players[0].Effects[0].Source != 1 || resistSealEngine.players[0].Effects[0].Kind != 1 {
		return fmt.Errorf("official 100-percent card-seal resistance apply is results=%+v effects=%+v", sealResist100Apply, resistSealEngine.players[0].Effects)
	}
	resistedSeal, err := resistSealEngine.executeEnemyPersistentEffect(sealActor, 1, *sealRole)
	if err != nil {
		return err
	}
	if len(resistedSeal) != 1 || resistedSeal[0].Command != CommandDebuffFailed || resistedSeal[0].Args[0] != 1 ||
		resistedSeal[0].Args[1] != int64(sealRole.RoleIndex) || resistedSeal[0].Args[2] != int64(battleBuffCodes["CARD_SEAL"]) {
		return fmt.Errorf("official 100-percent card-seal resistance projection is %+v", resistedSeal)
	}
	if len(resistSealEngine.players[0].Effects) != 1 {
		return fmt.Errorf("official 100-percent card-seal resistance left state %+v", resistSealEngine.players[0].Effects)
	}
	if next := resistSealEngine.rng.next(); next != 394756926 {
		return fmt.Errorf("official 100-percent card-seal resistance consumed wrong RNG count; next=%d, want 394756926", next)
	}

	// Official 12100922 applies 40% CARD_SEAL_REGIST to every living Arthur.
	// Enemy 38001112 then excludes RECOVERY cards (p8=0), selects one other
	// hand card per target, and performs independent resistance/duration draws.
	sealResist40Role, err := findPlayerStatusRole(12100922, "CARD_SEAL_REGIST")
	if err != nil {
		return err
	}
	if sealResist40Role.Target != "FRIEND_ALL" || sealResist40Role.Parameters[0] != "2" ||
		sealResist40Role.Parameters[1] != "40" || sealResist40Role.Parameters[2] != "0" {
		return fmt.Errorf("official party card-seal resistance row changed: %+v", sealResist40Role)
	}
	var partySealRole *CombatSkillRole
	for index := range catalog.SkillRoles[38001112] {
		candidate := &catalog.SkillRoles[38001112][index]
		if candidate.Function == "CARD_SEAL" {
			partySealRole = candidate
			break
		}
	}
	if partySealRole == nil || partySealRole.Target != "FRIEND_ALL" || partySealRole.Parameters[0] != "2" ||
		partySealRole.Parameters[1] != "2" || partySealRole.Parameters[2] != "1" || partySealRole.Parameters[3] != "1" ||
		partySealRole.Parameters[4] != "RECOVERY" || partySealRole.Parameters[5] != "ALL" ||
		partySealRole.Parameters[6] != "-1" || partySealRole.Parameters[7] != "-1" || partySealRole.Parameters[8] != "0" {
		return fmt.Errorf("official party card-seal row changed: %+v", partySealRole)
	}
	partySealEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	for index := range partySealEngine.players {
		partySealEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, ArthurType: 1, HP: 10000, MaxHP: 10000}
		partySealEngine.players[index].Deck[0] = BattleCard{CardType: 1, CardID: 10000052, Level: 50} // DEFENSE, selected by RECOVERY exclusion.
		partySealEngine.players[index].Deck[1] = BattleCard{CardType: 2, CardID: 10000185, Level: 30} // RECOVERY, excluded from candidates.
		partySealEngine.players[index].Hand[0] = 1
		partySealEngine.players[index].Hand[1] = 2
	}
	partyResistApply, err := partySealEngine.executePersistentEffect(
		battleAction{memberType: 1, target: 1, cardLevel: 60, roles: []CombatSkillRole{*sealResist40Role}},
		*sealResist40Role, 60, 1,
	)
	if err != nil {
		return err
	}
	if len(partyResistApply) != maxRoomMembers {
		return fmt.Errorf("official party card-seal resistance projected %d rows", len(partyResistApply))
	}
	for index := range partySealEngine.players {
		result := partyResistApply[index]
		effects := partySealEngine.players[index].Effects
		if result.Command != CommandBuff || len(result.Args) != 11 || result.Args[0] != int64(index+1) ||
			result.Args[3] != int64(battleBuffCodes["CARD_SEAL_REGIST"]) || len(effects) != 1 ||
			effects[0].Function != "CARD_SEAL_REGIST" || effects[0].Value != 40 || effects[0].Remaining != 2 ||
			effects[0].Source != 1 || effects[0].Kind != 1 {
			return fmt.Errorf("official party card-seal resistance target %d is result=%+v effects=%+v", index+1, result, effects)
		}
	}
	partySealResults, err := partySealEngine.executeEnemyPersistentEffect(&battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5}, 1, *partySealRole)
	if err != nil {
		return err
	}
	if len(partySealResults) != maxRoomMembers || partySealResults[2].Command != CommandDebuffFailed ||
		partySealResults[2].Args[0] != 3 || partySealResults[2].Args[1] != int64(partySealRole.RoleIndex) ||
		partySealResults[2].Args[2] != int64(battleBuffCodes["CARD_SEAL"]) {
		return fmt.Errorf("official mixed party card-seal failure prefix is %+v", partySealResults)
	}
	for index := 0; index < maxRoomMembers; index++ {
		result := partySealResults[index]
		effects := partySealEngine.players[index].Effects
		if index == 2 {
			if result.Command != CommandDebuffFailed || len(effects) != 1 || effects[0].Function != "CARD_SEAL_REGIST" {
				return fmt.Errorf("official mixed party card-seal resisted target %d is result=%+v effects=%+v", index+1, result, effects)
			}
			continue
		}
		seal, sealed := playerCardSealEffect(&partySealEngine.players[index], 1)
		_, recoverySealed := playerCardSealEffect(&partySealEngine.players[index], 2)
		if result.Command != CommandBuff || len(result.Args) != 11 || result.Args[0] != int64(index+1) ||
			result.Args[3] != int64(battleBuffCodes["CARD_SEAL"]) || result.Args[7] != 8 || result.Args[8] != -1 ||
			len(effects) != 2 || !sealed || recoverySealed || seal.Remaining != 2 || seal.Source != 5 || seal.Kind != 2 {
			return fmt.Errorf("official mixed party card-seal target %d is result=%+v effects=%+v sealed=%t recovery=%t", index+1, result, effects, sealed, recoverySealed)
		}
	}
	if len(partySealEngine.players[2].Effects) != 1 {
		return fmt.Errorf("official mixed party card-seal resisted target state is %+v", partySealEngine.players[2].Effects)
	}
	if next := partySealEngine.rng.next(); next != 1587371241 {
		return fmt.Errorf("official mixed party card-seal consumed wrong RNG count; next=%d, want 1587371241", next)
	}
	darkResist60Role, err := findPlayerStatusRole(12501334, "DARKNESS_REGIST")
	if err != nil {
		return err
	}
	if darkResist60Role.Target != "SELECT" || darkResist60Role.Parameters[0] != "2" ||
		darkResist60Role.Parameters[1] != "60" || darkResist60Role.Parameters[2] != "0" {
		return fmt.Errorf("official 60-percent darkness resistance row changed: %+v", darkResist60Role)
	}
	darkResist60Engine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(3)}
	darkResist60Engine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000}
	darkResist60Apply, err := darkResist60Engine.executePersistentEffect(
		battleAction{memberType: 1, target: 1, cardLevel: 60, roles: []CombatSkillRole{*darkResist60Role}},
		*darkResist60Role, 60, 1,
	)
	if err != nil {
		return err
	}
	if len(darkResist60Apply) != 1 || darkResist60Apply[0].Command != CommandBuff ||
		len(darkResist60Apply[0].Args) != 11 || darkResist60Apply[0].Args[0] != 1 ||
		darkResist60Apply[0].Args[3] != int64(battleBuffCodes["DARKNESS_REGIST"]) ||
		len(darkResist60Engine.players[0].Effects) != 1 || darkResist60Engine.players[0].Effects[0].Function != "DARKNESS_REGIST" ||
		darkResist60Engine.players[0].Effects[0].Value != 60 || darkResist60Engine.players[0].Effects[0].Remaining != 2 ||
		darkResist60Engine.players[0].Effects[0].Source != 1 || darkResist60Engine.players[0].Effects[0].Kind != 1 {
		return fmt.Errorf("official 60-percent darkness resistance apply is results=%+v effects=%+v", darkResist60Apply, darkResist60Engine.players[0].Effects)
	}
	darknessThrough60, err := darkResist60Engine.executeEnemyPersistentEffect(darkActor, 1, *darknessRole)
	if err != nil {
		return err
	}
	if len(darknessThrough60) != 1 || darknessThrough60[0].Command != CommandBuff ||
		len(darkResist60Engine.players[0].Effects) != 2 || darkResist60Engine.players[0].Effects[1].Function != "DARKNESS_APPOINT" ||
		darkResist60Engine.players[0].Effects[1].Mask != 10 || darkResist60Engine.players[0].Effects[1].Source != 5 {
		return fmt.Errorf("official 60-percent darkness resistance roll is results=%+v effects=%+v", darknessThrough60, darkResist60Engine.players[0].Effects)
	}
	if next := darkResist60Engine.rng.next(); next != 407442942 {
		return fmt.Errorf("official 60-percent darkness resistance consumed wrong RNG count; next=%d, want 407442942", next)
	}

	darkResist100Role, err := findPlayerStatusRole(12501392, "DARKNESS_REGIST")
	if err != nil {
		return err
	}
	if darkResist100Role.Target != "SELECT" || darkResist100Role.Parameters[0] != "1" ||
		darkResist100Role.Parameters[1] != "100" || darkResist100Role.Parameters[2] != "0" {
		return fmt.Errorf("official 100-percent darkness resistance row changed: %+v", darkResist100Role)
	}
	resistDarkEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	resistDarkEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000}
	darkResist100Apply, err := resistDarkEngine.executePersistentEffect(
		battleAction{memberType: 1, target: 1, cardLevel: 60, roles: []CombatSkillRole{*darkResist100Role}},
		*darkResist100Role, 60, 1,
	)
	if err != nil {
		return err
	}
	if len(darkResist100Apply) != 1 || darkResist100Apply[0].Command != CommandBuff ||
		len(resistDarkEngine.players[0].Effects) != 1 || resistDarkEngine.players[0].Effects[0].Value != 100 ||
		resistDarkEngine.players[0].Effects[0].Remaining != 1 || resistDarkEngine.players[0].Effects[0].Source != 1 {
		return fmt.Errorf("official 100-percent darkness resistance apply is results=%+v effects=%+v", darkResist100Apply, resistDarkEngine.players[0].Effects)
	}
	resistedDarkness, err := resistDarkEngine.executeEnemyPersistentEffect(darkActor, 1, *darknessRole)
	if err != nil {
		return err
	}
	if len(resistedDarkness) != 1 || resistedDarkness[0].Command != CommandDebuffFailed || resistedDarkness[0].Args[0] != 1 ||
		resistedDarkness[0].Args[1] != int64(darknessRole.RoleIndex) || resistedDarkness[0].Args[2] != int64(battleBuffCodes["DARKNESS_APPOINT"]) ||
		len(resistDarkEngine.players[0].Effects) != 1 {
		return fmt.Errorf("100-percent darkness resistance projection is %+v", resistedDarkness)
	}
	if next := resistDarkEngine.rng.next(); next != 442046446 {
		return fmt.Errorf("official 100-percent darkness resistance consumed wrong RNG count; next=%d, want 442046446", next)
	}

	// Native CARD_TRAP_DAMAGE excludes sealed cards. Conversely, CARD_SEAL
	// consumes an existing trap on the selected card before adding the seal.
	interactionEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1)}
	interactionEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 2, HP: 10000, MaxHP: 10000}
	interactionEngine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 10000052, Level: 50}
	interactionEngine.players[0].Deck[1] = BattleCard{CardType: 2, CardID: 10000052, Level: 50}
	interactionEngine.players[0].Hand[0] = 1
	interactionEngine.players[0].Hand[1] = 2
	interactionEngine.players[0].Effects = []battleEffect{{Function: "CARD_SEAL", CardType: 1, Remaining: 3, Kind: 2}}
	if _, err := interactionEngine.executeEnemyPersistentEffect(trapActor, 1, *trapRole); err != nil {
		return err
	}
	if _, trappedSealed := playerCardTrapEffect(&interactionEngine.players[0], 1); trappedSealed {
		return errors.New("card trap selected an already sealed card")
	}
	if _, trappedOpen := playerCardTrapEffect(&interactionEngine.players[0], 2); !trappedOpen {
		return errors.New("card trap did not select the only unsealed hand card")
	}
	interactionEngine.players[0].Effects = []battleEffect{{Function: "CARD_TRAP_DAMAGE", CardType: 1, Value: 1000, Remaining: 3, Kind: 2}}
	interactionEngine.players[0].Deck[1] = BattleCard{CardType: 2, CardID: 10131011, Level: 60}
	if _, err := interactionEngine.executeEnemyPersistentEffect(sealActor, 1, *sealRole); err != nil {
		return err
	}
	if _, stillTrapped := playerCardTrapEffect(&interactionEngine.players[0], 1); stillTrapped {
		return errors.New("card seal did not remove the selected card trap")
	}
	if _, nowSealed := playerCardSealEffect(&interactionEngine.players[0], 1); !nowSealed {
		return errors.New("card seal was not applied after removing the selected trap")
	}

	// The only active official DEBUFF_REGIST rows (44209002/102/202) are
	// 99-turn, 100-percent STAN resistance. The original x86 API retains it,
	// but 8bb40's STAN branch bypasses the DOT-only typed resistance check.
	var debuffRegistRole *CombatSkillRole
	for index := range catalog.SkillRoles[44209002] {
		candidate := &catalog.SkillRoles[44209002][index]
		if candidate.Function == "DEBUFF_REGIST" {
			debuffRegistRole = candidate
			break
		}
	}
	if debuffRegistRole == nil || debuffRegistRole.Parameters[0] != "99" || debuffRegistRole.Parameters[1] != "100" || debuffRegistRole.Parameters[2] != "STAN" {
		return errors.New("official debuff-resistance contract changed")
	}
	var stunRole *CombatSkillRole
	for index := range catalog.SkillRoles[11100282] {
		candidate := &catalog.SkillRoles[11100282][index]
		if candidate.Function == "STAN" {
			copy := *candidate
			copy.Parameters[1] = "100" // Isolate typed resistance from the role's accuracy roll.
			stunRole = &copy
			break
		}
	}
	if stunRole == nil {
		return errors.New("official STAN role 11100282 is missing")
	}
	resistDebuffEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 1}
	resistDebuffEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 1, HP: 10000, MaxHP: 10000}
	resistDebuffEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	registActor := &resistDebuffEngine.enemies[0]
	resolvedRegistRole := *debuffRegistRole
	resolvedRegistRole.Target = catalog.Skills[44209002][0].Target
	if _, err := resistDebuffEngine.executeEnemyPersistentEffect(registActor, 0, resolvedRegistRole); err != nil {
		return err
	}
	if len(registActor.Effects) != 1 || registActor.Effects[0].Function != "DEBUFF_REGIST" || registActor.Effects[0].Parameter != "STAN" || registActor.Effects[0].Value != 100 {
		return fmt.Errorf("debuff resistance durable state is %+v", registActor.Effects)
	}
	stunSkill := catalog.Skills[11100282][0]
	stunResults, err := resistDebuffEngine.executePlayerRole(battleAction{memberType: 1, cardLevel: 1, target: 5, skill: stunSkill}, *stunRole, 1)
	if err != nil {
		return err
	}
	if len(stunResults) != 2 || stunResults[0].Command != 62 || len(stunResults[0].Args) != 11 ||
		stunResults[0].Args[0] != 5 || stunResults[0].Args[1] != int64(stunRole.RoleIndex) ||
		stunResults[0].Args[3] != int64(battleBuffCodes["STAN"]) || stunResults[1].Command != 6 ||
		combatEffectCount(registActor.Effects, "STAN", "") != 1 {
		return fmt.Errorf("STAN bypass of typed debuff resistance is results=%+v effects=%+v", stunResults, registActor.Effects)
	}
	nonResistStunEngine := &BattleEngine{catalog: catalog, turn: 1, phase: PhaseUserAttack, rng: newXorShift128(1), enemyCount: 1}
	for index := range nonResistStunEngine.players {
		nonResistStunEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	nonResistStunEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Effects: []battleEffect{{Function: "STAN", Remaining: 1, Kind: 2}}}
	stunPhaseResults, err := nonResistStunEngine.EnemyPhase()
	if err != nil {
		return err
	}
	if len(stunPhaseResults) != 3 || stunPhaseResults[0].Command != CommandStun || len(stunPhaseResults[0].Args) != 1 || stunPhaseResults[0].Args[0] != 5 ||
		stunPhaseResults[1].Command != 59 || len(stunPhaseResults[1].Args) != 0 || stunPhaseResults[2].Command != 59 || len(stunPhaseResults[2].Args) != 0 {
		return fmt.Errorf("stunned enemy phase projection is %+v", stunPhaseResults)
	}

	appointedRoles := catalog.SkillRoles[44188005]
	var appointedRole *CombatSkillRole
	for index := range appointedRoles {
		if appointedRoles[index].Function == "DEAL_PENALTY_TURN_APPOINT" {
			appointedRole = &appointedRoles[index]
			break
		}
	}
	if appointedRole == nil || appointedRole.Parameters[0] != "2" || appointedRole.Parameters[1] != "3" {
		return errors.New("official appointed deal-penalty contract changed")
	}
	appointedEngine := &BattleEngine{catalog: catalog, turn: 1}
	appointedActor := &battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5}
	for index := range appointedEngine.players {
		appointedEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 100, MaxHP: 100}
	}
	appointedResults, err := appointedEngine.executeEnemyRole(appointedActor, 1, *appointedRole, appointedRoles)
	if err != nil {
		return err
	}
	if len(appointedResults) != maxRoomMembers*2 {
		return fmt.Errorf("appointed deal penalty returned %d results, want %d", len(appointedResults), maxRoomMembers*2)
	}
	appointedRow, err := codec.Row(appointedResults[0])
	if err != nil {
		return err
	}
	if appointedRow != "62,1,1,0,408,5,0,1,3,0,0,0" {
		return fmt.Errorf("appointed deal penalty ResultCmd projection is %q", appointedRow)
	}
	for index := range appointedEngine.players {
		if appointedResults[index*2].Command != CommandBuff || appointedResults[index*2].Args[0] != int64(index+1) ||
			appointedResults[index*2+1].Command != CommandBattleParam ||
			!equalBattleArgs(appointedResults[index*2+1].Args, battleParameterArgs(index+1, 100, 100, 0, 0, 0, 0, 0, 0, 0, 0)) {
			return fmt.Errorf("appointed deal penalty omitted per-target parameter snapshot: %+v", appointedResults)
		}
	}
	appointedEngine.turn = 2
	release, tickErr = expireForEffectContract(appointedEngine)
	if tickErr != nil {
		return tickErr
	}
	if len(release) != 0 {
		return errors.New("appointed deal penalty emitted a premature release")
	}
	for index := range appointedEngine.players {
		if playerDrawEffectValue(&appointedEngine.players[index], battleBuffCodes["DEAL_PENALTY"]) != 3 {
			return fmt.Errorf("appointed deal penalty omitted member %d", index+1)
		}
	}
	appointedEngine.turn = 3
	if _, tickErr = expireForEffectContract(appointedEngine); tickErr != nil {
		return tickErr
	}
	for index := range appointedEngine.players {
		if playerDrawEffectValue(&appointedEngine.players[index], battleBuffCodes["DEAL_PENALTY"]) != 0 {
			return fmt.Errorf("appointed deal penalty did not expire for member %d", index+1)
		}
	}

	findPlayerRole := func(skillID int, function string) (*CombatSkillRole, error) {
		for index := range catalog.SkillRoles[skillID] {
			role := &catalog.SkillRoles[skillID][index]
			if role.Function == function {
				return role, nil
			}
		}
		return nil, fmt.Errorf("official player role %s is missing from skill %d", function, skillID)
	}
	findEnemyRole := func(skillID int, function string, parameter string) (*CombatSkillRole, error) {
		for index := range catalog.SkillRoles[skillID] {
			candidate := &catalog.SkillRoles[skillID][index]
			if candidate.Function == function && (parameter == "" || candidate.Parameters[1] == parameter) {
				return candidate, nil
			}
		}
		return nil, fmt.Errorf("official enemy role %s/%s is missing from skill %d", function, parameter, skillID)
	}

	// D-298 follows the producer through the actual 822a0/8d7e0 consumer:
	// Chain multiplies the complete fixed heal/regen, not only its fixed term.
	fixedHealRole, err := findPlayerRole(14200292, "HEAL_FIXED")
	if err != nil {
		return err
	}
	if value := fixedHealRoleValue(*fixedHealRole, 60, 4, 10000); value != 30563 {
		return fmt.Errorf("official fixed-heal level/chain/source value is %d, want 30563", value)
	}
	selfHealRole, err := findPlayerRole(29900002, "HEAL_BY_SELF_PARAM")
	if err != nil {
		return err
	}
	if value := selfScaledHealRoleValue(*selfHealRole, 60, 1, 10000); value != 10000 {
		return fmt.Errorf("official self-parameter heal value is %d, want 10000", value)
	}
	fixedRegenerateRole, err := findPlayerRole(14200192, "REGENERATE_FIXED")
	if err != nil {
		return err
	}
	if value := fixedRegenerateRoleValue(*fixedRegenerateRole, 60, 4, 10000); value != 22820 {
		return fmt.Errorf("official fixed-regenerate level/chain/source value is %d, want 22820", value)
	}

	// ATTR_DEF producers keep two independent groups. The first is a signed
	// per-mille multiplier; the second is a signed fixed delta after ordinary
	// defense. ATTR_DEF_DOWN ignores Chain in the original API; its preview is
	// separate from ResultCmd62's four zero notification values (88a20/8fb80).
	playerAttrDown, err := findPlayerRole(11104672, "ATTR_DEF_DOWN")
	if err != nil {
		return err
	}
	if primary, fixed := attributeDefenseRoleValues(*playerAttrDown, 60, 1); primary != 0 || fixed != 18350 {
		return fmt.Errorf("official player attribute-defense-down is primary=%d fixed=%d, want 0/18350", primary, fixed)
	}
	playerAttrChain, err := findPlayerRole(10301551, "ATTR_DEF_DOWN")
	if err != nil {
		return err
	}
	if primary, fixed := attributeDefenseRoleValues(*playerAttrChain, 60, 4); primary != 0 || fixed != 10116 {
		return fmt.Errorf("official attribute-defense-down ignores Chain: primary=%d fixed=%d, want 0/10116", primary, fixed)
	}
	playerAttrEffect := persistentBattleEffect(*playerAttrDown, persistentRoleValue(*playerAttrDown, 60, &battlePlayer{}, 1), 3, 2, 1, 1, 60)
	playerAttrResult := battlePersistentResult(5, *playerAttrDown, battleBuffCodes["ATTR_DEF_DOWN"], playerAttrEffect)
	if len(playerAttrResult.Args) != 11 || playerAttrResult.Args[7] != 0 || playerAttrResult.Args[8] != 0 {
		return fmt.Errorf("official attribute-defense-down ResultCmd projection is %+v", playerAttrResult)
	}

	// Native role 168 (FUN_0009900d) emits action type 24/subtype 1. Its
	// consumer sends ResultCmd207 first and ResultCmd506 second; the latter is
	// the managed Arthur.burst_gauge write. The official row below also gates
	// the transition threshold (100), distinct from the gauge cap (300).
	burstRole, err := findPlayerRole(11110112, "BURST_GAUGE_QUICK_UP")
	if err != nil {
		return err
	}
	burstEngine := &BattleEngine{catalog: catalog, turn: 1}
	burstEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 100, MaxHP: 100, Burst: 97, BurstState: burstGaugeNormal}
	burstResults, err := burstEngine.executePlayerRole(battleAction{memberType: 1, cardLevel: 60}, *burstRole, 1)
	if err != nil {
		return err
	}
	if len(burstResults) != 2 || burstResults[0].Command != 207 || burstResults[0].Args[0] != 1 || burstResults[0].Args[1] != int64(burstRole.RoleIndex) || burstResults[0].Args[2] != 6 || burstResults[0].Args[3] != 1 || burstResults[1].Command != CommandBurstGaugeState || burstResults[1].Args[0] != 1 || burstResults[1].Args[1] != 103 || burstEngine.players[0].Burst != 103 {
		return fmt.Errorf("official player burst-gauge projection/state is results=%+v player=%+v", burstResults, burstEngine.players[0])
	}

	// Native fixed/self-parameter producers pass FUN_000795e1's chain value as
	// an additive coefficient (times ten for self-parameter roles, D-383).
	// It never multiplies the completed value as a percentage. These CN
	// rows also gate the distinct ATK_UP_BY_SELF_PARAM buff code and the native
	// ResultCmd72 -> one ResultCmd6 expiry projection.
	fixedParameterRole, err := findPlayerRole(11100452, "ATK_UP_FIXED")
	if err != nil {
		return err
	}
	if got := fixedBuffRoleValue(*fixedParameterRole, 60, 3); got != 7660 {
		return fmt.Errorf("official fixed parameter chain value is %d, want 7660", got)
	}
	selfParameterRole, err := findPlayerRole(11401602, "ATK_UP_BY_SELF_PARAM")
	if err != nil {
		return err
	}
	if got := selfScaledParameterValue(*selfParameterRole, 60, 4, 10000); got != 10720 {
		return fmt.Errorf("official self parameter chain value is %d, want 10720", got)
	}
	parameterEngine := &BattleEngine{catalog: catalog, turn: 1}
	parameterEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000, Attack: 1000, Magic: 2000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	fixedParameterResults, err := parameterEngine.executeFixedParameter(battleAction{memberType: 1, target: 1, cardLevel: 60}, *fixedParameterRole, 3)
	if err != nil {
		return err
	}
	if len(fixedParameterResults) != 2 || fixedParameterResults[0].Command != CommandBuff || fixedParameterResults[0].Args[3] != int64(battleBuffCodes["ATK_UP_FIXED"]) || parameterEngine.players[0].Attack != 8660 {
		return fmt.Errorf("fixed parameter projection/state is results=%+v player=%+v", fixedParameterResults, parameterEngine.players[0])
	}
	if _, err := parameterEngine.executeFixedParameter(battleAction{memberType: 1, target: 1, cardLevel: 60}, *fixedParameterRole, 3); err != nil {
		return err
	}
	if parameterEngine.players[0].Attack != 16320 {
		return fmt.Errorf("same-kind fixed parameters did not stack additively: %+v", parameterEngine.players[0])
	}
	selfParameterResults, err := parameterEngine.executeSelfScaledParameter(battleAction{memberType: 1, target: 1, cardLevel: 60}, *selfParameterRole, 4)
	if err != nil {
		return err
	}
	if len(selfParameterResults) != 2 || selfParameterResults[0].Command != CommandBuff || selfParameterResults[0].Args[3] != int64(battleBuffCodes["ATK_UP_BY_SELF_PARAM"]) || parameterEngine.players[0].Magic != 12720 {
		return fmt.Errorf("self parameter projection/state is results=%+v player=%+v", selfParameterResults, parameterEngine.players[0])
	}
	// Role 23 is not an alias for the ATK-up role above. The official
	// 10300951 row derives a DEF increase from the actor's current HP and then
	// applies that retained value to FRIEND_ALL with the DEF_UP buff identity.
	// Keeping a full vector here catches a dispatcher or parameter-owner
	// regression that the shared selfScaledParameterValue arithmetic cannot.
	defenseSelfRole, err := findPlayerRole(10300951, "DEF_UP_BY_SELF_PARAM")
	if err != nil {
		return err
	}
	if defenseSelfRole.Parameters[1] != "DEF" || defenseSelfRole.Parameters[2] != "HP" ||
		selfScaledParameterValue(*defenseSelfRole, 60, 1, 10000) != 1900 {
		return fmt.Errorf("official player DEF_UP_BY_SELF_PARAM changed: %+v", defenseSelfRole)
	}
	defenseSelfEngine := newSphereContractEngine(catalog)
	defenseSelfEngine.players[0].HP = 10000
	defenseSelfEngine.players[0].MaxHP = 10000
	defenseSelfEngine.players[0].BaseMaxHP = 10000
	defenseSelfResults, err := defenseSelfEngine.executeSelfScaledParameter(
		battleAction{memberType: 1, target: 1, cardLevel: 60}, *defenseSelfRole, 1,
	)
	if err != nil {
		return err
	}
	if len(defenseSelfResults) != maxRoomMembers*2 || defenseSelfResults[0].Command != CommandBuff ||
		defenseSelfResults[0].Args[3] != int64(battleBuffCodes["DEF_UP_BY_SELF_PARAM"]) {
		return fmt.Errorf("official player DEF_UP_BY_SELF_PARAM projection is %+v", defenseSelfResults)
	}
	for index := range defenseSelfEngine.players {
		player := &defenseSelfEngine.players[index]
		if player.Defense != 2900 || len(player.Effects) != 1 || player.Effects[0].Delta != 1900 ||
			player.Effects[0].Function != "DEF_UP_BY_SELF_PARAM" || player.Effects[0].Parameter != "DEF" {
			return fmt.Errorf("official player DEF_UP_BY_SELF_PARAM target %d is %+v", index+1, player)
		}
	}

	// Role 32 has its own GUARD_BREAK buff identity and DEF/MDEF consumer.
	// Official 12600041 produces 1160 DEF break at level 60 and must retain
	// the unclamped delta even when a target stat later reaches zero.
	guardFixedRole, err := findPlayerRole(12600041, "GUARD_BREAK_FIXED")
	if err != nil {
		return err
	}
	if guardFixedRole.Parameters[1] != "DEF" || fixedBuffRoleValue(*guardFixedRole, 60, 1) != 1160 {
		return fmt.Errorf("official player GUARD_BREAK_FIXED changed: %+v", guardFixedRole)
	}
	guardFixedEngine := newSphereContractEngine(catalog)
	guardFixedEngine.enemies[0].Defense = 2000
	guardFixedEngine.enemies[0].BaseDefense = 2000
	guardFixedResults, err := guardFixedEngine.executeEnemyParameterDebuff(
		battleAction{memberType: 1, target: 5, cardLevel: 60}, *guardFixedRole, 1,
	)
	if err != nil {
		return err
	}
	if len(guardFixedResults) != 2 || guardFixedResults[0].Command != CommandBuff ||
		guardFixedResults[0].Args[3] != int64(battleBuffCodes["GUARD_BREAK_FIXED"]) ||
		guardFixedEngine.enemies[0].Defense != 840 || len(guardFixedEngine.enemies[0].Effects) != 1 ||
		guardFixedEngine.enemies[0].Effects[0].Delta != -1160 {
		return fmt.Errorf("official player GUARD_BREAK_FIXED state is results=%+v enemy=%+v", guardFixedResults, guardFixedEngine.enemies[0])
	}
	parameterEngine.turn = 2
	premature, tickErr := expireForEffectContract(parameterEngine)
	if tickErr != nil {
		return tickErr
	}
	if len(premature) != 0 {
		return fmt.Errorf("parameter effects emitted premature expiry results %+v", premature)
	}
	parameterEngine.turn = 3
	expiryResults, tickErr := expireForEffectContract(parameterEngine)
	if tickErr != nil {
		return tickErr
	}
	if len(expiryResults) != 2 || expiryResults[0].Command != 72 || expiryResults[1].Command != 72 || parameterEngine.players[0].Attack != 1000 || parameterEngine.players[0].Magic != 2000 {
		return fmt.Errorf("parameter expiry projection/state is results=%+v player=%+v", expiryResults, parameterEngine.players[0])
	}

	// FUN_00088a20 retains typed parameter effects, FUN_00071d48 aggregates the
	// live list and FUN_00071e60 derives BATTLE_PARAM from the member's base
	// tuple. 88a20 separately commits a positive MAX_HP delta through 73e9f;
	// expiry merely clamps HP when the effective ceiling falls.
	maxHPRole, err := findPlayerRole(12502532, "ATK_UP_FIXED")
	if err != nil {
		return err
	}
	if maxHPRole.Parameters[1] != "MAX_HP" || fixedBuffRoleValue(*maxHPRole, 60, 1) != 2784 {
		return fmt.Errorf("official MAX_HP parameter role changed: %+v", maxHPRole)
	}
	maxHPEngine := &BattleEngine{catalog: catalog, turn: 1}
	maxHPEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 1, HP: 5000, MaxHP: 10000, BaseMaxHP: 10000,
		Attack: 1000, BaseAttack: 1000,
	}
	maxHPResults, err := maxHPEngine.executeFixedParameter(
		battleAction{memberType: 1, target: 1, cardLevel: 60}, *maxHPRole, 1,
	)
	if err != nil {
		return err
	}
	if len(maxHPResults) != 2 || maxHPEngine.players[0].MaxHP != 12784 || maxHPEngine.players[0].HP != 7784 {
		return fmt.Errorf("official MAX_HP application missed its HP increment: results=%+v player=%+v", maxHPResults, maxHPEngine.players[0])
	}
	// Model an ordinary heal received while the enlarged ceiling is active.
	// Expiry must clip the surplus without applying the increment again.
	maxHPEngine.players[0].HP = 12000
	maxHPEngine.turn = 2
	if premature, tickErr := expireForEffectContract(maxHPEngine); tickErr != nil || len(premature) != 0 {
		return fmt.Errorf("official MAX_HP effect expired early: results=%+v err=%v", premature, tickErr)
	}
	maxHPEngine.turn = 3
	maxHPExpiry, tickErr := expireForEffectContract(maxHPEngine)
	if tickErr != nil {
		return tickErr
	}
	if len(maxHPExpiry) != 1 || maxHPExpiry[0].Command != 72 ||
		maxHPEngine.players[0].MaxHP != 10000 || maxHPEngine.players[0].HP != 10000 {
		return fmt.Errorf("official MAX_HP expiry did not clamp surplus HP to the restored ceiling: results=%+v player=%+v", maxHPExpiry, maxHPEngine.players[0])
	}
	// Explicit release enters FUN_00073420 -> FUN_00072a18 after ResultCmd66.
	// It must project grouped 72, per-effect 71 and the final ResultCmd6; the
	// same final snapshot carries the downward HP ceiling clamp.
	var enemyAllBuffRelease *CombatSkillRole
	for index := range catalog.SkillRoles[30901415] {
		candidate := &catalog.SkillRoles[30901415][index]
		if candidate.Function == "BUFF_RELEASE" {
			enemyAllBuffRelease = candidate
			break
		}
	}
	if enemyAllBuffRelease == nil || enemyAllBuffRelease.Parameters[0] != "100" {
		return errors.New("official enemy all-buff-release contract changed")
	}
	maxHPReleaseEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 1}
	maxHPReleaseEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 1, HP: 5000, MaxHP: 10000, BaseMaxHP: 10000,
		Attack: 1000, BaseAttack: 1000,
	}
	if _, err := maxHPReleaseEngine.executeFixedParameter(
		battleAction{memberType: 1, target: 1, cardLevel: 60}, *maxHPRole, 1,
	); err != nil {
		return err
	}
	maxHPReleaseEngine.players[0].HP = 12000
	explicitRelease, err := maxHPReleaseEngine.executeEnemyRelease(
		&maxHPReleaseEngine.enemies[0], 1, *enemyAllBuffRelease,
	)
	if err != nil {
		return err
	}
	if len(explicitRelease) != 4 || explicitRelease[0].Command != CommandBuffRelease ||
		explicitRelease[1].Command != 72 || explicitRelease[2].Command != CommandBuffLostOne ||
		explicitRelease[3].Command != CommandBattleParam || len(maxHPReleaseEngine.players[0].Effects) != 0 ||
		maxHPReleaseEngine.players[0].MaxHP != 10000 || maxHPReleaseEngine.players[0].HP != 10000 {
		return fmt.Errorf("official explicit MAX_HP release lifecycle is results=%+v player=%+v", explicitRelease, maxHPReleaseEngine.players[0])
	}
	rewriteReleaseEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 1}
	rewriteReleaseEngine.players[0] = battlePlayer{
		MemberType: 1, HP: 10000, MaxHP: 10000, BaseMaxHP: 10000,
		Attribute: "ICE", BaseAttribute: "WIND",
		Effects: []battleEffect{{
			Function: "REWRITE", Attribute: "ICE", Kind: 1, Remaining: 2,
			RoleIndex: 3, AppliedTurn: 1,
		}},
	}
	rewriteRelease, err := rewriteReleaseEngine.executeEnemyRelease(
		&rewriteReleaseEngine.enemies[0], 1, *enemyAllBuffRelease,
	)
	if err != nil {
		return err
	}
	if len(rewriteRelease) != 5 || rewriteRelease[0].Command != CommandBuffRelease ||
		rewriteRelease[1].Command != 72 || rewriteRelease[2].Command != CommandBuffLostOne ||
		rewriteRelease[3].Command != CommandRewrite || rewriteRelease[3].Args[1] != int64(combatAttributeCode("WIND")) ||
		rewriteRelease[4].Command != CommandBattleParam || rewriteReleaseEngine.players[0].Attribute != "WIND" ||
		len(rewriteReleaseEngine.players[0].Effects) != 0 {
		return fmt.Errorf("official explicit rewrite-release lifecycle is results=%+v player=%+v", rewriteRelease, rewriteReleaseEngine.players[0])
	}

	atkBreakRole, err := findPlayerRole(12600092, "ATK_BREAK_FIXED")
	if err != nil {
		return err
	}
	if atkBreakRole.Parameters[1] != "INT" || fixedBuffRoleValue(*atkBreakRole, 60, 1) != 834 {
		return fmt.Errorf("official fixed ATK break role changed: %+v", atkBreakRole)
	}
	breakEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	breakEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000, BaseMaxHP: 10000}
	breakEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 5, HP: 10000, MaxHP: 10000, BaseMaxHP: 10000,
		Magic: 500, BaseMagic: 500,
	}
	breakResults, err := breakEngine.executeEnemyParameterDebuff(
		battleAction{memberType: 1, target: 5, cardLevel: 60}, *atkBreakRole, 1,
	)
	if err != nil {
		return err
	}
	if len(breakResults) != 2 || breakEngine.enemies[0].Magic != 0 || len(breakEngine.enemies[0].Effects) != 1 || breakEngine.enemies[0].Effects[0].Delta != -834 {
		return fmt.Errorf("official fixed ATK break clamp is results=%+v enemy=%+v", breakResults, breakEngine.enemies[0])
	}
	for _, turn := range []int{2, 3} {
		breakEngine.turn = turn
		if premature, tickErr := expireForEffectContract(breakEngine); tickErr != nil || len(premature) != 0 {
			return fmt.Errorf("official fixed ATK break expired at turn %d: results=%+v err=%v", turn, premature, tickErr)
		}
	}
	breakEngine.turn = 4
	breakExpiry, tickErr := expireForEffectContract(breakEngine)
	if tickErr != nil {
		return tickErr
	}
	if len(breakExpiry) != 1 || breakEngine.enemies[0].Magic != 500 {
		return fmt.Errorf("clamped fixed ATK break rebounded past base: results=%+v enemy=%+v", breakExpiry, breakEngine.enemies[0])
	}

	stackedBreak := &BattleEngine{turn: 2}
	stackedBreak.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 5, HP: 1000, MaxHP: 1000, BaseMaxHP: 1000,
		Attack: 0, BaseAttack: 500,
		Effects: []battleEffect{
			{Function: "ATK_BREAK_FIXED", Parameter: "ATK", Value: 400, Delta: -400, Kind: 2, Remaining: 1, AppliedTurn: 1, RoleIndex: 1},
			{Function: "ATK_BREAK_BY_SELF_PARAM", Parameter: "ATK", Value: 400, Delta: -400, Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 2},
		},
	}
	stackedBreak.enemyCount = 1
	firstBreakExpiry, tickErr := expireForEffectContract(stackedBreak)
	if tickErr != nil {
		return tickErr
	}
	if len(firstBreakExpiry) != 1 || firstBreakExpiry[0].Command != 72 || !equalBattleArgs(firstBreakExpiry[0].Args, []int64{5, 0, 100, 4, 1}) ||
		stackedBreak.enemies[0].Attack != 100 || len(stackedBreak.enemies[0].Effects) != 1 {
		return fmt.Errorf("stacked clamped parameter recompute is results=%+v enemy=%+v", firstBreakExpiry, stackedBreak.enemies[0])
	}
	stackedBreak.turn = 3
	secondBreakExpiry, tickErr := expireForEffectContract(stackedBreak)
	if tickErr != nil {
		return tickErr
	}
	if len(secondBreakExpiry) != 1 || stackedBreak.enemies[0].Attack != 500 || len(stackedBreak.enemies[0].Effects) != 0 {
		return fmt.Errorf("stacked parameter expiry did not restore base: results=%+v enemy=%+v", secondBreakExpiry, stackedBreak.enemies[0])
	}

	var enemySelfBreak *CombatSkillRole
	for index := range catalog.SkillRoles[30301132] {
		candidate := &catalog.SkillRoles[30301132][index]
		if candidate.Function == "ATK_BREAK_BY_SELF_PARAM" && candidate.Parameters[1] == "ATK" {
			enemySelfBreak = candidate
			break
		}
	}
	if enemySelfBreak == nil || selfScaledParameterValue(*enemySelfBreak, 0, 1, 1000) != 1000 {
		return fmt.Errorf("official enemy self-parameter ATK break changed: %+v", enemySelfBreak)
	}
	enemySelfEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	enemySelfEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 1, HP: 1000, MaxHP: 1000, BaseMaxHP: 1000,
		Attack: 500, BaseAttack: 500,
	}
	enemySelfEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 5, HP: 1000, MaxHP: 1000, BaseMaxHP: 1000,
		Recovery: 1000, BaseRecovery: 1000,
	}
	enemySelfResults, err := enemySelfEngine.executeEnemyParameterRole(&enemySelfEngine.enemies[0], 1, *enemySelfBreak)
	if err != nil {
		return err
	}
	if len(enemySelfResults) != 2 || enemySelfEngine.players[0].Attack != 0 || enemySelfEngine.players[0].Effects[0].Delta != -1000 {
		return fmt.Errorf("official enemy self-parameter ATK break is results=%+v player=%+v", enemySelfResults, enemySelfEngine.players[0])
	}

	// Native role 142 is a typed ceiling status, not an ordinary ATK/INT/MND
	// buff. Official 11109072 raises the ATK ceiling by 6006 at level 60 while
	// leaving raw BaseAttack untouched. The projected ATK and ResultCmd6
	// follow the live ceiling (original API projection/expiry chain).
	parameterLimitRole, err := findPlayerRole(11109072, "PARAM_LIMIT_BREAK_FIXED")
	if err != nil {
		return err
	}
	if value := fixedBuffRoleValue(*parameterLimitRole, 60, 1); value != 6006 {
		return fmt.Errorf("official parameter-limit value is %d, want 6006", value)
	}
	limitEngine := &BattleEngine{catalog: catalog, turn: 1}
	limitEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 1, HP: 10000, MaxHP: 10000, Attack: 150000,
		LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999,
	}
	limitResults, err := limitEngine.executePlayerParameterLimit(battleAction{memberType: 1, target: 1, cardLevel: 60}, *parameterLimitRole, 1)
	if err != nil {
		return err
	}
	if len(limitResults) != 2 || limitResults[0].Command != CommandBuff || limitResults[1].Command != CommandBattleParam || limitResults[1].Args[8] != 106005 || limitEngine.players[0].BaseAttack != 150000 || limitEngine.players[0].Attack != 106005 || combatStatValue(&limitEngine.players[0], "ATK") != 106005 {
		return fmt.Errorf("parameter-limit projection/state is results=%+v player=%+v", limitResults, limitEngine.players[0])
	}
	for turn := 2; turn <= 4; turn++ {
		limitEngine.turn = turn
		limitResults, tickErr = expireForEffectContract(limitEngine)
		if tickErr != nil {
			return tickErr
		}
	}
	if len(limitResults) != 1 || limitResults[0].Command != 72 || limitEngine.players[0].BaseAttack != 150000 || limitEngine.players[0].Attack != 99999 || combatStatValue(&limitEngine.players[0], "ATK") != 99999 {
		return fmt.Errorf("parameter-limit expiry is results=%+v player=%+v", limitResults, limitEngine.players[0])
	}

	// The shared ENDURE producer accepts source percentages and role targets.
	// Keep this concrete SELF/lifetime control; whole-table CN1-percent/
	// SELF-or-SELECT census constraints do not describe the modern source.

	endureRole, err := findEnemyRole(36901109, "ENDURE", "")
	if err != nil {
		return err
	}
	if endureRole.Target != "SELF" || endureRole.Parameters[0] != "2" || endureRole.Parameters[1] != "1" || endureRole.Parameters[2] != "0" {
		return fmt.Errorf("official enemy ENDURE 36901109 changed: %+v", endureRole)
	}
	endureEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	endureEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000}
	endureApply, err := endureEngine.executeEnemyPersistentEffect(&endureEngine.enemies[0], 1, *endureRole)
	if err != nil {
		return err
	}
	if len(endureApply) != 1 || endureApply[0].Command != CommandBuff || len(endureApply[0].Args) != 11 ||
		endureApply[0].Args[0] != 5 || endureApply[0].Args[1] != int64(endureRole.RoleIndex) ||
		endureApply[0].Args[2] != 0 || endureApply[0].Args[3] != int64(battleBuffCodes["ENDURE"]) ||
		endureApply[0].Args[7] != 0 || len(endureEngine.enemies[0].Effects) != 1 {
		return fmt.Errorf("official enemy ENDURE application is results=%+v enemy=%+v", endureApply, endureEngine.enemies[0])
	}
	endure := endureEngine.enemies[0].Effects[0]
	if endure.Value != 1 || endure.Rate != 1 || endure.Remaining != 2 || endure.Source != 5 || endure.ListType != 0 {
		return fmt.Errorf("official enemy ENDURE durable state is %+v", endure)
	}
	endureDamage := endureEngine.resolveIncomingDamageEffects(&endureEngine.enemies[0].Effects, 5000, endureEngine.enemies[0].HP, "FIRE", "MAGIC")
	if endureDamage.Damage != 5000 || endureEngine.enemies[0].Effects[0].Remaining != 2 {
		return fmt.Errorf("official enemy ENDURE ordinary damage is resolution=%+v effects=%+v", endureDamage, endureEngine.enemies[0].Effects)
	}
	endureEngine.enemies[0].HP = nativeHPCommit(endureEngine.enemies[0].HP, 1000, -endureDamage.Damage, endureEngine.enemies[0].Effects)
	lethalEndureDamage := endureEngine.resolveIncomingDamageEffects(&endureEngine.enemies[0].Effects, 200000, endureEngine.enemies[0].HP, "FIRE", "MAGIC")
	if lethalEndureDamage.Damage != 200000 || endureEngine.enemies[0].HP != 10 || endureEngine.enemies[0].Effects[0].Remaining != 2 {
		return fmt.Errorf("official enemy ENDURE nonlethal damage is resolution=%+v effects=%+v", lethalEndureDamage, endureEngine.enemies[0].Effects)
	}
	endureEngine.enemies[0].HP = nativeHPCommit(endureEngine.enemies[0].HP, 1000, -lethalEndureDamage.Damage, endureEngine.enemies[0].Effects)
	endureEngine.turn = 2
	endureTick, tickErr := expireForEffectContract(endureEngine)
	if tickErr != nil {
		return tickErr
	}
	if len(endureTick) != 0 || len(endureEngine.enemies[0].Effects) != 1 || endureEngine.enemies[0].Effects[0].Remaining != 1 {
		return fmt.Errorf("official enemy ENDURE premature expiry is results=%+v enemy=%+v", endureTick, endureEngine.enemies[0])
	}
	endureEngine.turn = 3
	endureTick, tickErr = expireForEffectContract(endureEngine)
	if tickErr != nil {
		return tickErr
	}
	if len(endureTick) != 1 || endureTick[0].Command != 72 || len(endureEngine.enemies[0].Effects) != 0 || endureEngine.enemies[0].HP != 10 {
		return fmt.Errorf("official enemy ENDURE natural expiry is results=%+v enemy=%+v", endureTick, endureEngine.enemies[0])
	}

	// Official 12507862 is self/2 turns/one revival at 50 percent MaxHP.
	// Native FUN_0005feb6 emits effect, heal, remaining-count and loss rows in
	// that order after the lethal action has committed.
	gutsRole, err := findPlayerRole(12507862, "GUTS")
	if err != nil {
		return err
	}
	gutsEngine := &BattleEngine{catalog: catalog, turn: 1}
	gutsEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000}
	gutsApply, err := gutsEngine.executePersistentEffect(battleAction{memberType: 1, target: 1, cardLevel: 60}, *gutsRole, 60, 1)
	if err != nil {
		return err
	}
	if len(gutsApply) != 1 || len(gutsEngine.players[0].Effects) != 1 || gutsEngine.players[0].Effects[0].Uses != 1 || gutsEngine.players[0].Effects[0].Rate != 50 || gutsApply[0].Args[7] != 1 {
		return fmt.Errorf("official GUTS apply state is results=%+v player=%+v", gutsApply, gutsEngine.players[0])
	}
	gutsEngine.players[0].HP = 0
	gutsHP, gutsResults := resolveGuts(1, 10000, 0, &gutsEngine.players[0].Effects)
	wantedGutsCommands := []int{53, 61, 200, 72}
	if gutsHP != 5000 || len(gutsResults) != len(wantedGutsCommands) || len(gutsEngine.players[0].Effects) != 0 {
		return fmt.Errorf("official GUTS resolution is hp=%d results=%+v effects=%+v", gutsHP, gutsResults, gutsEngine.players[0].Effects)
	}
	for index, command := range wantedGutsCommands {
		if gutsResults[index].Command != command {
			return fmt.Errorf("official GUTS command %d is %d, want %d", index, gutsResults[index].Command, command)
		}
	}
	if len(gutsResults[0].Args) != 3 || gutsResults[0].Args[0] != 1 || gutsResults[0].Args[1] != 1 ||
		gutsResults[0].Args[2] != int64(battleBuffCodes["GUTS"]) ||
		gutsResults[1].Args[2] != 5000 || gutsResults[1].Args[3] != 5000 || gutsResults[2].Args[2] != 0 {
		return fmt.Errorf("official GUTS heal/count projection is %+v", gutsResults)
	}

	// This enemy GUTS control installs its one-use state on a selected player
	// while retaining enemy source identity. Other source counts, durations
	// and revival percentages are not engine invariants.
	// Exercise that opposite-side object graph and the second lethal commit;
	// player 12507862 alone cannot prove either behavior.

	enemyGutsVariants := catalog.Skills[38111141]
	enemyGutsRole, err := findEnemyRole(38111141, "GUTS", "")
	if err != nil {
		return err
	}
	if len(enemyGutsVariants) != 1 || enemyGutsVariants[0].Target != "USER_ONE" || enemyGutsRole.Target != "SELECT" {
		return fmt.Errorf("official enemy GUTS 38111141 object graph changed: skill=%+v role=%+v", enemyGutsVariants, enemyGutsRole)
	}
	enemyGutsEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	enemyGutsEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000}
	enemyGutsEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	enemyGutsApply, err := enemyGutsEngine.executeEnemyPersistentEffect(&enemyGutsEngine.enemies[0], 1, *enemyGutsRole)
	if err != nil {
		return err
	}
	if len(enemyGutsApply) != 1 || enemyGutsApply[0].Command != CommandBuff || len(enemyGutsApply[0].Args) != 11 ||
		enemyGutsApply[0].Args[0] != 1 || enemyGutsApply[0].Args[3] != int64(battleBuffCodes["GUTS"]) ||
		enemyGutsApply[0].Args[7] != 1 || len(enemyGutsEngine.players[0].Effects) != 1 ||
		len(enemyGutsEngine.enemies[0].Effects) != 0 {
		return fmt.Errorf("official enemy GUTS application is results=%+v player=%+v enemy=%+v", enemyGutsApply, enemyGutsEngine.players[0], enemyGutsEngine.enemies[0])
	}
	enemyGutsEffect := enemyGutsEngine.players[0].Effects[0]
	if enemyGutsEffect.Source != 5 || enemyGutsEffect.Remaining != 2 || enemyGutsEffect.Uses != 1 ||
		enemyGutsEffect.Rate != 20 || enemyGutsEffect.ListType != 0 {
		return fmt.Errorf("official enemy GUTS durable state is %+v", enemyGutsEffect)
	}
	enemyGutsEngine.players[0].HP = 0
	enemyGutsHP, enemyGutsResults := resolveGuts(1, 10000, 0, &enemyGutsEngine.players[0].Effects)
	if enemyGutsHP != 2000 || len(enemyGutsResults) != 4 || len(enemyGutsEngine.players[0].Effects) != 0 ||
		enemyGutsResults[0].Command != 53 || enemyGutsResults[1].Command != 61 ||
		enemyGutsResults[1].Args[2] != 2000 || enemyGutsResults[1].Args[3] != 2000 ||
		enemyGutsResults[2].Command != 200 || enemyGutsResults[2].Args[2] != 0 || enemyGutsResults[3].Command != 72 {
		return fmt.Errorf("official enemy GUTS lethal recovery is hp=%d results=%+v effects=%+v", enemyGutsHP, enemyGutsResults, enemyGutsEngine.players[0].Effects)
	}
	secondGutsHP, secondGutsResults := resolveGuts(1, 10000, 0, &enemyGutsEngine.players[0].Effects)
	if secondGutsHP != 0 || len(secondGutsResults) != 0 {
		return fmt.Errorf("official enemy GUTS repeated lethal commit is hp=%d results=%+v", secondGutsHP, secondGutsResults)
	}

	// Ordinary 44096005 inherits an ENEMY_ONE selection and must
	// install NORMAL state on that concrete part, whereas passive 40000080
	// inherits SELF and installs PASSIVE state on the acting enemy. A synthetic
	// SELF role cannot prove either object graph or list-type projection.

	attrHideRole, err := findEnemyRole(44096005, "ATTR_HIDE", "")
	if err != nil {
		return err
	}
	if attrHideRole.Target != "SELECT" || attrHideRole.Parameters[0] != "99" {
		return fmt.Errorf("official enemy ATTR_HIDE 44096005 changed: %+v", attrHideRole)
	}
	attrHideEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 2}
	attrHideEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000}
	attrHideEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 1000, MaxHP: 1000}
	attrHideApply, err := attrHideEngine.executeEnemyPersistentEffect(&attrHideEngine.enemies[0], 6, *attrHideRole)
	if err != nil {
		return err
	}
	if len(attrHideApply) != 1 || attrHideApply[0].Command != CommandBuff || len(attrHideApply[0].Args) != 11 ||
		attrHideApply[0].Args[0] != 6 || attrHideApply[0].Args[2] != 0 ||
		attrHideApply[0].Args[3] != int64(battleBuffCodes["ATTR_HIDE"]) || attrHideApply[0].Args[7] != 0 ||
		len(attrHideEngine.enemies[0].Effects) != 0 || len(attrHideEngine.enemies[1].Effects) != 1 {
		return fmt.Errorf("official enemy ATTR_HIDE normal application is results=%+v enemies=%+v", attrHideApply, attrHideEngine.enemies[:2])
	}
	attrHide := attrHideEngine.enemies[1].Effects[0]
	if attrHide.Value != 0 || attrHide.Rate != 0 || attrHide.Remaining != 99 || attrHide.Source != 5 || attrHide.ListType != 0 {
		return fmt.Errorf("official enemy ATTR_HIDE normal state is %+v", attrHide)
	}
	attrHideEngine.turn = 2
	attrHideTick, tickErr := expireForEffectContract(attrHideEngine)
	if tickErr != nil {
		return tickErr
	}
	if len(attrHideTick) != 0 || len(attrHideEngine.enemies[1].Effects) != 1 || attrHideEngine.enemies[1].Effects[0].Remaining != 98 {
		return fmt.Errorf("official enemy ATTR_HIDE duration tick is results=%+v enemy=%+v", attrHideTick, attrHideEngine.enemies[1])
	}
	passiveAttrHideRole, err := findEnemyRole(40000080, "ATTR_HIDE", "")
	if err != nil {
		return err
	}
	if passiveAttrHideRole.Target != "SELECT" || passiveAttrHideRole.Parameters[0] != "999" {
		return fmt.Errorf("official passive ATTR_HIDE 40000080 changed: %+v", passiveAttrHideRole)
	}
	passiveAttrHideEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	passiveAttrHideEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000}
	passiveAttrHideApply, err := passiveAttrHideEngine.executeEnemyPersistentEffectWithListType(
		&passiveAttrHideEngine.enemies[0], 5, *passiveAttrHideRole, 1,
	)
	if err != nil {
		return err
	}
	if len(passiveAttrHideApply) != 1 || passiveAttrHideApply[0].Command != CommandPassiveBuff ||
		len(passiveAttrHideApply[0].Args) != 11 || passiveAttrHideApply[0].Args[0] != 5 ||
		passiveAttrHideApply[0].Args[2] != 1 || passiveAttrHideApply[0].Args[3] != int64(battleBuffCodes["ATTR_HIDE"]) ||
		passiveAttrHideApply[0].Args[7] != 0 || len(passiveAttrHideEngine.enemies[0].Effects) != 1 ||
		passiveAttrHideEngine.enemies[0].Effects[0].Remaining != 999 ||
		passiveAttrHideEngine.enemies[0].Effects[0].ListType != 1 || passiveAttrHideEngine.enemies[0].Effects[0].Source != 5 {
		return fmt.Errorf("official enemy ATTR_HIDE passive application is results=%+v enemy=%+v", passiveAttrHideApply, passiveAttrHideEngine.enemies[0])
	}

	// Native battle-attack-option case 0/1/2/3/4/5/7 consumes the attack
	// operators below. These official representative rows keep the master
	// columns and the in-memory arithmetic tied together without a client run.
	operatorActor := &battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 8000, MaxHP: 10000, Attack: 1000, Magic: 2000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	piercingRole, err := findPlayerRole(11100472, "ATK_OP_PIERCING")
	if err != nil {
		return err
	}
	piercing := collectAttackModifiers([]CombatSkillRole{*piercingRole}, operatorActor, 60, 2, false)
	if piercing.piercingRate != 65 {
		return fmt.Errorf("official level-scaled piercing is %d, want 65", piercing.piercingRate)
	}
	damageIncreaseRole, err := findPlayerRole(11101714, "ATK_OP_DAMAGE_INCREASE")
	if err != nil {
		return err
	}
	damageIncrease := collectAttackModifiers([]CombatSkillRole{*damageIncreaseRole}, operatorActor, 60, 1, false)
	if damageIncrease.damageIncrease != 2400 {
		return fmt.Errorf("official current-HP damage increase is %d, want 2400", damageIncrease.damageIncrease)
	}
	drainRole, err := findPlayerRole(11100492, "ATK_OP_DRAIN")
	if err != nil {
		return err
	}
	drain := collectAttackModifiers([]CombatSkillRole{*drainRole}, operatorActor, 60, 2, false)
	if drain.drainRate != 50 || drain.drainCap != 0 || 10000*drain.drainRate/100 != 5000 {
		return fmt.Errorf("official drain projection is rate %d cap %d", drain.drainRate, drain.drainCap)
	}
	drainAllRole, err := findPlayerRole(11108772, "ATK_OP_DRAIN_ALL")
	if err != nil {
		return err
	}
	drainAll := collectAttackModifiers([]CombatSkillRole{*drainAllRole}, operatorActor, 60, 1, false)
	if drainAll.drainAllRate != 10 || drainAll.drainAllCap != 0 || drainAll.drainRate != 0 {
		return fmt.Errorf("official all-member drain projection is all rate %d cap %d self rate %d", drainAll.drainAllRate, drainAll.drainAllCap, drainAll.drainRate)
	}
	// Native attack-option subtype 7 keeps an independent team-drain slot.
	// Exercise the complete attack consumer so the contract proves that every
	// living member receives ten percent of committed damage while a dead
	// member is neither revived nor sent a heal result.
	drainAllEngine := &BattleEngine{enemyCount: 1}
	for index := range drainAllEngine.players {
		drainAllEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 5000 + index*1000, MaxHP: 10000}
	}
	drainAllEngine.players[3].HP = 0
	drainAllEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	drainAllAttack := CombatSkillRole{RoleIndex: 9, Function: "ATTACK_AA", Target: "SELECT"}
	drainAllAttack.Parameters[0] = "1000"
	drainAllAttack.Parameters[4] = "1"
	drainAllAttack.Parameters[7] = "LIGHT"
	drainAllAttack.Parameters[8] = "PHYSICS"
	drainAllResults, drainAllErr := drainAllEngine.executePlayerAttack(battleAction{
		memberType: 1, target: 5, cardLevel: 60,
		roles: []CombatSkillRole{*drainAllRole, drainAllAttack},
	}, drainAllAttack, 1)
	if drainAllErr != nil {
		return drainAllErr
	}
	if drainAllEngine.enemies[0].HP != 9000 || drainAllEngine.players[0].HP != 5100 ||
		drainAllEngine.players[1].HP != 6100 || drainAllEngine.players[2].HP != 7100 ||
		drainAllEngine.players[3].HP != 0 {
		return fmt.Errorf("official all-member drain commit is enemy=%d players=%d/%d/%d/%d",
			drainAllEngine.enemies[0].HP, drainAllEngine.players[0].HP, drainAllEngine.players[1].HP,
			drainAllEngine.players[2].HP, drainAllEngine.players[3].HP)
	}
	healRows := 0
	for _, result := range drainAllResults {
		if result.Command != 61 {
			continue
		}
		healRows++
		if len(result.Args) < 4 || result.Args[1] != int64(drainAllAttack.RoleIndex) || result.Args[2] != 100 {
			return fmt.Errorf("official all-member drain heal row is %+v", result)
		}
	}
	if healRows != 3 {
		return fmt.Errorf("official all-member drain emitted %d heal rows, want 3", healRows)
	}
	revengeRole, err := findPlayerRole(11100752, "ATK_OP_REVENGE")
	if err != nil {
		return err
	}
	revenge := collectAttackModifiers([]CombatSkillRole{*revengeRole}, operatorActor, 60, 2, false)
	operatorActor.DamageTaken = 4000
	if bonus := attackRevengeBonus(operatorActor.DamageTaken, operatorActor, revenge.revengeRate, revenge.revengeParameter, revenge.revengeCapRate); bonus != 1600 {
		return fmt.Errorf("official cumulative-damage revenge bonus is %d, want 1600", bonus)
	}
	var nowTurnRevengeRole *CombatSkillRole
	for index := range catalog.SkillRoles[44117005] {
		candidate := &catalog.SkillRoles[44117005][index]
		if candidate.Function == "ATK_OP_NOW_TURN_REVENGE" {
			nowTurnRevengeRole = candidate
			break
		}
	}
	if nowTurnRevengeRole == nil {
		return errors.New("official enemy current-turn revenge role is missing from skill 44117005")
	}
	nowTurnActor := &battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 6000, MaxHP: 10000, Attack: 2000}
	nowTurnRevenge := collectAttackModifiers([]CombatSkillRole{*nowTurnRevengeRole}, nowTurnActor, 0, 1, false)
	if nowTurnRevenge.nowTurnRevengeRate != 20 || nowTurnRevenge.nowTurnRevengeParameter != "MAX_HP" || nowTurnRevenge.nowTurnRevengeCapRate != 5 ||
		nowTurnRevenge.revengeRate != 0 {
		return fmt.Errorf("official current-turn revenge projection is %+v", nowTurnRevenge)
	}
	if bonus := attackRevengeBonus(4000, nowTurnActor, nowTurnRevenge.nowTurnRevengeRate, nowTurnRevenge.nowTurnRevengeParameter, nowTurnRevenge.nowTurnRevengeCapRate); bonus != 500 {
		return fmt.Errorf("official current-turn revenge capped bonus is %d, want 500", bonus)
	}
	// Subtype 5 is consumed by the normal enemy attack, not emitted as an
	// independent result. Confirm the 500-point capped bonus reaches damage and
	// remains separate from cumulative revenge subtype 0.
	nowTurnEngine := &BattleEngine{enemyCount: 1}
	nowTurnEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000}
	for index := 1; index < maxRoomMembers; index++ {
		nowTurnEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1}
	}
	nowTurnEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 6000, MaxHP: 10000, Attack: 2000, TurnDamage: 4000}
	nowTurnAttack := CombatSkillRole{RoleIndex: 3, Function: "ATTACK_AA", Target: "SELECT"}
	nowTurnAttack.Parameters[0] = "1000"
	nowTurnAttack.Parameters[4] = "1"
	nowTurnAttack.Parameters[7] = "NULL"
	nowTurnAttack.Parameters[8] = "PHYSICS"
	nowTurnResults, nowTurnErr := nowTurnEngine.executeEnemyAttack(&nowTurnEngine.enemies[0], 1, nowTurnAttack, []CombatSkillRole{nowTurnAttack, *nowTurnRevengeRole})
	if nowTurnErr != nil {
		return nowTurnErr
	}
	if nowTurnEngine.players[0].HP != 8500 || len(nowTurnResults) < 2 || nowTurnResults[0].Command != 60 ||
		len(nowTurnResults[0].Args) < 3 || nowTurnResults[0].Args[2] != -1500 {
		return fmt.Errorf("official current-turn revenge attack is hp=%d results=%+v", nowTurnEngine.players[0].HP, nowTurnResults)
	}
	attrInvalidRole, err := findPlayerRole(11101612, "ATK_OP_ATTR_RATE_DOWN_INVALID")
	if err != nil {
		return err
	}
	attrInvalid := collectAttackModifiers([]CombatSkillRole{*attrInvalidRole}, operatorActor, 60, 1, false)
	if !attrInvalid.attrRateDownInvalid || maxInt(100, 50) != 100 {
		return errors.New("official attribute-disadvantage invalidation is inactive")
	}
	attrInvalidAttackRole, err := findPlayerRole(11101612, "ATTACK_AA")
	if err != nil {
		return err
	}
	if attrInvalidRole.Target != "SELECT" || attrInvalidAttackRole.Target != "SELECT" ||
		attrInvalidAttackRole.Parameters[0] != "2361" || attrInvalidAttackRole.Parameters[1] != "40000" ||
		attrInvalidAttackRole.Parameters[2] != "1000" || attrInvalidAttackRole.Parameters[4] != "1" ||
		attrInvalidAttackRole.Parameters[5] != "ATK" || attrInvalidAttackRole.Parameters[6] != "150" ||
		attrInvalidAttackRole.Parameters[7] != "ICE" || attrInvalidAttackRole.Parameters[8] != "PHYSICS" {
		return fmt.Errorf("official attribute-disadvantage invalidation attack rows changed: modifier=%+v attack=%+v", attrInvalidRole, attrInvalidAttackRole)
	}
	newAttrInvalidEngine := func() *BattleEngine {
		engine := &BattleEngine{rng: newXorShift128(11), enemyCount: 1}
		engine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, Attack: 1000, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		for index := 1; index < maxRoomMembers; index++ {
			engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1}
		}
		engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 20000, MaxHP: 20000}
		engine.enemies[0].Level.AttributeRates[combatAttributeIndex("ICE")] = 50
		return engine
	}
	attrInvalidEngine := newAttrInvalidEngine()
	attrInvalidResults, attrInvalidErr := attrInvalidEngine.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidRole, *attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if attrInvalidErr != nil {
		return attrInvalidErr
	}
	if len(attrInvalidResults) != 2 || attrInvalidResults[0].Command != 60 ||
		attrInvalidResults[0].Args[2] != -8641 || attrInvalidResults[0].Args[6] != 100 ||
		attrInvalidResults[0].Args[7] != 1 || attrInvalidEngine.enemies[0].HP != 11359 ||
		attrInvalidResults[1].Command != CommandHP {
		return fmt.Errorf("official attribute-disadvantage invalidation attack is enemy=%+v results=%+v", attrInvalidEngine.enemies[0], attrInvalidResults)
	}
	attrDisadvantageEngine := newAttrInvalidEngine()
	attrDisadvantageResults, attrDisadvantageErr := attrDisadvantageEngine.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if attrDisadvantageErr != nil {
		return attrDisadvantageErr
	}
	if len(attrDisadvantageResults) != 2 || attrDisadvantageResults[0].Command != 60 ||
		attrDisadvantageResults[0].Args[2] != -4320 || attrDisadvantageResults[0].Args[6] != 50 ||
		attrDisadvantageResults[0].Args[7] != 1 || attrDisadvantageEngine.enemies[0].HP != 15680 ||
		attrDisadvantageResults[1].Command != CommandHP {
		return fmt.Errorf("official ordinary attribute-disadvantage attack is enemy=%+v results=%+v", attrDisadvantageEngine.enemies[0], attrDisadvantageResults)
	}

	// CRITICAL_DOWN has no player producer in the active CN master. Bind its
	// enemy SELECT application to the same official player attack used above so
	// the negative-critical half-damage branch is proven end to end rather than
	// only through a synthetic battleEffect passed to nativeCriticalOutcome.
	var criticalDownRole *CombatSkillRole
	for index := range catalog.SkillRoles[34601718] {
		candidate := &catalog.SkillRoles[34601718][index]
		if candidate.Function == "CRITICAL_DOWN" {
			criticalDownRole = candidate
			break
		}
	}
	if criticalDownRole == nil {
		return errors.New("official critical-down role is missing from skill 34601718")
	}
	if criticalDownRole.Target != "SELECT" || criticalDownRole.Parameters[0] != "99" ||
		criticalDownRole.Parameters[1] != "700" || criticalDownRole.Parameters[2] != "0" {
		return fmt.Errorf("official critical-down role changed: %+v", criticalDownRole)
	}
	newCriticalDownEngine := func() *BattleEngine {
		engine := &BattleEngine{catalog: catalog, rng: newXorShift128(11), turn: 1, enemyCount: 1}
		engine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, Attack: 1000, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		for index := 1; index < maxRoomMembers; index++ {
			engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1}
		}
		engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 20000, MaxHP: 20000}
		engine.enemies[0].Level.AttributeRates[combatAttributeIndex("ICE")] = 100
		return engine
	}
	criticalDownEngine := newCriticalDownEngine()
	criticalDownApply, err := criticalDownEngine.executeEnemyRole(
		&criticalDownEngine.enemies[0], 1, *criticalDownRole, []CombatSkillRole{*criticalDownRole},
	)
	if err != nil {
		return err
	}
	if len(criticalDownApply) != 2 || criticalDownApply[0].Command != CommandBuff || len(criticalDownApply[0].Args) != 11 ||
		criticalDownApply[0].Args[0] != 1 || criticalDownApply[0].Args[3] != int64(battleBuffCodes["CRITICAL_DOWN"]) ||
		criticalDownApply[0].Args[7] != 0 || len(criticalDownEngine.players[0].Effects) != 1 ||
		criticalDownEngine.players[0].Effects[0].Function != "CRITICAL_DOWN" || criticalDownEngine.players[0].Effects[0].Rate != 700 ||
		criticalDownEngine.players[0].Effects[0].Remaining != 99 || criticalDownEngine.players[0].Effects[0].Source != 5 ||
		criticalDownEngine.players[0].Effects[0].Kind != 2 || criticalDownApply[1].Command != CommandBattleParam ||
		!equalBattleArgs(criticalDownApply[1].Args, battleParameterArgs(1, 10000, 10000, 1000, 0, 0, 0, 0, 99999, 99999, 99999)) {
		return fmt.Errorf("official critical-down application is results=%+v player=%+v", criticalDownApply, criticalDownEngine.players[0])
	}
	criticalDownResults, err := criticalDownEngine.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if err != nil {
		return err
	}
	if len(criticalDownResults) != 2 || criticalDownResults[0].Command != 60 ||
		criticalDownResults[0].Args[2] != -2880 || criticalDownResults[0].Args[6] != 100 ||
		criticalDownResults[0].Args[7] != -1 || criticalDownEngine.enemies[0].HP != 17120 ||
		criticalDownResults[1].Command != CommandHP {
		return fmt.Errorf("official critical-down negative-critical attack is enemy=%+v results=%+v", criticalDownEngine.enemies[0], criticalDownResults)
	}
	if next := criticalDownEngine.rng.next(); next != 269032894 {
		return fmt.Errorf("official critical-down attack consumed wrong RNG count; next=%d, want 269032894", next)
	}
	ordinaryCriticalEngine := newCriticalDownEngine()
	ordinaryCriticalResults, err := ordinaryCriticalEngine.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if err != nil {
		return err
	}
	if len(ordinaryCriticalResults) != 2 || ordinaryCriticalResults[0].Command != 60 ||
		ordinaryCriticalResults[0].Args[2] != -8641 || ordinaryCriticalResults[0].Args[6] != 100 ||
		ordinaryCriticalResults[0].Args[7] != 1 || ordinaryCriticalEngine.enemies[0].HP != 11359 ||
		ordinaryCriticalResults[1].Command != CommandHP {
		return fmt.Errorf("official ordinary positive-critical control is enemy=%+v results=%+v", ordinaryCriticalEngine.enemies[0], ordinaryCriticalResults)
	}

	// Complete both active CRITICAL_UP producer sides. Player 11107442 reaches
	// the native +1000 clamp; seed 3 therefore changes the same official attack
	// from a non-critical hit to a positive critical without changing power or
	// attribute rate.
	playerCriticalUpRole, err := findPlayerRole(11107442, "CRITICAL_UP")
	if err != nil {
		return err
	}
	if playerCriticalUpRole.Target != "SELF" || playerCriticalUpRole.Parameters[0] != "2" ||
		playerCriticalUpRole.Parameters[1] != "1000" || playerCriticalUpRole.Parameters[2] != "0" {
		return fmt.Errorf("official player critical-up role changed: %+v", playerCriticalUpRole)
	}
	playerCriticalUpEngine := newCriticalDownEngine()
	playerCriticalUpEngine.rng = newXorShift128(3)
	playerCriticalUpApply, err := playerCriticalUpEngine.executePersistentEffect(
		battleAction{memberType: 1, target: 1, cardLevel: 60, roles: []CombatSkillRole{*playerCriticalUpRole}},
		*playerCriticalUpRole, 60, 1,
	)
	if err != nil {
		return err
	}
	if len(playerCriticalUpApply) != 1 || playerCriticalUpApply[0].Command != CommandBuff ||
		playerCriticalUpApply[0].Args[0] != 1 || playerCriticalUpApply[0].Args[3] != int64(battleBuffCodes["CRITICAL_UP"]) ||
		playerCriticalUpApply[0].Args[7] != 0 || len(playerCriticalUpEngine.players[0].Effects) != 1 ||
		playerCriticalUpEngine.players[0].Effects[0].Rate != 1000 || playerCriticalUpEngine.players[0].Effects[0].Remaining != 2 ||
		playerCriticalUpEngine.players[0].Effects[0].Source != 1 || playerCriticalUpEngine.players[0].Effects[0].Kind != 1 {
		return fmt.Errorf("official player critical-up application is results=%+v player=%+v", playerCriticalUpApply, playerCriticalUpEngine.players[0])
	}
	playerCriticalUpResults, err := playerCriticalUpEngine.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if err != nil {
		return err
	}
	if len(playerCriticalUpResults) != 2 || playerCriticalUpResults[0].Command != 60 ||
		playerCriticalUpResults[0].Args[2] != -8641 || playerCriticalUpResults[0].Args[7] != 1 ||
		playerCriticalUpEngine.enemies[0].HP != 11359 {
		return fmt.Errorf("official player critical-up attack is enemy=%+v results=%+v", playerCriticalUpEngine.enemies[0], playerCriticalUpResults)
	}
	playerCriticalControlEngine := newCriticalDownEngine()
	playerCriticalControlEngine.rng = newXorShift128(3)
	playerCriticalControlResults, err := playerCriticalControlEngine.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if err != nil {
		return err
	}
	if len(playerCriticalControlResults) != 2 || playerCriticalControlResults[0].Args[2] != -5761 ||
		playerCriticalControlResults[0].Args[7] != 0 || playerCriticalControlEngine.enemies[0].HP != 14239 {
		return fmt.Errorf("official player critical-up control is enemy=%+v results=%+v", playerCriticalControlEngine.enemies[0], playerCriticalControlResults)
	}
	if next := playerCriticalUpEngine.rng.next(); next != 407442942 {
		return fmt.Errorf("official player critical-up attack consumed wrong RNG count; next=%d, want 407442942", next)
	}

	var enemyCriticalUpRole *CombatSkillRole
	for index := range catalog.SkillRoles[37901107] {
		candidate := &catalog.SkillRoles[37901107][index]
		if candidate.Function == "CRITICAL_UP" {
			enemyCriticalUpRole = candidate
			break
		}
	}
	var enemyCriticalAttackRole *CombatSkillRole
	for index := range catalog.SkillRoles[34601710] {
		candidate := &catalog.SkillRoles[34601710][index]
		if candidate.Function == "ATTACK_AA" {
			enemyCriticalAttackRole = candidate
			break
		}
	}
	if enemyCriticalUpRole == nil || enemyCriticalAttackRole == nil || enemyCriticalUpRole.Target != "SELECT" ||
		enemyCriticalUpRole.Parameters[0] != "2" || enemyCriticalUpRole.Parameters[1] != "100" ||
		enemyCriticalUpRole.Parameters[2] != "1" || enemyCriticalAttackRole.Parameters[0] != "2500" ||
		enemyCriticalAttackRole.Parameters[2] != "1000" || enemyCriticalAttackRole.Parameters[5] != "ATK" ||
		enemyCriticalAttackRole.Parameters[6] != "0" || enemyCriticalAttackRole.Parameters[7] != "DARK" {
		return fmt.Errorf("official enemy critical-up/attack rows changed: up=%+v attack=%+v", enemyCriticalUpRole, enemyCriticalAttackRole)
	}
	newEnemyCriticalUpEngine := func() *BattleEngine {
		engine := &BattleEngine{catalog: catalog, rng: newXorShift128(98), turn: 1, enemyCount: 1}
		engine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000}
		for index := 1; index < maxRoomMembers; index++ {
			engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1}
		}
		engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, Attack: 1000, HP: 10000, MaxHP: 10000}
		return engine
	}
	enemyCriticalUpEngine := newEnemyCriticalUpEngine()
	enemyCriticalUpApply, err := enemyCriticalUpEngine.executeEnemyRole(
		&enemyCriticalUpEngine.enemies[0], 5, *enemyCriticalUpRole, []CombatSkillRole{*enemyCriticalUpRole},
	)
	if err != nil {
		return err
	}
	if len(enemyCriticalUpApply) != 2 || enemyCriticalUpApply[0].Command != CommandBuff ||
		enemyCriticalUpApply[0].Args[0] != 5 || enemyCriticalUpApply[0].Args[3] != int64(battleBuffCodes["CRITICAL_UP"]) ||
		enemyCriticalUpApply[0].Args[7] != 0 || len(enemyCriticalUpEngine.enemies[0].Effects) != 1 ||
		enemyCriticalUpEngine.enemies[0].Effects[0].Rate != 101 || enemyCriticalUpEngine.enemies[0].Effects[0].Remaining != 2 ||
		enemyCriticalUpEngine.enemies[0].Effects[0].Source != 5 || enemyCriticalUpEngine.enemies[0].Effects[0].Kind != 1 ||
		enemyCriticalUpApply[1].Command != CommandBattleParam ||
		!equalBattleArgs(enemyCriticalUpApply[1].Args, battleParameterArgs(5, 10000, 10000, 1000, 0, 0, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("official enemy critical-up application is results=%+v enemy=%+v", enemyCriticalUpApply, enemyCriticalUpEngine.enemies[0])
	}
	enemyCriticalUpResults, err := enemyCriticalUpEngine.executeEnemyAttack(
		&enemyCriticalUpEngine.enemies[0], 1, *enemyCriticalAttackRole, []CombatSkillRole{*enemyCriticalAttackRole},
	)
	if err != nil {
		return err
	}
	if len(enemyCriticalUpResults) != 2 || enemyCriticalUpResults[0].Command != 60 ||
		enemyCriticalUpResults[0].Args[2] != -5250 || enemyCriticalUpResults[0].Args[7] != 1 ||
		enemyCriticalUpEngine.players[0].HP != 4750 || enemyCriticalUpResults[1].Command != CommandHP {
		return fmt.Errorf("official enemy critical-up attack is player=%+v results=%+v", enemyCriticalUpEngine.players[0], enemyCriticalUpResults)
	}
	enemyCriticalControlEngine := newEnemyCriticalUpEngine()
	enemyCriticalControlResults, err := enemyCriticalControlEngine.executeEnemyAttack(
		&enemyCriticalControlEngine.enemies[0], 1, *enemyCriticalAttackRole, []CombatSkillRole{*enemyCriticalAttackRole},
	)
	if err != nil {
		return err
	}
	if len(enemyCriticalControlResults) != 2 || enemyCriticalControlResults[0].Args[2] != -3500 ||
		enemyCriticalControlResults[0].Args[7] != 0 || enemyCriticalControlEngine.players[0].HP != 6500 {
		return fmt.Errorf("official enemy critical-up control is player=%+v results=%+v", enemyCriticalControlEngine.players[0], enemyCriticalControlResults)
	}

	// Native barrier consumer FUN_00047028 matches physics/attribute, consumes
	// one p3 use per matching hit, and absorbs only damage <= p1+p2*level.
	barrierRole := CombatSkillRole{Function: "ATTACK_BARRIER_APPOINT_ATTR"}
	barrierRole.Parameters[0] = "3"
	barrierRole.Parameters[1] = "1000"
	barrierRole.Parameters[2] = "10"
	barrierRole.Parameters[3] = "2"
	barrierRole.Parameters[4] = "PHYSICS"
	barrierRole.Parameters[5] = "DARK"
	barrier := persistentBattleEffect(barrierRole, persistentRoleValue(barrierRole, 60, operatorActor, 1), 3, 1, 1, 1, 60)
	if barrier.Value != 1600 || barrier.Uses != 2 || barrier.Remaining != 3 {
		return fmt.Errorf("barrier state is value=%d uses=%d duration=%d", barrier.Value, barrier.Uses, barrier.Remaining)
	}
	barrierEffects := []battleEffect{barrier}
	wrongAttribute := engine.resolveIncomingDamageEffects(&barrierEffects, 1200, 5000, "LIGHT", "PHYSICS")
	if wrongAttribute.Damage != 1200 || barrierEffects[0].Uses != 2 {
		return errors.New("appointed barrier consumed a non-matching attribute")
	}
	absorbed := engine.resolveIncomingDamageEffects(&barrierEffects, 1200, 5000, "DARK", "PHYSICS")
	if absorbed.Damage != 0 || !absorbed.BarrierTriggered || absorbed.BarrierRemainingUses != 1 || len(barrierEffects) != 1 || barrierEffects[0].Uses != 1 {
		return errors.New("appointed barrier did not absorb and consume one matching hit")
	}
	barrierRows := damageEffectResults(1, absorbed)
	if len(barrierRows) != 1 || barrierRows[0].Command != 200 || len(barrierRows[0].Args) != 4 || barrierRows[0].Args[2] != 1 || barrierRows[0].Args[3] != int64(combatAttributeCode("DARK")) {
		return errors.New("appointed barrier did not project native ResultCmd200 count/attribute state")
	}
	overThreshold := engine.resolveIncomingDamageEffects(&barrierEffects, 1700, 5000, "DARK", "PHYSICS")
	if overThreshold.Damage != 1700 || !overThreshold.BarrierTriggered || overThreshold.BarrierRemainingUses != 0 || len(barrierEffects) != 0 || len(overThreshold.Released) != 1 {
		return errors.New("appointed barrier threshold/use release contract is wrong")
	}
	barrierRows = damageEffectResults(1, overThreshold)
	if len(barrierRows) != 2 || barrierRows[0].Command != 200 || barrierRows[0].Args[2] != 0 || barrierRows[1].Command != 72 {
		return errors.New("depleted barrier did not project count before release")
	}

	// 838d0 retains the original damage and adds scaled nonlethal reflection;
	// matching a hit does not decrement the retained status duration.
	reflectionRole := CombatSkillRole{Function: "REFLECTION"}
	reflectionRole.Parameters[0] = "3"
	reflectionRole.Parameters[1] = "500"
	reflectionRole.Parameters[2] = "0"
	reflectionRole.Parameters[3] = "MAGIC"
	reflection := persistentBattleEffect(reflectionRole, persistentRoleValue(reflectionRole, 60, operatorActor, 1), 3, 1, 1, 1, 60)
	reflectionEffects := []battleEffect{reflection}
	physical := engine.resolveIncomingDamageEffects(&reflectionEffects, 2000, 5000, "FIRE", "PHYSICS")
	if physical.Damage != 2000 || physical.Reflected != 0 || reflectionEffects[0].Remaining != 3 {
		return errors.New("magic reflection consumed a physical hit")
	}
	magic := engine.resolveIncomingDamageEffects(&reflectionEffects, 2000, 5000, "FIRE", "MAGIC")
	if magic.Damage != 2000 || magic.Reflected != 100 || reflectionEffects[0].Remaining != 3 {
		return fmt.Errorf("reflection resolution is damage=%d reflected=%d duration=%d", magic.Damage, magic.Reflected, reflectionEffects[0].Remaining)
	}
	if damage := registeredDOTValue(1000, 100, 25); damage != 750 {
		return fmt.Errorf("native DOT reduction is %d, want 750", damage)
	}
	if damage := registeredDOTValue(1000, 100, -50); damage != 1500 {
		return fmt.Errorf("native DOT vulnerability is %d, want 1500", damage)
	}
	if damage := registeredDOTValue(1, 100, 100); damage != 1 {
		return fmt.Errorf("native DOT minimum is %d, want 1", damage)
	}
	// a077a's preview contains unchained fixed+source. Actual 8bb40
	// registration applies attribute, whole-value Chain, then DOT reduction.
	playerDOT := CombatSkillRole{Function: "BURN", ChainRate: 20}
	playerDOT.Parameters[3] = "299"
	playerDOT.Parameters[4] = "10370"
	playerDOT.Parameters[5] = "389"
	playerDOT.Parameters[6] = "2"
	playerDOT.Parameters[7] = "INT"
	if value := persistentRoleValue(playerDOT, 60, operatorActor, 2); value != 1939 {
		return fmt.Errorf("native player DOT producer value is %d, want 1939", value)
	}
	officialBurn, err := findPlayerRole(11102292, "BURN")
	if err != nil {
		return err
	}
	applicationEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1, rng: newXorShift128(1)}
	applicationEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 50000, MaxHP: 50000, Magic: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	dotLevel := CombatEnemyLevel{}
	dotLevel.AttributeRates[combatAttributeIndex("FIRE")] = 150
	dotLevel.DOTReductions[combatDOTIndex("BURN")] = 25
	applicationEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 50000, MaxHP: 50000, Level: dotLevel}
	applicationResults, err := applicationEngine.executePlayerRole(battleAction{memberType: 1, target: 5, cardLevel: 60}, *officialBurn, 4)
	if err != nil {
		return err
	}
	if len(applicationResults) != 2 || len(applicationEngine.enemies[0].Effects) != 1 ||
		applicationResults[1].Command != CommandBattleParam ||
		!equalBattleArgs(applicationResults[1].Args, battleParameterArgs(5, 50000, 50000, 0, 0, 0, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("official BURN application is results=%+v effects=%+v", applicationResults, applicationEngine.enemies[0].Effects)
	}
	storedBurn := applicationEngine.enemies[0].Effects[0]
	if storedBurn.Value != 8658 || storedBurn.Remaining != 3 || storedBurn.Rate != 100 ||
		storedBurn.Parameters != [4]int{100, 921, 38, 0} ||
		len(applicationResults[0].Args) != 11 || applicationResults[0].Args[7] != 0 || applicationResults[0].Args[8] != 0 || applicationResults[0].Args[9] != 0 {
		return fmt.Errorf("official BURN target state/result is effect=%+v result=%+v", storedBurn, applicationResults[0])
	}
	rngBeforeDuplicate := applicationEngine.rng
	duplicateResults, err := applicationEngine.executePlayerRole(battleAction{memberType: 1, target: 5, cardLevel: 60}, *officialBurn, 4)
	if err != nil {
		return err
	}
	if len(duplicateResults) != 1 || duplicateResults[0].Command != CommandDebuffFailed || len(duplicateResults[0].Args) != 4 ||
		duplicateResults[0].Args[0] != 5 || duplicateResults[0].Args[1] != int64(officialBurn.RoleIndex) ||
		duplicateResults[0].Args[2] != int64(battleBuffCodes["BURN"]) || duplicateResults[0].Args[3] != 5 ||
		len(applicationEngine.enemies[0].Effects) != 1 || applicationEngine.rng != rngBeforeDuplicate {
		return fmt.Errorf("native duplicate DOT failure projection/state is results=%+v effects=%+v", duplicateResults, applicationEngine.enemies[0].Effects)
	}
	partialRole := *officialBurn
	partialRole.Target = "ENEMY_ALL"
	partialEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 2, rng: newXorShift128(1)}
	partialEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 50000, MaxHP: 50000, Magic: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	partialEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 50000, MaxHP: 50000, Effects: []battleEffect{{Function: "BURN", Kind: 2, Remaining: 2}}}
	partialEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 50000, MaxHP: 50000}
	partialResults, err := partialEngine.executePlayerRole(battleAction{memberType: 1, cardLevel: 60}, partialRole, 1)
	if err != nil {
		return err
	}
	if len(partialResults) != 2 || partialResults[0].Command != CommandBuff || partialResults[0].Args[0] != 6 ||
		len(partialEngine.enemies[0].Effects) != 1 || len(partialEngine.enemies[1].Effects) != 1 ||
		partialResults[1].Command != CommandBattleParam ||
		!equalBattleArgs(partialResults[1].Args, battleParameterArgs(6, 50000, 50000, 0, 0, 0, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("native per-role partial DOT projection is results=%+v enemies=%+v", partialResults, partialEngine.enemies[:2])
	}
	resistEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1, rng: newXorShift128(1)}
	resistEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 50000, MaxHP: 50000, Magic: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	resistLevel := CombatEnemyLevel{}
	resistLevel.StatusResistances[combatDOTStatusResistanceIndex("BURN")] = 100
	resistEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 50000, MaxHP: 50000, Level: resistLevel}
	resistedResults, err := resistEngine.executePlayerRole(battleAction{memberType: 1, target: 5, cardLevel: 60}, *officialBurn, 1)
	if err != nil {
		return err
	}
	if len(resistedResults) != 1 || resistedResults[0].Command != CommandDebuffFailed || resistedResults[0].Args[0] != 5 ||
		resistedResults[0].Args[1] != int64(officialBurn.RoleIndex) || resistedResults[0].Args[2] != int64(battleBuffCodes["BURN"]) ||
		len(resistEngine.enemies[0].Effects) != 0 {
		return fmt.Errorf("official enemy inherent BURN resistance projection/state is results=%+v effects=%+v", resistedResults, resistEngine.enemies[0].Effects)
	}
	// Close all five active DOT identities on both sides with official roles.
	// They share one producer, but their status-resistance index, damage
	// attribute, source member and target side are observable independently.
	type officialDOTVector struct {
		function       string
		attribute      string
		playerSkillID  int
		playerValue    int
		playerDuration int
		playerParams   [4]int
		enemySkillID   int
		enemyValue     int
		enemyDuration  int
		enemyParams    [4]int
	}
	officialDOTVectors := []officialDOTVector{
		{function: "POISON", attribute: "DARK", playerSkillID: 11103692, playerValue: 7697, playerDuration: 3, playerParams: [4]int{100, 921, 38, 0}, enemySkillID: 31901118, enemyValue: 500, enemyDuration: 6, enemyParams: [4]int{100, 500, 0, 0}},
		{function: "BURN", attribute: "FIRE", playerSkillID: 11102292, playerValue: 7697, playerDuration: 3, playerParams: [4]int{100, 921, 38, 0}, enemySkillID: 34201105, enemyValue: 1000, enemyDuration: 99, enemyParams: [4]int{100, 1000, 0, 0}},
		{function: "FREEZE", attribute: "ICE", playerSkillID: 11102872, playerValue: 9854, playerDuration: 3, playerParams: [4]int{100, 1159, 50, 0}, enemySkillID: 35601116, enemyValue: 2000, enemyDuration: 5, enemyParams: [4]int{100, 2000, 0, 0}},
		{function: "BLEED", attribute: "WIND", playerSkillID: 11102974, playerValue: 10795, playerDuration: 3, playerParams: [4]int{100, 1747, 50, 0}, enemySkillID: 35601117, enemyValue: 2000, enemyDuration: 5, enemyParams: [4]int{100, 2000, 0, 0}},
		{function: "ELECTRIC", attribute: "LIGHT", playerSkillID: 11104492, playerValue: 7697, playerDuration: 3, playerParams: [4]int{100, 921, 38, 0}, enemySkillID: 35601118, enemyValue: 2000, enemyDuration: 5, enemyParams: [4]int{100, 2000, 0, 0}},
	}
	findEnemyDOTRole := func(skillID int, function string) (*CombatSkillRole, error) {
		for index := range catalog.SkillRoles[skillID] {
			role := &catalog.SkillRoles[skillID][index]
			if role.Function == function {
				return role, nil
			}
		}
		return nil, fmt.Errorf("official enemy DOT role %s is missing from skill %d", function, skillID)
	}
	for _, vector := range officialDOTVectors {
		if combatDOTAttribute(vector.function) != vector.attribute {
			return fmt.Errorf("native %s DOT attribute mapping is %s, want %s", vector.function, combatDOTAttribute(vector.function), vector.attribute)
		}
		playerRole, findErr := findPlayerRole(vector.playerSkillID, vector.function)
		if findErr != nil {
			return findErr
		}
		if playerRole.Target != "SELECT" || combatParameterInt(playerRole.Parameters[0]) != vector.playerDuration ||
			combatParameterInt(playerRole.Parameters[1]) != 100 {
			return fmt.Errorf("official player %s DOT role changed: %+v", vector.function, *playerRole)
		}
		playerDOTEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1, rng: newXorShift128(1)}
		playerDOTEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 50000, MaxHP: 50000, Attack: 10000, Magic: 10000, Recovery: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		playerDOTLevel := CombatEnemyLevel{}
		playerDOTLevel.AttributeRates[combatAttributeIndex(vector.attribute)] = 100
		playerDOTEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 50000, MaxHP: 50000, Level: playerDOTLevel}
		playerDOTResults, executeErr := playerDOTEngine.executePlayerRole(battleAction{memberType: 1, target: 5, cardLevel: 60}, *playerRole, 4)
		if executeErr != nil {
			return executeErr
		}
		if len(playerDOTResults) != 2 || playerDOTResults[0].Command != CommandBuff || len(playerDOTEngine.enemies[0].Effects) != 1 ||
			playerDOTResults[1].Command != CommandBattleParam ||
			!equalBattleArgs(playerDOTResults[1].Args, battleParameterArgs(5, 50000, 50000, 0, 0, 0, 0, 0, 0, 0, 0)) {
			return fmt.Errorf("official player %s DOT application is results=%+v effects=%+v", vector.function, playerDOTResults, playerDOTEngine.enemies[0].Effects)
		}
		playerEffect := playerDOTEngine.enemies[0].Effects[0]
		if playerEffect.Function != vector.function || playerEffect.Value != vector.playerValue || playerEffect.Remaining != vector.playerDuration ||
			playerEffect.Kind != 2 || playerEffect.Source != 1 || playerEffect.Parameters != vector.playerParams {
			return fmt.Errorf("official player %s DOT state is %+v, want value=%d duration=%d params=%v", vector.function, playerEffect, vector.playerValue, vector.playerDuration, vector.playerParams)
		}
		playerTickResults, tickErr := playerDOTEngine.tickEnemyDOTEffects()
		if tickErr != nil {
			return tickErr
		}
		playerDOTEngine.turn = 2
		if lifecycle, lifecycleErr := expireForEffectContract(playerDOTEngine); lifecycleErr != nil || len(lifecycle) != 0 {
			return fmt.Errorf("official player %s DOT next-turn lifecycle is results=%+v err=%v", vector.function, lifecycle, lifecycleErr)
		}
		playerTickDamage := false
		for _, result := range playerTickResults {
			if result.Command == 60 && len(result.Args) >= 10 && result.Args[0] == 5 && result.Args[2] == int64(-vector.playerValue) &&
				result.Args[3] == int64(50000-vector.playerValue) && result.Args[9] == 0 {
				playerTickDamage = true
			}
		}
		if !playerTickDamage || playerDOTEngine.enemies[0].HP != 50000-vector.playerValue ||
			len(playerDOTEngine.enemies[0].Effects) != 1 || playerDOTEngine.enemies[0].Effects[0].Remaining != vector.playerDuration-1 {
			return fmt.Errorf("official player %s next DOT tick is results=%+v enemy=%+v", vector.function, playerTickResults, playerDOTEngine.enemies[0])
		}
		playerResistEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1, rng: newXorShift128(1)}
		playerResistEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 50000, MaxHP: 50000, Attack: 10000, Magic: 10000, Recovery: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		resistLevel := CombatEnemyLevel{}
		resistLevel.StatusResistances[combatDOTStatusResistanceIndex(vector.function)] = 100
		playerResistEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 50000, MaxHP: 50000, Level: resistLevel}
		playerResistResults, resistErr := playerResistEngine.executePlayerRole(battleAction{memberType: 1, target: 5, cardLevel: 60}, *playerRole, 4)
		if resistErr != nil {
			return resistErr
		}
		if len(playerResistResults) != 1 || playerResistResults[0].Command != CommandDebuffFailed || len(playerResistEngine.enemies[0].Effects) != 0 {
			return fmt.Errorf("official player %s inherent-resistance path is results=%+v effects=%+v", vector.function, playerResistResults, playerResistEngine.enemies[0].Effects)
		}

		enemyRole, findErr := findEnemyDOTRole(vector.enemySkillID, vector.function)
		if findErr != nil {
			return findErr
		}
		if enemyRole.Target != "SELECT" || combatParameterInt(enemyRole.Parameters[0]) != vector.enemyDuration ||
			combatParameterInt(enemyRole.Parameters[1]) != 100 || enemyPersistentRoleValue(*enemyRole, 0, &battleEnemy{}) != vector.enemyValue {
			return fmt.Errorf("official enemy %s DOT role changed: %+v", vector.function, *enemyRole)
		}
		enemyDOTEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1, rng: newXorShift128(1)}
		enemyDOTEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 50000, MaxHP: 50000}
		enemyDOTEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 50000, MaxHP: 50000}
		enemyDOTResults, executeErr := enemyDOTEngine.executeEnemyPersistentEffect(&enemyDOTEngine.enemies[0], 1, *enemyRole)
		if executeErr != nil {
			return executeErr
		}
		if len(enemyDOTResults) != 1 || enemyDOTResults[0].Command != CommandBuff || len(enemyDOTEngine.players[0].Effects) != 1 {
			return fmt.Errorf("official enemy %s DOT application is results=%+v effects=%+v", vector.function, enemyDOTResults, enemyDOTEngine.players[0].Effects)
		}
		enemyEffect := enemyDOTEngine.players[0].Effects[0]
		if enemyEffect.Function != vector.function || enemyEffect.Value != vector.enemyValue || enemyEffect.Remaining != vector.enemyDuration ||
			enemyEffect.Kind != 2 || enemyEffect.Source != 5 || enemyEffect.Parameters != vector.enemyParams {
			return fmt.Errorf("official enemy %s DOT state is %+v, want value=%d duration=%d params=%v", vector.function, enemyEffect, vector.enemyValue, vector.enemyDuration, vector.enemyParams)
		}
		enemyTickResults := enemyDOTEngine.tickPlayerDOTEffects()
		enemyDOTEngine.turn = 2
		if lifecycle, lifecycleErr := expireForEffectContract(enemyDOTEngine); lifecycleErr != nil || len(lifecycle) != 0 {
			return fmt.Errorf("official enemy %s DOT next-turn lifecycle is results=%+v err=%v", vector.function, lifecycle, lifecycleErr)
		}
		enemyTickDamage := false
		for _, result := range enemyTickResults {
			if result.Command == 60 && len(result.Args) >= 10 && result.Args[0] == 1 && result.Args[2] == int64(-vector.enemyValue) &&
				result.Args[3] == int64(50000-vector.enemyValue) && result.Args[9] == 0 {
				enemyTickDamage = true
			}
		}
		if !enemyTickDamage || enemyDOTEngine.players[0].HP != 50000-vector.enemyValue ||
			len(enemyDOTEngine.players[0].Effects) != 1 || enemyDOTEngine.players[0].Effects[0].Remaining != vector.enemyDuration-1 {
			return fmt.Errorf("official enemy %s next DOT tick is results=%+v player=%+v", vector.function, enemyTickResults, enemyDOTEngine.players[0])
		}
	}

	// DEBUFF_REGIST is a typed resistance on the target and must be checked
	// against the official enemy ELECTRIC role before any DOT is appended.
	officialEnemyElectric, err := findEnemyDOTRole(35601118, "ELECTRIC")
	if err != nil {
		return err
	}
	enemyDotEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1, rng: newXorShift128(1)}
	enemyDotEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 1, HP: 5000, MaxHP: 5000,
		Effects: []battleEffect{{Function: "DEBUFF_REGIST", Parameter: "ELECTRIC", Value: 100, Remaining: 2}},
	}
	enemyDotEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 5000, MaxHP: 5000}
	enemyDotResults, err := enemyDotEngine.executeEnemyPersistentEffect(&enemyDotEngine.enemies[0], 1, *officialEnemyElectric)
	if err != nil {
		return err
	}
	if len(enemyDotResults) != 1 || enemyDotResults[0].Command != CommandDebuffFailed || len(enemyDotResults[0].Args) != 4 ||
		enemyDotResults[0].Args[0] != 1 || enemyDotResults[0].Args[1] != int64(officialEnemyElectric.RoleIndex) ||
		enemyDotResults[0].Args[2] != int64(battleBuffCodes["ELECTRIC"]) || enemyDotResults[0].Args[3] != 5 ||
		len(enemyDotEngine.players[0].Effects) != 1 {
		return fmt.Errorf("player DEBUFF_REGIST official enemy ELECTRIC projection/state is results=%+v effects=%+v", enemyDotResults, enemyDotEngine.players[0].Effects)
	}

	// Native role 79 (FUN_0009835e) changes an existing DOT instead of adding
	// a standalone buff. p0 extends its duration; p1+p2*level and
	// p3+p4*level/1000 is a fixed addition after percentage scaling.
	// ResultCmd201 carries three positive-change flags (86210 / D-359).
	dotRole, err := findPlayerRole(13602781, "DOT_VALUE_UP")
	if err != nil {
		return err
	}
	dotEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	dotEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 100, MaxHP: 100}
	dotEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 5000, MaxHP: 5000, Effects: []battleEffect{{Function: "FREEZE", Value: 1000, Remaining: 2, Kind: 2}}}
	dotResults, err := dotEngine.executePlayerRole(battleAction{memberType: 1, cardLevel: 60, target: 5}, *dotRole, 1)
	if err != nil {
		return err
	}
	if len(dotResults) != 1 || dotResults[0].Command != 201 || len(dotResults[0].Args) != 7 {
		return fmt.Errorf("DOT value-up projection is %+v", dotResults)
	}
	freeze := dotEngine.enemies[0].Effects[0]
	if freeze.Value != 1100 || freeze.Remaining != 3 || dotResults[0].Args[2] != 0 || dotResults[0].Args[3] != 1<<9 || dotResults[0].Args[4] != 1 || dotResults[0].Args[5] != 1 || dotResults[0].Args[6] != 0 {
		return fmt.Errorf("freeze value-up state=%+v result=%+v", freeze, dotResults[0])
	}
	dotEngine.enemies[0].Effects = nil
	dotResults, err = dotEngine.executePlayerRole(battleAction{memberType: 1, cardLevel: 60, target: 5}, *dotRole, 1)
	if err != nil {
		return err
	}
	if len(dotResults) != 1 || dotResults[0].Command != 202 || len(dotResults[0].Args) != 2 {
		return fmt.Errorf("missing DOT did not project ResultCmd202: %+v", dotResults)
	}
	// Upgrade the synthetic enemy-side DOT_VALUE_UP probe to official target
	// objects. 35701102 applies FRIEND_ALL BURN +44% without a turn extension;
	// the following turn proves that the mutated retained values, not the
	// ResultCmd201 display fields, drive all four DOT damage commits.
	var enemyAllDOTRole *CombatSkillRole
	for index := range catalog.SkillRoles[35701102] {
		candidate := &catalog.SkillRoles[35701102][index]
		if candidate.Function == "DOT_VALUE_UP" {
			enemyAllDOTRole = candidate
			break
		}
	}
	if enemyAllDOTRole == nil || enemyAllDOTRole.Target != "FRIEND_ALL" ||
		enemyAllDOTRole.Parameters[0] != "0" || enemyAllDOTRole.Parameters[1] != "44" ||
		enemyAllDOTRole.Parameters[5] != "BURN" {
		return fmt.Errorf("official enemy FRIEND_ALL DOT_VALUE_UP changed: %+v", enemyAllDOTRole)
	}
	dotEngine = &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	dotEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	for index := range dotEngine.players {
		baseValue := 1000 * (index + 1)
		dotEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
			MemberType: index + 1,
			HP:         10000,
			MaxHP:      10000,
			Effects: []battleEffect{{
				Function: "BURN", Value: baseValue, Remaining: 2, AppliedTurn: 1,
				Kind: 2, Source: 5, RoleIndex: 4,
			}},
		}
	}
	dotResults, err = dotEngine.executeEnemyRole(&dotEngine.enemies[0], 5, *enemyAllDOTRole, nil)
	if err != nil {
		return err
	}
	if len(dotResults) != maxRoomMembers {
		return fmt.Errorf("official enemy FRIEND_ALL DOT_VALUE_UP projected %d rows, want %d: %+v", len(dotResults), maxRoomMembers, dotResults)
	}
	for index := range dotEngine.players {
		wantValue := 1440 * (index + 1)
		if dotEngine.players[index].Effects[0].Value != wantValue || dotEngine.players[index].Effects[0].Remaining != 2 ||
			dotResults[index].Command != 201 || len(dotResults[index].Args) != 7 || dotResults[index].Args[0] != int64(index+1) ||
			dotResults[index].Args[4] != 0 || dotResults[index].Args[5] != 1 || dotResults[index].Args[6] != 0 {
			return fmt.Errorf("official enemy FRIEND_ALL DOT_VALUE_UP member %d is player=%+v result=%+v", index+1, dotEngine.players[index], dotResults[index])
		}
	}
	dotTickResults := dotEngine.tickPlayerDOTEffects()
	dotEngine.turn = 2
	if lifecycle, lifecycleErr := expireForEffectContract(dotEngine); lifecycleErr != nil || len(lifecycle) != 0 {
		return fmt.Errorf("official enemy FRIEND_ALL DOT next-turn lifecycle is results=%+v err=%v", lifecycle, lifecycleErr)
	}
	dotTickDamage := [maxRoomMembers]int{}
	for _, result := range dotTickResults {
		if result.Command == 60 && len(result.Args) >= 10 && result.Args[0] >= 1 && result.Args[0] <= maxRoomMembers {
			dotTickDamage[result.Args[0]-1] = int(-result.Args[2])
			if result.Args[9] != 0 {
				return fmt.Errorf("official enemy FRIEND_ALL DOT tick reserved source is %+v", result)
			}
		}
	}
	for index := range dotEngine.players {
		wantDamage := 1440 * (index + 1)
		if dotTickDamage[index] != wantDamage || dotEngine.players[index].HP != 10000-wantDamage ||
			len(dotEngine.players[index].Effects) != 1 || dotEngine.players[index].Effects[0].Remaining != 1 {
			return fmt.Errorf("official enemy FRIEND_ALL DOT tick member %d damage=%d player=%+v", index+1, dotTickDamage[index], dotEngine.players[index])
		}
	}

	// 34901160 covers the other active enemy target form: SELECT POISON gets a
	// +200% value delta and one retained turn. Only the concrete target changes.
	var enemySelectDOTRole *CombatSkillRole
	for index := range catalog.SkillRoles[34901160] {
		candidate := &catalog.SkillRoles[34901160][index]
		if candidate.Function == "DOT_VALUE_UP" {
			enemySelectDOTRole = candidate
			break
		}
	}
	if enemySelectDOTRole == nil || enemySelectDOTRole.Target != "SELECT" ||
		enemySelectDOTRole.Parameters[0] != "1" || enemySelectDOTRole.Parameters[1] != "200" ||
		enemySelectDOTRole.Parameters[5] != "POISON" {
		return fmt.Errorf("official enemy SELECT DOT_VALUE_UP changed: %+v", enemySelectDOTRole)
	}
	selectDOTEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	selectDOTEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	for index := range selectDOTEngine.players {
		selectDOTEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	selectDOTEngine.players[1].Effects = []battleEffect{{Function: "POISON", Value: 1000, Remaining: 2, AppliedTurn: 1, Kind: 2, Source: 5}}
	selectDOTResults, err := selectDOTEngine.executeEnemyRole(&selectDOTEngine.enemies[0], 2, *enemySelectDOTRole, nil)
	if err != nil {
		return err
	}
	if len(selectDOTResults) != 1 || selectDOTResults[0].Command != 201 || selectDOTResults[0].Args[0] != 2 ||
		selectDOTResults[0].Args[4] != 1 || selectDOTResults[0].Args[5] != 1 ||
		selectDOTEngine.players[1].Effects[0].Value != 3000 || selectDOTEngine.players[1].Effects[0].Remaining != 3 ||
		len(selectDOTEngine.players[0].Effects) != 0 {
		return fmt.Errorf("official enemy SELECT DOT_VALUE_UP is results=%+v players=%+v", selectDOTResults, selectDOTEngine.players)
	}

	// D-377: native 57530 -> 4ce6c keeps an empty AI target at zero,
	// independently of the outer skill. Explicit low-stat enemy selectors
	// resolve a living enemy member before SELECT roles execute.
	targetEngine := &BattleEngine{catalog: catalog, enemyCount: 2, rng: newXorShift128(1)}
	targetEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 8000, MaxHP: 10000, Attack: 2000}
	targetEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 6000, MaxHP: 9000, Attack: 1000}
	nullTarget, ok := targetEngine.selectEnemyActionTarget(&targetEngine.enemies[0], CombatEnemyAction{SkillID: 44047002})
	if !ok || nullTarget != 0 {
		return fmt.Errorf("official NULL AI target is %d ok=%t, want zero", nullTarget, ok)
	}
	lowAttackTarget, ok := targetEngine.selectEnemyActionTarget(&targetEngine.enemies[0], CombatEnemyAction{Target: "LOW_ATK_ENEMY"})
	if !ok || lowAttackTarget != 6 {
		return fmt.Errorf("LOW_ATK_ENEMY target is %d ok=%t, want member 6", lowAttackTarget, ok)
	}
	lowMaxHPTarget, ok := targetEngine.selectEnemyActionTarget(&targetEngine.enemies[0], CombatEnemyAction{Target: "LOW_MAX_HP_ENEMY"})
	if !ok || lowMaxHPTarget != 6 {
		return fmt.Errorf("LOW_MAX_HP_ENEMY target is %d ok=%t, want member 6", lowMaxHPTarget, ok)
	}
	lowHPTarget, ok := targetEngine.selectEnemyActionTarget(&targetEngine.enemies[0], CombatEnemyAction{Target: "LOW_HP_ENEMY"})
	if !ok || lowHPTarget != 6 {
		return fmt.Errorf("LOW_HP_ENEMY target is %d ok=%t, want member 6", lowHPTarget, ok)
	}
	for index := range targetEngine.players {
		targetEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 1000 + index*1000, MaxHP: 10000}
	}
	highHPUser, ok := targetEngine.selectEnemyActionTarget(&targetEngine.enemies[0], CombatEnemyAction{Target: "HIGH_HP_USER"})
	if !ok || highHPUser != 4 {
		return fmt.Errorf("HIGH_HP_USER target is %d ok=%t, want member 4", highHPUser, ok)
	}
	bullyTarget, ok := targetEngine.selectEnemyActionTarget(&targetEngine.enemies[0], CombatEnemyAction{Target: "BULLY"})
	if !ok || bullyTarget != 1 {
		return fmt.Errorf("native BULLY/LOW_HP_USER target is %d ok=%t, want member 1", bullyTarget, ok)
	}
	targetEngine.players[2].Effects = []battleEffect{{Function: "WEAKNESS", Kind: 2, Remaining: 2}}
	weaknessTarget, ok := targetEngine.selectEnemyActionTarget(&targetEngine.enemies[0], CombatEnemyAction{Target: "WEAKNESS_USER"})
	if !ok || weaknessTarget != 3 {
		return fmt.Errorf("native WEAKNESS_USER target is %d ok=%t, want member 3", weaknessTarget, ok)
	}
	targetEngine.players[1].Effects = []battleEffect{{Function: "DARKNESS", Kind: 2, Remaining: 2}}
	debuffAction := CombatEnemyAction{Target: "USER_DEBUFF"}
	debuffAction.TargetParams[0] = "DARKNESS"
	debuffTarget, ok := targetEngine.selectEnemyActionTarget(&targetEngine.enemies[0], debuffAction)
	if !ok || debuffTarget != 2 {
		return fmt.Errorf("native USER_DEBUFF target is %d ok=%t, want member 2", debuffTarget, ok)
	}
	targetEngine.players[0].HateHistory[0] = 1000
	targetEngine.players[0].HateHistory[1] = 1000
	targetEngine.players[1].HateHistory[0] = 1800
	hate1, ok := targetEngine.playerHateRankTarget(1)
	if !ok || hate1 != 1 || targetEngine.playerHate(1) != 1900 || targetEngine.playerHate(2) != 1800 {
		return fmt.Errorf("native weighted hate rank is target=%d ok=%t values=%d/%d", hate1, ok, targetEngine.playerHate(1), targetEngine.playerHate(2))
	}
	targetEngine.addPlayerRoleHate(2, 10000, 100, 700)
	if targetEngine.players[1].HateHistory[0] != 2500 {
		return fmt.Errorf("native role hate cap produced %d, want 2500", targetEngine.players[1].HateHistory[0])
	}
	hate1, ok = targetEngine.playerHateRankTarget(1)
	if !ok || hate1 != 2 {
		return fmt.Errorf("native role hate did not change HATE1 target: target=%d ok=%t", hate1, ok)
	}
	for draw := 0; draw < 8; draw++ {
		notHate1, selected := targetEngine.selectEnemyActionTarget(&targetEngine.enemies[0], CombatEnemyAction{Target: "RANDOM_EXCEPT_HATE1"})
		if !selected || notHate1 == 2 {
			return fmt.Errorf("native RANDOM_EXCEPT_HATE1 selected target=%d ok=%t", notHate1, selected)
		}
	}
	deathFlagRole := CombatSkillRole{Function: "ENEMY_AI_TRIGGER_FLAG_SET", Target: "SELF"}
	deathFlagRole.Parameters[0] = "3"
	deathFlagRole.Parameters[1] = "1"
	// Native role 45 (FUN_000979cf) is an intentional no-effect producer: it
	// clears the role result payload and returns success. The enemy scheduler
	// still spends its action budget/use count; the first-role NONE suppresses
	// ResultCmd50. The empty action phase emits WAIT_AND_SEE and both59 footers.

	noneSkill, noneRoles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(30701114)
	if !ok || noneSkill.FunctionID != 30701114 || noneSkill.Target != "SELF" || len(noneRoles) != 1 ||
		noneRoles[0].Function != "NONE" || noneRoles[0].Target != "SELECT" {
		return fmt.Errorf("official enemy feint/NONE contract changed: skill=%+v roles=%+v ok=%t", noneSkill, noneRoles, ok)
	}
	noneEngine := &BattleEngine{
		catalog: catalog, rng: newXorShift128(1), enemyUses: make(map[int]int), enemyCount: 1, phase: PhaseUserAttack,
	}
	for index := range noneEngine.players {
		noneEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 1000, MaxHP: 1000}
	}
	noneAction := CombatEnemyAction{Slot: 1, Category: "skill", SkillID: 30701114, Target: "SELF", Priority: 1, ActionCost: 1, MaxUses: 1, Rate: 100}
	noneEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000, Level: CombatEnemyLevel{ActionsPerTurn: 1, Actions: []CombatEnemyAction{noneAction}}}
	noneResults, err := noneEngine.EnemyPhase()
	if err != nil {
		return fmt.Errorf("official enemy feint/NONE phase failed: %w", err)
	}
	// 7adb0 skips a first-role NONE; 64670 emits 7 when no skill row was produced.
	if len(noneResults) != 3 || noneResults[0].Command != CommandWaitAndSee || len(noneResults[0].Args) != 0 ||
		noneResults[1].Command != 59 || len(noneResults[1].Args) != 0 || noneResults[2].Command != 59 || len(noneResults[2].Args) != 0 {
		return fmt.Errorf("official enemy feint/NONE projection is %+v", noneResults)
	}
	if noneEngine.enemies[0].ActionConsumed != 1 || noneEngine.enemyUses[enemyActionUseKey(&noneEngine.enemies[0], noneAction)] != 1 ||
		noneEngine.phase != PhaseEnemy {
		return fmt.Errorf("official enemy feint/NONE scheduler state is consumed=%d uses=%v phase=%d results=%+v",
			noneEngine.enemies[0].ActionConsumed, noneEngine.enemyUses, noneEngine.phase, noneResults)
	}
	// Native role 51 is the other successful no-effect producer. Its concrete
	// text control retains the outer authoritative action: it spends the fixed
	// slot budget, selects the action target and emits ResultCmd50 while the role
	// itself emits no effect row.

	outputTextSkill, outputTextRoles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(44071001)
	if !ok || outputTextSkill.FunctionID != 44071001 || outputTextSkill.Target != "SELF" || len(outputTextRoles) != 1 ||
		outputTextRoles[0].Function != "OUTPUT_TEXT" || outputTextRoles[0].Target != "SELECT" {
		return fmt.Errorf("official enemy OUTPUT_TEXT contract changed: skill=%+v roles=%+v ok=%t", outputTextSkill, outputTextRoles, ok)
	}
	outputTextLevel, ok := catalog.EnemyLevels[40006912]
	if !ok || outputTextLevel.ActionsPerTurn != 2 {
		return fmt.Errorf("official enemy OUTPUT_TEXT level 40006912 is %+v", outputTextLevel)
	}
	outputTextAction := CombatEnemyAction{}
	for _, action := range outputTextLevel.Actions {
		if action.SkillID == 44071001 {
			outputTextAction = action
			break
		}
	}
	if outputTextAction.Slot != 2 || outputTextAction.Category != "skill" || outputTextAction.AIConditionID != 40072001 ||
		outputTextAction.Priority != 2 || outputTextAction.Target != "RANDOM" || outputTextAction.ActionCost != 1 ||
		outputTextAction.MaxUses != 999 || outputTextAction.Rate != 100 || outputTextAction.CountOnMiss {
		return fmt.Errorf("official enemy OUTPUT_TEXT action changed: %+v", outputTextAction)
	}
	outputTextLevel.ActionsPerTurn = 1
	outputTextLevel.Actions = []CombatEnemyAction{outputTextAction}
	outputTextEngine := &BattleEngine{
		// Official40072001 enables turn2, not blank turn0/turn1 columns.
		catalog: catalog, turn: 2, rng: newXorShift128(1), enemyUses: make(map[int]int), enemyCount: 1, phase: PhaseUserAttack,
	}
	for index := range outputTextEngine.players {
		outputTextEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 1000, MaxHP: 1000}
	}
	outputTextEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000, Level: outputTextLevel}
	outputTextResults, err := outputTextEngine.EnemyPhase()
	if err != nil {
		return fmt.Errorf("official enemy OUTPUT_TEXT phase failed: %w", err)
	}
	wantOutputTextArgs := []int64{5, 44071001, 3, 1, 0, 44071001, 0}
	if len(outputTextResults) != 3 || outputTextResults[0].Command != CommandSkill ||
		!equalBattleArgs(outputTextResults[0].Args, wantOutputTextArgs) ||
		outputTextResults[1].Command != 59 || len(outputTextResults[1].Args) != 0 || outputTextResults[2].Command != 59 || len(outputTextResults[2].Args) != 0 {
		return fmt.Errorf("official enemy OUTPUT_TEXT projection is %+v, want %v", outputTextResults, wantOutputTextArgs)
	}
	if outputTextEngine.enemies[0].ActionConsumed != 1 ||
		outputTextEngine.enemyUses[enemyActionUseKey(&outputTextEngine.enemies[0], outputTextAction)] != 1 ||
		battleResultsContainCommand(outputTextResults, CommandWaitAndSee) || outputTextEngine.phase != PhaseEnemy {
		return fmt.Errorf("official enemy OUTPUT_TEXT scheduler state is consumed=%d uses=%v phase=%d results=%+v",
			outputTextEngine.enemies[0].ActionConsumed, outputTextEngine.enemyUses, outputTextEngine.phase, outputTextResults)
	}
	outputTextControl := newXorShift128(1)
	outputTextControl.next() // exec_rate
	outputTextControl.next() // RANDOM target
	if next, want := outputTextEngine.rng.next(), outputTextControl.next(); next != want {
		return fmt.Errorf("official enemy OUTPUT_TEXT consumed wrong RNG count: next=%d, want %d", next, want)
	}
	schedulerCatalog := &CombatCatalog{
		Skills: map[int][]CombatSkillDefinition{
			900001: {{ID: 900001, FunctionID: 900001, Target: "SELF", Kind: "ATTACK", DamageKind: "PHYSICS"}},
			900002: {{ID: 900002, FunctionID: 900002, Target: "SELF", Kind: "SORCERY", DamageKind: "MAGIC"}},
			900003: {{ID: 900003, FunctionID: 900003, Target: "SELF"}},
			900004: {{ID: 900004, FunctionID: 900004, Target: "SELF"}},
		},
		SkillRoles: map[int][]CombatSkillRole{
			900001: {{Function: "OUTPUT_TEXT"}},
			900002: {{Function: "OUTPUT_TEXT"}},
			900003: {{Function: "OUTPUT_TEXT"}},
			900004: {deathFlagRole},
		},
	}
	newSchedulerEngine := func(actionsPerTurn int, actions ...CombatEnemyAction) *BattleEngine {
		value := &BattleEngine{catalog: schedulerCatalog, rng: newXorShift128(1), enemyUses: make(map[int]int), enemyCount: 1}
		value.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 100, MaxHP: 100, Level: CombatEnemyLevel{ActionsPerTurn: actionsPerTurn, Actions: actions}}
		return value
	}
	scheduler := newSchedulerEngine(3,
		CombatEnemyAction{Slot: 1, Category: "skill", SkillID: 900001, Priority: 20, ActionCost: 2, MaxUses: 1, Rate: 100},
		CombatEnemyAction{Slot: 2, Category: "skill", SkillID: 900002, Priority: 5, ActionCost: 2, MaxUses: 1, Rate: 100},
		CombatEnemyAction{Slot: 0, Category: "normal", SkillID: 900003, Priority: 10, ActionCost: 1, MaxUses: 100, Rate: 100},
	)
	plan := scheduler.buildEnemyActionPlan(&scheduler.enemies[0])
	if len(plan) != 2 || plan[0].action.SkillID != 900003 || plan[1].action.SkillID != 900001 {
		return fmt.Errorf("native enemy action budget/priority plan is %+v", plan)
	}
	missScheduler := newSchedulerEngine(3,
		CombatEnemyAction{Slot: 1, Category: "skill", SkillID: 900001, ActionCost: 2, MaxUses: 1, Rate: 0, CountOnMiss: true},
		CombatEnemyAction{Slot: 0, Category: "normal", SkillID: 900003, ActionCost: 2, MaxUses: 100, Rate: 100},
	)
	if failedPlan := missScheduler.buildEnemyActionPlan(&missScheduler.enemies[0]); len(failedPlan) != 0 {
		return fmt.Errorf("native failed-rate action budget produced plan %+v", failedPlan)
	}
	perSlotScheduler := newSchedulerEngine(2,
		CombatEnemyAction{Slot: 1, Category: "skill", SkillID: 900001, ActionCost: 1, MaxUses: 1, Rate: 100},
		CombatEnemyAction{Slot: 2, Category: "skill", SkillID: 900001, ActionCost: 1, MaxUses: 1, Rate: 100},
	)
	if perSlotPlan := perSlotScheduler.buildEnemyActionPlan(&perSlotScheduler.enemies[0]); len(perSlotPlan) != 2 {
		return fmt.Errorf("native per-slot max-use counters produced plan %+v", perSlotPlan)
	}
	zeroScheduler := newSchedulerEngine(0, CombatEnemyAction{Slot: 0, Category: "normal", SkillID: 900003, ActionCost: 0, MaxUses: 100, Rate: 100})
	if zeroPlan := zeroScheduler.buildEnemyActionPlan(&zeroScheduler.enemies[0]); len(zeroPlan) != 20 || zeroScheduler.enemies[0].ActionConsumed != 0 {
		return fmt.Errorf("native zero-cost actions must fit zero budget, bounded by 20 slots: %+v", zeroPlan)
	}
	chargeScheduler := newSchedulerEngine(10, CombatEnemyAction{Slot: 15, Category: "special", SkillID: 900001, ActionCost: 1, MaxUses: 1, Rate: 100})
	if chargePlan := chargeScheduler.buildEnemyActionPlan(&chargeScheduler.enemies[0]); len(chargePlan) != 0 {
		return fmt.Errorf("charge-start slot leaked into ordinary enemy phase: %+v", chargePlan)
	}
	if chargeStartPlan := chargeScheduler.buildEnemyChargeStartPlan(&chargeScheduler.enemies[0]); len(chargeStartPlan) != 1 || chargeScheduler.enemies[0].ActionConsumed != 1 {
		return fmt.Errorf("native charge-start plan is plan=%+v consumed=%d", chargeStartPlan, chargeScheduler.enemies[0].ActionConsumed)
	}
	signScheduler := newSchedulerEngine(2,
		CombatEnemyAction{Slot: 1, Category: "skill", SkillID: 900001, ActionCost: 1, MaxUses: 1, Rate: 100},
		CombatEnemyAction{Slot: 2, Category: "skill", SkillID: 900002, ActionCost: 1, MaxUses: 1, Rate: 100},
	)
	if sign := signScheduler.enemyAttackSign(&signScheduler.enemies[0]); sign != 3 || signScheduler.enemies[0].ActionConsumed != 0 || len(signScheduler.enemyUses) != 0 {
		return fmt.Errorf("native attack-sign probe is sign=%d consumed=%d uses=%v, want physics|magic without state commit", sign, signScheduler.enemies[0].ActionConsumed, signScheduler.enemyUses)
	}
	chargeTurn := newSchedulerEngine(2, CombatEnemyAction{Slot: 15, Category: "special", SkillID: 900001, ActionCost: 1, MaxUses: 1, Rate: 100})
	for index := range chargeTurn.players {
		chargeTurn.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 100, MaxHP: 100}
	}
	chargeTurn.phase = PhaseStarted
	chargeResults, err := nextTurnForBattleContract(chargeTurn)
	if err != nil {
		return fmt.Errorf("native charge-start turn phase failed: %w", err)
	}
	chargeFound := false
	for _, result := range chargeResults {
		if result.Command == CommandChargeStart && len(result.Args) == 3 && result.Args[0] == 5 && result.Args[1] == 900001 && result.Args[2] == 900001 {
			chargeFound = true
		}
	}
	if !chargeFound || chargeTurn.enemies[0].ActionConsumed != 1 {
		return fmt.Errorf("native CHARGE_START projection is found=%t consumed=%d results=%+v", chargeFound, chargeTurn.enemies[0].ActionConsumed, chargeResults)
	}
	orderScheduler := &BattleEngine{catalog: schedulerCatalog, rng: newXorShift128(1), enemyUses: make(map[int]int), enemyCount: 2, phase: PhaseUserAttack}
	for index := range orderScheduler.players {
		orderScheduler.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 100, MaxHP: 100}
	}
	orderScheduler.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 100, MaxHP: 100, Level: CombatEnemyLevel{ActionsPerTurn: 1, Actions: []CombatEnemyAction{{Slot: 1, Category: "skill", SkillID: 900001, Target: "SELF", Priority: 20, ActionCost: 1, MaxUses: 1, Rate: 100}}}}
	orderScheduler.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 100, MaxHP: 100, Level: CombatEnemyLevel{ActionsPerTurn: 1, Actions: []CombatEnemyAction{{Slot: 1, Category: "skill", SkillID: 900002, Target: "SELF", Priority: 10, ActionCost: 1, MaxUses: 1, Rate: 100}}}}
	orderResults, err := orderScheduler.EnemyPhase()
	if err != nil {
		return fmt.Errorf("native cross-enemy order simulation failed: %w", err)
	}
	orderedMembers := make([]int64, 0, 2)
	for _, result := range orderResults {
		if result.Command == 50 && len(result.Args) > 0 {
			orderedMembers = append(orderedMembers, result.Args[0])
		}
	}
	if len(orderedMembers) != 2 || orderedMembers[0] != 6 || orderedMembers[1] != 5 {
		return fmt.Errorf("native cross-enemy priority order is %v, want [6 5]", orderedMembers)
	}
	deathEngine := newSchedulerEngine(1, CombatEnemyAction{
		Slot: 20, Category: "death", SkillID: 900004, Target: "SELF", ActionCost: 0, MaxUses: 1, Rate: 100,
	})
	deathEngine.enemies[0].HP = 0
	deathResults, err := deathEngine.resolveEnemyDeath(nil, &deathEngine.enemies[0])
	if err != nil {
		return fmt.Errorf("native enemy death action simulation failed: %w", err)
	}
	if len(deathResults) != 2 || deathResults[0].Command != 50 || deathResults[1].Command != CommandEnemyBreak ||
		!deathEngine.enemies[0].hasAIFlag(3) || !deathEngine.enemies[0].DeathActionTriggered || !deathEngine.enemies[0].Broken ||
		deathEngine.enemyUses[enemyActionUseKey(&deathEngine.enemies[0], deathEngine.enemies[0].Level.Actions[0])] != 1 {
		return fmt.Errorf("native enemy death action order/state is results=%+v enemy=%+v uses=%v", deathResults, deathEngine.enemies[0], deathEngine.enemyUses)
	}
	repeatedDeathResults, err := deathEngine.resolveEnemyDeath(nil, &deathEngine.enemies[0])
	if err != nil || len(repeatedDeathResults) != 0 {
		return fmt.Errorf("native enemy death action repeated: results=%+v err=%v", repeatedDeathResults, err)
	}
	aliasSkill, aliasRoles, ok := targetEngine.enemySkillBase(36501109)
	if !ok || aliasSkill.FunctionID != 36501108 || aliasSkill.Target != "SINGER" || len(aliasRoles) != 1 || aliasRoles[0].Function != "ENEMY_CURSE" {
		return fmt.Errorf("official enemy function alias 36501109 is skill=%+v roles=%+v ok=%t", aliasSkill, aliasRoles, ok)
	}

	// CN SkillDataContainer offers up to five rows for one outer enemy skill.
	// Native FUN_000d7578 evaluates both conditions and keeps the first row at
	// the greatest BranchPriority; taking variants[0] silently disables 2,617
	// official conditional rows.
	branchEngine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 8, enemyCount: 3}
	for index := range branchEngine.players {
		branchEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 5000, MaxHP: 10000}
	}
	for index := 0; index < branchEngine.enemyCount; index++ {
		branchEngine.enemies[index] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 5, HP: 10000, MaxHP: 10000}
	}
	actor := &branchEngine.enemies[0]
	flagBase, _, ok := branchEngine.selectEnemySkillBranch(actor, 34301605, 1)
	if !ok || flagBase.FunctionID != 34301605 {
		return fmt.Errorf("enemy extension flag fallback is skill=%+v ok=%t", flagBase, ok)
	}
	actor.AIFlags |= 1 << 1
	flagged, _, ok := branchEngine.selectEnemySkillBranch(actor, 34301605, 1)
	if !ok || flagged.FunctionID != 34301635 {
		return fmt.Errorf("enemy extension flag branch is skill=%+v ok=%t", flagged, ok)
	}
	turnBranch, _, ok := branchEngine.selectEnemySkillBranch(actor, 31701111, 0)
	if !ok || turnBranch.FunctionID != 31701130 {
		return fmt.Errorf("enemy extension turn branch is skill=%+v ok=%t", turnBranch, ok)
	}
	hpBranch, _, ok := branchEngine.selectEnemySkillBranch(actor, 38301115, 1)
	if !ok || hpBranch.FunctionID != 38301145 {
		return fmt.Errorf("enemy extension target-HP branch is skill=%+v ok=%t", hpBranch, ok)
	}
	branchEngine.turnStats.PlayedByUser = [8]int{2, 2, 1, 1}
	playedBranch, _, ok := branchEngine.selectEnemySkillBranch(actor, 36501102, 5)
	if !ok || playedBranch.FunctionID != 36501152 {
		return fmt.Errorf("enemy extension friend-play branch is skill=%+v ok=%t", playedBranch, ok)
	}
	actor.TurnDamage, actor.TurnPhysical = 80000, 80000
	// SELF_DAMAGE now reads the calibrated per-attribute action record,
	// distinct from the aggregate accumulator used by revenge producers.
	branchEngine.recordSkillDamage(actor.MemberType, 80000, "WIND", "PHYSICS", false)
	damageBranch, _, ok := branchEngine.selectEnemySkillBranch(actor, 36701620, 0)
	if !ok || damageBranch.FunctionID != 36701620 {
		return fmt.Errorf("enemy extension self-damage branch is skill=%+v ok=%t", damageBranch, ok)
	}
	branchEngine.enemies[1].DiedTurn = branchEngine.turn
	branchEngine.enemies[1].TurnSkillStats.Died = true
	deadBranch, _, ok := branchEngine.selectEnemySkillBranch(actor, 44285206, 0)
	if !ok || deadBranch.FunctionID != 44285206 {
		return fmt.Errorf("enemy extension same-turn death branch is skill=%+v ok=%t", deadBranch, ok)
	}
	branchEngine.enemies[1].DiedTurn = branchEngine.turn - 1
	branchEngine.enemies[1].TurnSkillStats.Died = false
	if _, _, ok := branchEngine.selectEnemySkillBranch(actor, 44285206, 0); ok {
		return errors.New("enemy extension same-turn death branch matched an old death")
	}
	actor.Effects = []battleEffect{
		{Function: "POISON", Kind: 2},
		{Function: "BLEED", Kind: 2},
		{Function: "FREEZE", Kind: 2},
	}
	debuffKinds, _, ok := branchEngine.selectEnemySkillBranch(actor, 36801107, actor.MemberType)
	if !ok || debuffKinds.FunctionID != 36801147 {
		return fmt.Errorf("enemy extension debuff-kind branch is skill=%+v ok=%t", debuffKinds, ok)
	}

	// Player and enemy skills enter the same native FUN_000d7578 selector.
	// The player master currently has 4,020 conditional rows over the same
	// two-condition/five-slot shape, so its predicates must not retain the old
	// variants[0] and greater-or-equal priority approximation.
	playerBranchEngine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range playerBranchEngine.players {
		playerBranchEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, ArthurType: 1, HP: 10000, MaxHP: 10000}
	}
	playerBranchEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Effects: []battleEffect{{Function: "ELECTRIC", Kind: 2}}}
	actorPlayer := &playerBranchEngine.players[0]
	actorPlayer.Effects = []battleEffect{{Function: "ENCHANT", Kind: 1, Remaining: 1}}
	combinedAction := battleAction{memberType: 1, cardType: 1, target: 5, skill: catalog.Skills[11108882][0]}
	combinedBranch := playerBranchEngine.selectCombatSkillBranch(combinedAction, []battleAction{combinedAction}, map[string]int{"LIGHT": 1})
	if combinedBranch.FunctionID != 11108888 {
		return fmt.Errorf("player extension two-condition branch is %+v", combinedBranch)
	}
	actorPlayer.Effects = nil
	electricBranch := playerBranchEngine.selectCombatSkillBranch(combinedAction, []battleAction{combinedAction}, map[string]int{"LIGHT": 1})
	if electricBranch.FunctionID != 11108886 {
		return fmt.Errorf("player extension single-condition branch is %+v", electricBranch)
	}
	actorPlayer.BlessHolds = []battleBlessHold{{CardType: 22, Skill: CombatSkillDefinition{Attribute: "DARK"}, Remaining: 2}}
	playerBlessAction := battleAction{memberType: 1, cardType: 1, target: 5, skill: catalog.Skills[11108392][0]}
	playerBlessBranch := playerBranchEngine.selectCombatSkillBranch(playerBlessAction, []battleAction{playerBlessAction}, map[string]int{"DARK": 1})
	if playerBlessBranch.FunctionID != 11108394 || playerBlessBranch.Target != "ENEMY_ONE" {
		return fmt.Errorf("player extension typed blessing branch is %+v", playerBlessBranch)
	}
	actorPlayer.BlessHolds = nil
	actorPlayer.HP = 5000
	hpAction := battleAction{memberType: 1, cardType: 1, skill: catalog.Skills[14304592][0]}
	hpBranchPlayer := playerBranchEngine.selectCombatSkillBranch(hpAction, []battleAction{hpAction}, map[string]int{"FIRE": 1})
	if hpBranchPlayer.FunctionID != 14304594 {
		return fmt.Errorf("player extension friend-HP branch is %+v", hpBranchPlayer)
	}
	actorPlayer.HP = actorPlayer.MaxHP
	lowCostActions := make([]battleAction, 9)
	for index := range lowCostActions {
		lowCostActions[index] = battleAction{memberType: index%maxRoomMembers + 1, cardType: index + 1, skill: CombatSkillDefinition{Cost: 2}}
	}
	lowCostActions[0] = battleAction{memberType: 1, cardType: 1, skill: catalog.Skills[13603032][0]}
	lowCostActions[1].skill.Cost = 1
	lowCostBranch := playerBranchEngine.selectCombatSkillBranch(lowCostActions[0], lowCostActions, map[string]int{"LIGHT": 1})
	if lowCostBranch.FunctionID != 13603034 {
		return fmt.Errorf("player extension party-card-count/min-cost branch is %+v", lowCostBranch)
	}
	windCard := BattleCard{}
	for cardID := range catalog.Cards {
		skill, _, err := catalog.CardSkill(cardID, actorPlayer.ArthurType)
		if err == nil && combatAttributeMatches(skill.Attribute, "WIND") {
			windCard = BattleCard{CardID: cardID, Level: 1}
			break
		}
	}
	if windCard.CardID == 0 {
		return errors.New("official player extension catalog has no WIND deck card")
	}
	for index := range actorPlayer.Deck {
		if index < 8 {
			actorPlayer.Deck[index] = windCard
		}
	}
	deckAction := battleAction{memberType: 1, cardType: 1, target: 5, skill: catalog.Skills[11108052][0]}
	deckBranch := playerBranchEngine.selectCombatSkillBranch(deckAction, []battleAction{deckAction}, map[string]int{"WIND": 1})
	if deckBranch.FunctionID != 11108056 {
		return fmt.Errorf("player extension main-deck branch is %+v", deckBranch)
	}

	// Native roles 81/83/84 scale the parameter change from the acting
	// member's damage received during the current turn. The old generic fixed
	// buff path parsed p3 (for example 10) as a numeric source selector and
	// therefore reduced every one of these active official roles to zero.
	// Complete the fixed/self-parameter producer matrix with the enemy-side
	// representatives not exercised by the earlier ATK_UP/ATK_BREAK vectors.
	// These share arithmetic helpers but have distinct native subtype, target,
	// sign and BATTLE_BUFF identities.
	enemyDefenseSelf, err := findEnemyRole(31001112, "DEF_UP_BY_SELF_PARAM", "DEF")
	if err != nil {
		return err
	}
	if enemyDefenseSelf.Parameters[2] != "MND" || selfScaledParameterValue(*enemyDefenseSelf, 0, 1, 1000) != 1000 {
		return fmt.Errorf("official enemy DEF_UP_BY_SELF_PARAM changed: %+v", enemyDefenseSelf)
	}
	enemyDefenseEngine := newSphereContractEngine(catalog)
	enemyDefenseResults, err := enemyDefenseEngine.executeEnemyParameterRole(&enemyDefenseEngine.enemies[0], 5, *enemyDefenseSelf)
	if err != nil {
		return err
	}
	if len(enemyDefenseResults) != 2 || enemyDefenseResults[0].Command != CommandBuff ||
		enemyDefenseResults[0].Args[3] != int64(battleBuffCodes["DEF_UP_BY_SELF_PARAM"]) ||
		enemyDefenseEngine.enemies[0].Defense != 2000 || len(enemyDefenseEngine.enemies[0].Effects) != 1 ||
		enemyDefenseEngine.enemies[0].Effects[0].Delta != 1000 {
		return fmt.Errorf("official enemy DEF_UP_BY_SELF_PARAM state is results=%+v enemy=%+v", enemyDefenseResults, enemyDefenseEngine.enemies[0])
	}

	enemyGuardSelf, err := findEnemyRole(30701115, "GUARD_BREAK_BY_SELF_PARAM", "DEF")
	if err != nil {
		return err
	}
	if enemyGuardSelf.Parameters[2] != "MND" || selfScaledParameterValue(*enemyGuardSelf, 0, 1, 1000) != 500 {
		return fmt.Errorf("official enemy GUARD_BREAK_BY_SELF_PARAM changed: %+v", enemyGuardSelf)
	}
	enemyGuardSelfEngine := newSphereContractEngine(catalog)
	enemyGuardSelfResults, err := enemyGuardSelfEngine.executeEnemyParameterRole(&enemyGuardSelfEngine.enemies[0], 5, *enemyGuardSelf)
	if err != nil {
		return err
	}
	if len(enemyGuardSelfResults) != 2 || enemyGuardSelfResults[0].Command != CommandBuff ||
		enemyGuardSelfResults[0].Args[3] != int64(battleBuffCodes["GUARD_BREAK_BY_SELF_PARAM"]) ||
		enemyGuardSelfEngine.enemies[0].Defense != 500 || len(enemyGuardSelfEngine.enemies[0].Effects) != 1 ||
		enemyGuardSelfEngine.enemies[0].Effects[0].Delta != -500 {
		return fmt.Errorf("official enemy GUARD_BREAK_BY_SELF_PARAM state is results=%+v enemy=%+v", enemyGuardSelfResults, enemyGuardSelfEngine.enemies[0])
	}

	enemyGuardFixed, err := findEnemyRole(30301122, "GUARD_BREAK_FIXED", "MDEF")
	if err != nil {
		return err
	}
	if fixedBuffRoleValue(*enemyGuardFixed, 0, 1) != 5000 {
		return fmt.Errorf("official enemy GUARD_BREAK_FIXED changed: %+v", enemyGuardFixed)
	}
	enemyGuardFixedEngine := newSphereContractEngine(catalog)
	enemyGuardFixedEngine.enemies[0].MDefense = 6000
	enemyGuardFixedEngine.enemies[0].BaseMDefense = 6000
	enemyGuardFixedResults, err := enemyGuardFixedEngine.executeEnemyParameterRole(&enemyGuardFixedEngine.enemies[0], 5, *enemyGuardFixed)
	if err != nil {
		return err
	}
	if len(enemyGuardFixedResults) != 2 || enemyGuardFixedResults[0].Command != CommandBuff ||
		enemyGuardFixedResults[0].Args[3] != int64(battleBuffCodes["GUARD_BREAK_FIXED"]) ||
		enemyGuardFixedEngine.enemies[0].MDefense != 1000 || len(enemyGuardFixedEngine.enemies[0].Effects) != 1 ||
		enemyGuardFixedEngine.enemies[0].Effects[0].Delta != -5000 {
		return fmt.Errorf("official enemy GUARD_BREAK_FIXED state is results=%+v enemy=%+v", enemyGuardFixedResults, enemyGuardFixedEngine.enemies[0])
	}

	// Role 142's enemy master is a separate reachable side of the ceiling
	// contract. All 18 rows select one player and raise ATK or INT; they do not
	// mutate the raw base or target an enemy merely because the actor
	// is an enemy. Official 38111146 carries two 120-point rows and exercises
	// the SELECT partition, enemy source identity and both ResultCmd6 limits.
	enemyLimitAttack, err := findEnemyRole(38111146, "PARAM_LIMIT_BREAK_FIXED", "ATK")
	if err != nil {
		return err
	}
	enemyLimitMagic, err := findEnemyRole(38111146, "PARAM_LIMIT_BREAK_FIXED", "INT")
	if err != nil {
		return err
	}
	if fixedBuffRoleValue(*enemyLimitAttack, 0, 1) != 120 || fixedBuffRoleValue(*enemyLimitMagic, 0, 1) != 120 ||
		enemyLimitAttack.Target != "SELECT" || enemyLimitMagic.Target != "SELECT" {
		return fmt.Errorf("official enemy parameter-limit rows changed: ATK=%+v INT=%+v", enemyLimitAttack, enemyLimitMagic)
	}
	enemyLimitEngine := newSphereContractEngine(catalog)
	enemyLimitEngine.players[0].Attack = 150000
	enemyLimitEngine.players[0].BaseAttack = 150000
	enemyLimitEngine.players[0].Magic = 140000
	enemyLimitEngine.players[0].BaseMagic = 140000
	enemyLimitAttackResults, err := enemyLimitEngine.executeEnemyParameterLimit(
		&enemyLimitEngine.enemies[0], 1, *enemyLimitAttack,
	)
	if err != nil {
		return err
	}
	enemyLimitMagicResults, err := enemyLimitEngine.executeEnemyParameterLimit(
		&enemyLimitEngine.enemies[0], 1, *enemyLimitMagic,
	)
	if err != nil {
		return err
	}
	limitPlayer := &enemyLimitEngine.players[0]
	if len(enemyLimitAttackResults) != 2 || len(enemyLimitMagicResults) != 2 ||
		enemyLimitAttackResults[0].Command != CommandBuff || enemyLimitAttackResults[1].Command != CommandBattleParam ||
		enemyLimitMagicResults[0].Command != CommandBuff || enemyLimitMagicResults[1].Command != CommandBattleParam ||
		enemyLimitAttackResults[1].Args[8] != 100119 || enemyLimitMagicResults[1].Args[9] != 100119 ||
		limitPlayer.LimitAttack != 100119 || limitPlayer.LimitMagic != 100119 ||
		limitPlayer.BaseAttack != 150000 || limitPlayer.BaseMagic != 140000 ||
		limitPlayer.Attack != 100119 || limitPlayer.Magic != 100119 || len(limitPlayer.Effects) != 2 ||
		limitPlayer.Effects[0].Source != 5 || limitPlayer.Effects[1].Source != 5 ||
		limitPlayer.Effects[0].Delta != 120 || limitPlayer.Effects[1].Delta != 120 {
		return fmt.Errorf("official enemy parameter-limit state is ATK=%+v INT=%+v player=%+v", enemyLimitAttackResults, enemyLimitMagicResults, limitPlayer)
	}

	// The shared attack-option collector does not by itself prove the enemy
	// consumer. These four active families have 2,827 official enemy rows and
	// must retain the enemy actor, SELECT player target, defense partition,
	// actual-damage accounting and post-commit drain lifecycle. Use one official
	// full attack from each family rather than treating player-side arithmetic as
	// evidence for the opposite side.
	enemyPiercingAttack, err := findEnemyRole(237101111, "ATTACK_AA", "")
	if err != nil {
		return err
	}
	enemyPiercingRole, err := findEnemyRole(237101111, "ATK_OP_PIERCING", "")
	if err != nil {
		return err
	}
	if enemyPiercingAttack.Target != "SELECT" || enemyPiercingRole.Target != "SELECT" ||
		attackOperatorLevelValue(*enemyPiercingRole, 0, 1) != 30 {
		return fmt.Errorf("official enemy piercing rows changed: attack=%+v operator=%+v", enemyPiercingAttack, enemyPiercingRole)
	}
	enemyPiercingEngine := newSphereContractEngine(catalog)
	enemyPiercingEngine.players[0].HP = 10000
	enemyPiercingEngine.players[0].MaxHP = 10000
	enemyPiercingEngine.players[0].Defense = 1000
	enemyPiercingResults, err := enemyPiercingEngine.executeEnemyAttack(
		&enemyPiercingEngine.enemies[0], 1, *enemyPiercingAttack,
		[]CombatSkillRole{*enemyPiercingRole, *enemyPiercingAttack},
	)
	if err != nil {
		return err
	}
	if enemyPiercingEngine.players[0].HP != 8400 || enemyPiercingEngine.players[0].DamageTaken != 1600 ||
		len(enemyPiercingResults) != 3 || enemyPiercingResults[0].Command != 60 ||
		enemyPiercingResults[1].Command != 60 || enemyPiercingResults[2].Command != CommandHP ||
		enemyPiercingResults[0].Args[2] != -800 || enemyPiercingResults[1].Args[2] != -800 ||
		enemyPiercingResults[0].Args[9] != 5 || enemyPiercingResults[1].Args[9] != 5 {
		return fmt.Errorf("official enemy piercing attack is player=%+v results=%+v", enemyPiercingEngine.players[0], enemyPiercingResults)
	}

	enemyIncreaseAttack, err := findEnemyRole(35501119, "ATTACK_AA", "")
	if err != nil {
		return err
	}
	enemyIncreaseRole, err := findEnemyRole(35501119, "ATK_OP_DAMAGE_INCREASE", "")
	if err != nil {
		return err
	}
	enemyIncreaseEngine := newSphereContractEngine(catalog)
	enemyIncreaseEngine.enemies[0].HP = 8000
	enemyIncreaseEngine.enemies[0].MaxHP = 10000
	enemyIncreaseEngine.players[0].HP = 10000
	enemyIncreaseEngine.players[0].MaxHP = 10000
	enemyIncreaseEngine.players[0].Defense = 1000
	increaseModifiers := collectAttackModifiers([]CombatSkillRole{*enemyIncreaseRole}, playerFromEnemy(&enemyIncreaseEngine.enemies[0]), 0, 1, false)
	if enemyIncreaseRole.Target != "SELF" || increaseModifiers.damageIncrease != 64 {
		return fmt.Errorf("official enemy damage-increase projection is role=%+v modifiers=%+v", enemyIncreaseRole, increaseModifiers)
	}
	enemyIncreaseResults, err := enemyIncreaseEngine.executeEnemyAttack(
		&enemyIncreaseEngine.enemies[0], 1, *enemyIncreaseAttack,
		[]CombatSkillRole{*enemyIncreaseAttack, *enemyIncreaseRole},
	)
	if err != nil {
		return err
	}
	if enemyIncreaseEngine.players[0].HP != 8936 || len(enemyIncreaseResults) != 2 ||
		enemyIncreaseResults[0].Command != 60 || enemyIncreaseResults[0].Args[2] != -1064 ||
		enemyIncreaseResults[0].Args[9] != 5 || enemyIncreaseResults[1].Command != CommandHP {
		return fmt.Errorf("official enemy damage-increase attack is player=%+v results=%+v", enemyIncreaseEngine.players[0], enemyIncreaseResults)
	}

	enemyRevengeAttack, err := findEnemyRole(35601113, "ATTACK_AA", "")
	if err != nil {
		return err
	}
	enemyRevengeRole, err := findEnemyRole(35601113, "ATK_OP_REVENGE", "")
	if err != nil {
		return err
	}
	enemyRevengeEngine := newSphereContractEngine(catalog)
	enemyRevengeEngine.enemies[0].HP = 8000
	enemyRevengeEngine.enemies[0].MaxHP = 10000
	enemyRevengeEngine.enemies[0].DamageTaken = 4000
	enemyRevengeEngine.players[0].HP = 10000
	enemyRevengeEngine.players[0].MaxHP = 10000
	enemyRevengeEngine.players[0].Defense = 1000
	revengeModifiers := collectAttackModifiers([]CombatSkillRole{*enemyRevengeRole}, playerFromEnemy(&enemyRevengeEngine.enemies[0]), 0, 1, false)
	if revengeModifiers.revengeRate != 30 || revengeModifiers.revengeParameter != "ATK" ||
		revengeModifiers.revengeCapRate != 30 ||
		attackRevengeBonus(4000, playerFromEnemy(&enemyRevengeEngine.enemies[0]), revengeModifiers.revengeRate, revengeModifiers.revengeParameter, revengeModifiers.revengeCapRate) != 300 {
		return fmt.Errorf("official enemy cumulative-revenge projection is %+v", revengeModifiers)
	}
	enemyRevengeResults, err := enemyRevengeEngine.executeEnemyAttack(
		&enemyRevengeEngine.enemies[0], 1, *enemyRevengeAttack,
		[]CombatSkillRole{*enemyRevengeRole, *enemyRevengeAttack},
	)
	if err != nil {
		return err
	}
	if enemyRevengeEngine.players[0].HP != 8700 || len(enemyRevengeResults) != 2 ||
		enemyRevengeResults[0].Command != 60 || enemyRevengeResults[0].Args[2] != -1300 ||
		enemyRevengeResults[0].Args[9] != 5 || enemyRevengeResults[1].Command != CommandHP {
		return fmt.Errorf("official enemy cumulative-revenge attack is player=%+v results=%+v", enemyRevengeEngine.players[0], enemyRevengeResults)
	}

	enemyDrainAttack, err := findEnemyRole(36101109, "ATTACK_AA", "")
	if err != nil {
		return err
	}
	enemyDrainRole, err := findEnemyRole(36101109, "ATK_OP_DRAIN", "")
	if err != nil {
		return err
	}
	enemyDrainEngine := newSphereContractEngine(catalog)
	enemyDrainEngine.enemies[0].HP = 5000
	enemyDrainEngine.enemies[0].MaxHP = 10000
	enemyDrainEngine.players[0].HP = 10000
	enemyDrainEngine.players[0].MaxHP = 10000
	enemyDrainEngine.players[0].Defense = 500
	enemyDrainResults, err := enemyDrainEngine.executeEnemyAttack(
		&enemyDrainEngine.enemies[0], 1, *enemyDrainAttack,
		[]CombatSkillRole{*enemyDrainRole, *enemyDrainAttack},
	)
	if err != nil {
		return err
	}
	if enemyDrainEngine.players[0].HP != 9000 || enemyDrainEngine.enemies[0].HP != 6000 ||
		len(enemyDrainResults) != 3 || enemyDrainResults[0].Command != 60 ||
		enemyDrainResults[0].Args[2] != -1000 || enemyDrainResults[0].Args[9] != 5 ||
		enemyDrainResults[1].Command != CommandHP || enemyDrainResults[2].Command != 61 ||
		enemyDrainResults[2].Args[0] != 5 || enemyDrainResults[2].Args[1] != int64(enemyDrainAttack.RoleIndex) ||
		enemyDrainResults[2].Args[2] != 1000 {
		return fmt.Errorf("official enemy drain attack is actor=%+v player=%+v results=%+v", enemyDrainEngine.enemies[0], enemyDrainEngine.players[0], enemyDrainResults)
	}

	// BUFF_RELEASE_ONE has 349 active enemy rows but was previously represented
	// only by the shared DEBUFF_RELEASE_ONE selector. Official 31801118 requests
	// two concrete buff kinds. Both are tested before removal, share one rate
	// roll and emit the complete enemy-to-player 66/72/71/6 lifecycle while an
	// unrelated DEF buff survives.
	enemyOneBuffRelease, err := findEnemyRole(31801118, "BUFF_RELEASE_ONE", "")
	if err != nil {
		return err
	}
	if enemyOneBuffRelease.Target != "SELECT" || enemyOneBuffRelease.Parameters[0] != "100" ||
		enemyOneBuffRelease.Parameters[1] != "0" || enemyOneBuffRelease.Parameters[2] != "ATK_UP_BY_ATK" ||
		enemyOneBuffRelease.Parameters[3] != "ATK_UP_BY_INT" {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE row changed: %+v", enemyOneBuffRelease)
	}
	enemyOneBuffEngine := newSphereContractEngine(catalog)
	enemyOneBuffEngine.rng = newXorShift128(1)
	oneBuffPlayer := &enemyOneBuffEngine.players[0]
	oneBuffPlayer.BaseAttack = 1000
	oneBuffPlayer.BaseMagic = 1000
	oneBuffPlayer.BaseDefense = 1000
	oneBuffPlayer.Effects = []battleEffect{
		{Function: "ATK_UP_BY_SELF_PARAM", Kind: 1, Parameter: "ATK", Delta: 300, Value: 300, Remaining: 3, Source: 2, RoleIndex: 4},
		{Function: "ATK_UP_BY_SELF_PARAM", Kind: 1, Parameter: "INT", Delta: 400, Value: 400, Remaining: 3, Source: 2, RoleIndex: 5},
		{Function: "DEF_UP_FIXED", Kind: 1, Parameter: "DEF", Delta: 500, Value: 500, Remaining: 3, Source: 3, RoleIndex: 6},
	}
	refreshPlayerBattleParameters(oneBuffPlayer)
	enemyOneBuffResults, err := enemyOneBuffEngine.executeEnemyRelease(
		&enemyOneBuffEngine.enemies[0], 1, *enemyOneBuffRelease,
	)
	if err != nil {
		return err
	}
	wantOneBuffCommands := []int{CommandBuffRelease, 72, CommandBuffLostOne, CommandBattleParam, 72, CommandBuffLostOne, CommandBattleParam}
	if len(enemyOneBuffResults) != len(wantOneBuffCommands) || len(oneBuffPlayer.Effects) != 1 ||
		oneBuffPlayer.Effects[0].Function != "DEF_UP_FIXED" || oneBuffPlayer.Attack != 1000 ||
		oneBuffPlayer.Magic != 1000 || oneBuffPlayer.Defense != 1500 ||
		len(enemyOneBuffResults[0].Args) != 8 || enemyOneBuffResults[0].Args[0] != 1 ||
		enemyOneBuffResults[0].Args[2] != 1 || enemyOneBuffResults[0].Args[3] != 2 {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE state is player=%+v results=%+v", oneBuffPlayer, enemyOneBuffResults)
	}
	for index, command := range wantOneBuffCommands {
		if enemyOneBuffResults[index].Command != command {
			return fmt.Errorf("official enemy BUFF_RELEASE_ONE command %d is %d, want %d", index, enemyOneBuffResults[index].Command, command)
		}
	}
	if next := enemyOneBuffEngine.rng.next(); next != 442046446 {
		return fmt.Errorf("official enemy BUFF_RELEASE_ONE next xor128 is %d, want 442046446", next)
	}

	// Every one of the 233 active enemy REVIVE rows uses SELECT, whereas the
	// earlier arithmetic-only vector used a synthetic DEAD_ENEMY_ONE target.
	// Official 35101101 proves that the concrete dead enemy member selected by
	// the outer action survives the role-target partition and receives only the
	// native SkillRevive row (D-331 original API receipt).
	enemyReviveRole, err := findEnemyRole(35101101, "REVIVE", "")
	if err != nil {
		return err
	}
	if enemyReviveRole.Target != "SELECT" || enemyReviveRole.Parameters[0] != "600" ||
		enemyReviveRole.Parameters[1] != "0" || enemyReviveRole.Parameters[2] != "0" {
		return fmt.Errorf("official enemy REVIVE row changed: %+v", enemyReviveRole)
	}
	officialReviveEngine := &BattleEngine{catalog: catalog, enemyCount: 2}
	officialReviveEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	officialReviveEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 6, HP: 0, MaxHP: 10000, Broken: true,
		DeathActionTriggered: true, DiedTurn: 3, DeathCount: 1,
	}
	officialReviveResults, err := officialReviveEngine.executeEnemyRevive(
		&officialReviveEngine.enemies[0], 6, *enemyReviveRole,
	)
	if err != nil {
		return err
	}
	revivedEnemy := &officialReviveEngine.enemies[1]
	if len(officialReviveResults) != 1 || officialReviveResults[0].Command != CommandSkillRevive ||
		!equalBattleArgs(officialReviveResults[0].Args, []int64{6, int64(enemyReviveRole.RoleIndex), 6000, 6000}) ||
		revivedEnemy.HP != 6000 || revivedEnemy.Broken || revivedEnemy.DeathActionTriggered ||
		revivedEnemy.DiedTurn != 0 || revivedEnemy.DeathCount != 1 {
		return fmt.Errorf("official enemy SELECT REVIVE state is enemy=%+v results=%+v", revivedEnemy, officialReviveResults)
	}

	// Bind the synthetic barrier-consumer arithmetic above to official
	// player/enemy object graphs. Appointed barriers retain their attribute and
	// source on both sides; the ordinary enemy SELF barrier keeps threshold and
	// hit count independent even when its official threshold is only one.
	playerAppointedBarrier, err := findPlayerRole(12102254, "ATTACK_BARRIER_APPOINT_ATTR")
	if err != nil {
		return err
	}
	if playerAppointedBarrier.Target != "SELF" || playerAppointedBarrier.Parameters[0] != "2" ||
		playerAppointedBarrier.Parameters[1] != "1000000" || playerAppointedBarrier.Parameters[3] != "3" ||
		playerAppointedBarrier.Parameters[4] != "ALL" || playerAppointedBarrier.Parameters[5] != "DARK" {
		return fmt.Errorf("official player appointed-barrier row changed: %+v", playerAppointedBarrier)
	}
	playerBarrierEngine := newSphereContractEngine(catalog)
	playerBarrierResults, err := playerBarrierEngine.executePersistentEffect(
		battleAction{memberType: 1, target: 1, cardLevel: 60}, *playerAppointedBarrier, 60, 1,
	)
	if err != nil {
		return err
	}
	playerBarrierEffects := &playerBarrierEngine.players[0].Effects
	if len(playerBarrierResults) != 1 || playerBarrierResults[0].Command != CommandBuff ||
		len(*playerBarrierEffects) != 1 || (*playerBarrierEffects)[0].Value != 1000000 ||
		(*playerBarrierEffects)[0].Uses != 3 || (*playerBarrierEffects)[0].Remaining != 2 ||
		(*playerBarrierEffects)[0].DamageKind != "ALL" || (*playerBarrierEffects)[0].Attribute != "DARK" ||
		(*playerBarrierEffects)[0].Source != 1 {
		return fmt.Errorf("official player appointed-barrier state is results=%+v effects=%+v", playerBarrierResults, *playerBarrierEffects)
	}
	playerBarrierMismatch := playerBarrierEngine.resolveIncomingDamageEffects(playerBarrierEffects, 5000, 10000, "LIGHT", "PHYSICS")
	playerBarrierAbsorb := playerBarrierEngine.resolveIncomingDamageEffects(playerBarrierEffects, 5000, 10000, "DARK", "MAGIC")
	if playerBarrierMismatch.Damage != 5000 || playerBarrierAbsorb.Damage != 0 ||
		playerBarrierAbsorb.BarrierRemainingUses != 2 || len(*playerBarrierEffects) != 1 {
		return fmt.Errorf("official player appointed-barrier consumption is mismatch=%+v absorb=%+v effects=%+v", playerBarrierMismatch, playerBarrierAbsorb, *playerBarrierEffects)
	}

	enemyAppointedBarrier, err := findEnemyRole(38111142, "ATTACK_BARRIER_APPOINT_ATTR", "")
	if err != nil {
		return err
	}
	if enemyAppointedBarrier.Target != "SELECT" || enemyAppointedBarrier.Parameters[0] != "2" ||
		enemyAppointedBarrier.Parameters[1] != "100000000" || enemyAppointedBarrier.Parameters[3] != "2" ||
		enemyAppointedBarrier.Parameters[4] != "ALL" || enemyAppointedBarrier.Parameters[5] != "ICE" {
		return fmt.Errorf("official enemy appointed-barrier row changed: %+v", enemyAppointedBarrier)
	}
	enemyAppointedBarrierEngine := newSphereContractEngine(catalog)
	enemyAppointedBarrierResults, err := enemyAppointedBarrierEngine.executeEnemyPersistentEffect(
		&enemyAppointedBarrierEngine.enemies[0], 1, *enemyAppointedBarrier,
	)
	if err != nil {
		return err
	}
	enemyAppointedEffects := &enemyAppointedBarrierEngine.players[0].Effects
	if len(enemyAppointedBarrierResults) != 1 || enemyAppointedBarrierResults[0].Command != CommandBuff ||
		len(*enemyAppointedEffects) != 1 || (*enemyAppointedEffects)[0].Value != 100000000 ||
		(*enemyAppointedEffects)[0].Uses != 2 || (*enemyAppointedEffects)[0].Remaining != 2 ||
		(*enemyAppointedEffects)[0].Attribute != "ICE" || (*enemyAppointedEffects)[0].Source != 5 {
		return fmt.Errorf("official enemy appointed-barrier state is results=%+v effects=%+v", enemyAppointedBarrierResults, *enemyAppointedEffects)
	}
	enemyAppointedMismatch := enemyAppointedBarrierEngine.resolveIncomingDamageEffects(enemyAppointedEffects, 5000, 10000, "FIRE", "PHYSICS")
	enemyAppointedAbsorb := enemyAppointedBarrierEngine.resolveIncomingDamageEffects(enemyAppointedEffects, 5000, 10000, "ICE", "PHYSICS")
	enemyAppointedRows := damageEffectResults(1, enemyAppointedAbsorb)
	if enemyAppointedMismatch.Damage != 5000 || enemyAppointedAbsorb.Damage != 0 ||
		enemyAppointedAbsorb.BarrierRemainingUses != 1 || len(enemyAppointedRows) != 1 ||
		enemyAppointedRows[0].Command != 200 || enemyAppointedRows[0].Args[2] != 1 ||
		enemyAppointedRows[0].Args[3] != int64(combatAttributeCode("ICE")) {
		return fmt.Errorf("official enemy appointed-barrier consumption is mismatch=%+v absorb=%+v rows=%+v", enemyAppointedMismatch, enemyAppointedAbsorb, enemyAppointedRows)
	}

	enemySelfBarrier, err := findEnemyRole(44279009, "ATTACK_BARRIER", "")
	if err != nil {
		return err
	}
	if enemySelfBarrier.Target != "SELF" || enemySelfBarrier.Parameters[0] != "3" ||
		enemySelfBarrier.Parameters[1] != "1" || enemySelfBarrier.Parameters[2] != "1" ||
		enemySelfBarrier.Parameters[3] != "4" || enemySelfBarrier.Parameters[4] != "ALL" {
		return fmt.Errorf("official enemy self-barrier row changed: %+v", enemySelfBarrier)
	}
	enemySelfBarrierEngine := newSphereContractEngine(catalog)
	enemySelfBarrierResults, err := enemySelfBarrierEngine.executeEnemyPersistentEffect(
		&enemySelfBarrierEngine.enemies[0], 5, *enemySelfBarrier,
	)
	if err != nil {
		return err
	}
	enemySelfEffects := &enemySelfBarrierEngine.enemies[0].Effects
	if len(enemySelfBarrierResults) != 1 || enemySelfBarrierResults[0].Command != CommandBuff ||
		len(*enemySelfEffects) != 1 || (*enemySelfEffects)[0].Value != 2 ||
		(*enemySelfEffects)[0].Uses != 4 || (*enemySelfEffects)[0].Remaining != 3 ||
		(*enemySelfEffects)[0].DamageKind != "ALL" || (*enemySelfEffects)[0].Attribute != "" ||
		(*enemySelfEffects)[0].Source != 5 {
		return fmt.Errorf("official enemy self-barrier state is results=%+v effects=%+v", enemySelfBarrierResults, *enemySelfEffects)
	}
	enemySelfAbsorb := enemySelfBarrierEngine.resolveIncomingDamageEffects(enemySelfEffects, 1, 10000, "WIND", "PHYSICS")
	enemySelfOver := enemySelfBarrierEngine.resolveIncomingDamageEffects(enemySelfEffects, 3, 10000, "WIND", "MAGIC")
	if enemySelfAbsorb.Damage != 0 || enemySelfAbsorb.BarrierRemainingUses != 3 ||
		enemySelfOver.Damage != 3 || enemySelfOver.BarrierRemainingUses != 2 || len(*enemySelfEffects) != 1 {
		return fmt.Errorf("official enemy self-barrier consumption is absorb=%+v over=%+v effects=%+v", enemySelfAbsorb, enemySelfOver, *enemySelfEffects)
	}

	// Reflection also needs both official side/object graphs. The shared
	// consumer is not sufficient evidence for player-selected application,
	// enemy-selected application, reflected source identity, attacker ENDURE
	// ordering and the authoritative HP rows emitted by each attack path.
	playerReflectionRole, err := findPlayerRole(12400252, "REFLECTION")
	if err != nil {
		return err
	}
	if playerReflectionRole.Target != "SELECT" || playerReflectionRole.Parameters[0] != "2" ||
		playerReflectionRole.Parameters[1] != "15400" || playerReflectionRole.Parameters[2] != "770" ||
		playerReflectionRole.Parameters[3] != "ALL" {
		return fmt.Errorf("official player REFLECTION row changed: %+v", playerReflectionRole)
	}
	playerReflectionEngine := newSphereContractEngine(catalog)
	playerReflectionResults, err := playerReflectionEngine.executePersistentEffect(
		battleAction{memberType: 1, target: 1, cardLevel: 60}, *playerReflectionRole, 60, 1,
	)
	if err != nil {
		return err
	}
	playerReflectionEffects := &playerReflectionEngine.players[0].Effects
	if len(playerReflectionResults) != 1 || playerReflectionResults[0].Command != CommandBuff ||
		len(*playerReflectionEffects) != 1 || (*playerReflectionEffects)[0].Rate != 61600 ||
		(*playerReflectionEffects)[0].Remaining != 2 || (*playerReflectionEffects)[0].DamageKind != "ALL" ||
		(*playerReflectionEffects)[0].Source != 1 {
		return fmt.Errorf("official player REFLECTION state is results=%+v effects=%+v", playerReflectionResults, *playerReflectionEffects)
	}
	playerReflectionEngine.enemies[0].HP = 10000
	playerReflectionEngine.enemies[0].MaxHP = 10000
	playerReflectionEngine.enemies[0].Effects = []battleEffect{{Function: "ENDURE", Kind: 1, Value: 50, Rate: 50, Remaining: 2}}
	playerReflectionEngine.players[0].HP = 10000
	playerReflectionEngine.players[0].MaxHP = 10000
	playerReflectionEngine.players[0].Defense = 0
	reflectionEnemyAttack := CombatSkillRole{RoleIndex: 7, Function: "ATTACK_AA", Target: "SELECT"}
	reflectionEnemyAttack.Parameters[0] = "100"
	reflectionEnemyAttack.Parameters[4] = "1"
	reflectionEnemyAttack.Parameters[7] = "LIGHT"
	reflectionEnemyAttack.Parameters[8] = "PHYSICS"
	playerReflectionAttackResults, err := playerReflectionEngine.executeEnemyAttack(
		&playerReflectionEngine.enemies[0], 1, reflectionEnemyAttack, []CombatSkillRole{reflectionEnemyAttack},
	)
	if err != nil {
		return err
	}
	if playerReflectionEngine.players[0].HP != 9900 || playerReflectionEngine.enemies[0].HP != 9384 ||
		len(*playerReflectionEffects) != 1 || (*playerReflectionEffects)[0].Remaining != 2 ||
		len(playerReflectionAttackResults) != 3 || playerReflectionAttackResults[0].Command != 60 ||
		playerReflectionAttackResults[0].Args[0] != 1 || playerReflectionAttackResults[0].Args[2] != -100 ||
		playerReflectionAttackResults[0].Args[9] != 5 || playerReflectionAttackResults[1].Command != 60 ||
		playerReflectionAttackResults[1].Args[0] != 5 || playerReflectionAttackResults[1].Args[2] != -616 ||
		playerReflectionAttackResults[1].Args[8] != 2 || playerReflectionAttackResults[1].Args[9] != 1 ||
		playerReflectionAttackResults[2].Command != CommandHP {
		return fmt.Errorf("official player REFLECTION attack is enemy=%+v player=%+v results=%+v", playerReflectionEngine.enemies[0], playerReflectionEngine.players[0], playerReflectionAttackResults)
	}

	enemyReflectionRole, err := findEnemyRole(35809511, "REFLECTION", "")
	if err != nil {
		return err
	}
	if enemyReflectionRole.Target != "SELECT" || enemyReflectionRole.Parameters[0] != "3" ||
		enemyReflectionRole.Parameters[1] != "500" || enemyReflectionRole.Parameters[2] != "0" ||
		enemyReflectionRole.Parameters[3] != "PHYSICS" {
		return fmt.Errorf("official enemy REFLECTION row changed: %+v", enemyReflectionRole)
	}
	enemyReflectionEngine := newSphereContractEngine(catalog)
	enemyReflectionEngine.enemies[0].HP = 10000
	enemyReflectionEngine.enemies[0].MaxHP = 10000
	enemyReflectionResults, err := enemyReflectionEngine.executeEnemyPersistentEffect(
		&enemyReflectionEngine.enemies[0], 5, *enemyReflectionRole,
	)
	if err != nil {
		return err
	}
	enemyReflectionEffects := &enemyReflectionEngine.enemies[0].Effects
	if len(enemyReflectionResults) != 1 || enemyReflectionResults[0].Command != CommandBuff ||
		len(*enemyReflectionEffects) != 1 || (*enemyReflectionEffects)[0].Rate != 500 ||
		(*enemyReflectionEffects)[0].Remaining != 3 || (*enemyReflectionEffects)[0].DamageKind != "PHYSICS" ||
		(*enemyReflectionEffects)[0].Source != 5 {
		return fmt.Errorf("official enemy REFLECTION state is results=%+v effects=%+v", enemyReflectionResults, *enemyReflectionEffects)
	}
	enemyReflectionMismatch := enemyReflectionEngine.resolveIncomingDamageEffects(enemyReflectionEffects, 1000, 10000, "WIND", "MAGIC")
	if enemyReflectionMismatch.Damage != 1000 || enemyReflectionMismatch.Reflected != 0 ||
		len(*enemyReflectionEffects) != 1 || (*enemyReflectionEffects)[0].Remaining != 3 {
		return fmt.Errorf("official enemy REFLECTION physics mismatch is resolution=%+v effects=%+v", enemyReflectionMismatch, *enemyReflectionEffects)
	}
	enemyReflectionEngine.players[0].HP = 10000
	enemyReflectionEngine.players[0].MaxHP = 10000
	enemyReflectionEngine.players[0].Effects = []battleEffect{{Function: "ENDURE", Kind: 1, Value: 50, Rate: 50, Remaining: 2}}
	enemyReflectionEngine.enemies[0].Defense = 0
	reflectionPlayerAttack := CombatSkillRole{RoleIndex: 8, Function: "ATTACK_AA", Target: "SELECT"}
	reflectionPlayerAttack.Parameters[0] = "1000"
	reflectionPlayerAttack.Parameters[4] = "1"
	reflectionPlayerAttack.Parameters[7] = "WIND"
	reflectionPlayerAttack.Parameters[8] = "PHYSICS"
	enemyReflectionAttackResults, err := enemyReflectionEngine.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{reflectionPlayerAttack}},
		reflectionPlayerAttack, 1,
	)
	if err != nil {
		return err
	}
	if enemyReflectionEngine.enemies[0].HP != 9000 || enemyReflectionEngine.players[0].HP != 9950 ||
		len(*enemyReflectionEffects) != 1 || (*enemyReflectionEffects)[0].Remaining != 3 ||
		len(enemyReflectionAttackResults) != 3 || enemyReflectionAttackResults[0].Command != 60 ||
		enemyReflectionAttackResults[0].Args[0] != 5 || enemyReflectionAttackResults[0].Args[2] != -1000 ||
		enemyReflectionAttackResults[0].Args[9] != 1 || enemyReflectionAttackResults[1].Command != 60 ||
		enemyReflectionAttackResults[1].Args[0] != 1 || enemyReflectionAttackResults[1].Args[2] != -50 ||
		enemyReflectionAttackResults[1].Args[8] != 2 || enemyReflectionAttackResults[1].Args[9] != 5 ||
		enemyReflectionAttackResults[2].Command != CommandHP {
		return fmt.Errorf("official enemy REFLECTION attack is enemy=%+v player=%+v results=%+v", enemyReflectionEngine.enemies[0], enemyReflectionEngine.players[0], enemyReflectionAttackResults)
	}
	enemySelfHeal, err := findEnemyRole(40000076, "HEAL_BY_SELF_PARAM", "")
	if err != nil {
		return err
	}
	if enemySelfHeal.Parameters[0] != "MND" {
		return fmt.Errorf("official enemy self-parameter heal source changed: %+v", *enemySelfHeal)
	}
	if value := selfScaledHealRoleValue(*enemySelfHeal, 0, 1, 4200); value != 4200 {
		return fmt.Errorf("official enemy self-parameter heal value is %d, want 4200", value)
	}
	enemyFixedRegenerate, err := findEnemyRole(35401163, "REGENERATE_FIXED", "")
	if err != nil {
		return err
	}
	if value := fixedRegenerateRoleValue(*enemyFixedRegenerate, 0, 1, 4200); value != 700 {
		return fmt.Errorf("official enemy fixed-regenerate value is %d, want 700", value)
	}
	enemyAttrUp, err := findEnemyRole(34701101, "ATTR_DEF_UP", "")
	if err != nil {
		return err
	}
	if primary, fixed := attributeDefenseRoleValues(*enemyAttrUp, 0, 1); primary != -1000 || fixed != 0 {
		return fmt.Errorf("official enemy attribute-defense-up is primary=%d fixed=%d, want -1000/0", primary, fixed)
	}
	enemyAttrEffect := persistentBattleEffect(*enemyAttrUp, persistentRoleValue(*enemyAttrUp, 0, &battlePlayer{}, 1), 2, 1, 1, 5, 0)
	enemyAttrResult := battlePersistentResult(5, *enemyAttrUp, battleBuffCodes["ATTR_DEF_UP"], enemyAttrEffect)
	if len(enemyAttrResult.Args) != 11 || enemyAttrResult.Args[7] != 0 || enemyAttrResult.Args[8] != 0 {
		return fmt.Errorf("official attribute-defense-up ResultCmd projection is %+v", enemyAttrResult)
	}
	aggregateEffects := []battleEffect{
		enemyAttrEffect,
		{Function: "ATTR_DEF_DOWN", Attribute: "ICE", Parameters: [4]int{1500, 5000}, Remaining: 1},
		{Function: "ATTR_DEF_DOWN", Attribute: "ICE", Parameters: [4]int{500, 1000}, Remaining: 1},
	}
	if primary, fixed := attributeDefenseAdjustment(aggregateEffects, "ICE"); primary != 500 || fixed != 6000 {
		return fmt.Errorf("attribute-defense strongest/sum aggregate is primary=%d fixed=%d, want 500/6000", primary, fixed)
	}
	damageEngine := &BattleEngine{rng: newXorShift128(1), enemyCount: 1}
	damageEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 50000, MaxHP: 50000}
	damageEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 5, HP: 50000, MaxHP: 50000, Defense: 1000,
		Effects: []battleEffect{{Function: "ATTR_DEF_DOWN", Attribute: "ICE", Parameters: [4]int{1000, 5000}, Remaining: 1}},
	}
	attackRole := CombatSkillRole{Function: "ATTACK_AA", Target: "SELECT"}
	attackRole.Parameters[0] = "10000"
	attackRole.Parameters[4] = "1"
	attackRole.Parameters[7] = "ICE"
	attackRole.Parameters[8] = "PHYSICS"
	attrDamageResults, err := damageEngine.executePlayerAttack(battleAction{memberType: 1, target: 5}, attackRole, 1)
	if err != nil {
		return err
	}
	if len(attrDamageResults) < 2 || attrDamageResults[0].Command != 60 || attrDamageResults[0].Args[2] != -24000 || attrDamageResults[0].Args[6] != 100 || damageEngine.enemies[0].HP != 26000 {
		return fmt.Errorf("attribute-defense damage consumer is results=%+v enemy=%+v", attrDamageResults, damageEngine.enemies[0])
	}

	// The managed enemy-level parser fixes every ordinary skill at level 1,
	// and native ATTR_DEF producers FUN_0009b6de/FUN_0009b4b4 read that same
	// level through the common skill object. Keep one full attack-plus-debuff
	// action: old enemy persistent effects passed
	// zero, which visibly projected a zero primary rate for the growth rows.

	attrGrowthRole, err := findEnemyRole(34101411, "ATTR_DEF_DOWN", "")
	if err != nil {
		return err
	}
	if attrGrowthRole.Target != "SELECT" || attrGrowthRole.Parameters[0] != "99" || attrGrowthRole.Parameters[1] != "0" ||
		attrGrowthRole.Parameters[2] != "300" || attrGrowthRole.Parameters[3] != "6500" || attrGrowthRole.Parameters[4] != "0" || attrGrowthRole.Parameters[5] != "LIGHT" ||
		EnemySkillLevel(attrGrowthRole.Function) != 1 {
		return fmt.Errorf("official attribute-defense growth role changed: %+v", *attrGrowthRole)
	}
	if oldPrimary, oldFixed := attributeDefenseRoleValues(*attrGrowthRole, 0, 1); oldPrimary != 0 || oldFixed != 6500 {
		return fmt.Errorf("attribute-defense level-zero control is primary=%d fixed=%d, want 0/6500", oldPrimary, oldFixed)
	}
	if primary, fixed := attributeDefenseRoleValues(*attrGrowthRole, 1, 1); primary != 300 || fixed != 6500 {
		return fmt.Errorf("attribute-defense level-one value is primary=%d fixed=%d, want 300/6500", primary, fixed)
	}
	attrGrowthLevel, ok := catalog.EnemyLevels[30690162]
	if !ok {
		return errors.New("official attribute-defense growth enemy level 30690162 is missing")
	}
	var attrGrowthAction CombatEnemyAction
	for _, action := range attrGrowthLevel.Actions {
		if action.SkillID == 34101411 && action.Target == "RANDOM" && action.ActionCost == 1 {
			attrGrowthAction = action
			break
		}
	}
	if attrGrowthLevel.HPBars != 4 || attrGrowthLevel.ActionsPerTurn != 3 || attrGrowthAction.Slot != 6 || attrGrowthAction.Category != "skill" ||
		attrGrowthAction.AIConditionID != 34101411 || attrGrowthAction.Priority != 9 || attrGrowthAction.Target != "RANDOM" ||
		attrGrowthAction.ActionCost != 1 || attrGrowthAction.MaxUses != 1000 || attrGrowthAction.Rate != 100 || attrGrowthAction.CountOnMiss {
		return fmt.Errorf("official attribute-defense growth action changed: level=%+v action=%+v", attrGrowthLevel, attrGrowthAction)
	}
	attrGrowthEngine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), enemyCount: 1}
	for index := range attrGrowthEngine.players {
		attrGrowthEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 50000, MaxHP: 50000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	attrGrowthEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 50000, MaxHP: 50000, Attack: 1000, Level: attrGrowthLevel}
	attrGrowthResults, err := attrGrowthEngine.executeEnemyActionCandidate(&attrGrowthEngine.enemies[0], enemyActionCandidate{action: attrGrowthAction})
	if err != nil {
		return err
	}
	if len(attrGrowthResults) != 6 || attrGrowthEngine.players[3].HP != 45000 || len(attrGrowthEngine.players[3].Effects) != 1 ||
		attrGrowthEngine.players[3].Effects[0].Function != "ATTR_DEF_DOWN" || attrGrowthEngine.players[3].Effects[0].Rate != 300 || attrGrowthEngine.players[3].Effects[0].Value != 6500 ||
		!equalBattleArgs(attrGrowthResults[0].Args, []int64{5, 34101411, 4, 1, 1, 34101411, 0}) ||
		!equalBattleArgs(attrGrowthResults[1].Args, []int64{4, 0, -5000, 50000, 0, 4, 100, 0, 0, 5}) ||
		!equalBattleArgs(attrGrowthResults[2].Args, []int64{4, 50000, 45000, 1}) ||
		!equalBattleArgs(attrGrowthResults[3].Args, []int64{4, int64(attrGrowthRole.RoleIndex), 0, int64(battleBuffCodes["ATTR_DEF_DOWN"]), 2, 0, 16, 0, 0, 0, 0}) ||
		attrGrowthResults[4].Command != CommandBattleParam ||
		!equalBattleArgs(attrGrowthResults[4].Args, battleParameterArgs(4, 45000, 50000, 0, 0, 0, 0, 0, 99999, 99999, 99999)) ||
		attrGrowthResults[5].Command != CommandFixedAttrDefense ||
		!equalBattleArgs(attrGrowthResults[5].Args, []int64{4, 0, 0, 0, -6500, 0}) {
		return fmt.Errorf("official attribute-defense level-one action is results=%+v player=%+v", attrGrowthResults, attrGrowthEngine.players[3])
	}
	attrGrowthAttack := catalog.SkillRoles[34101411][0]
	attrGrowthFollowUp, err := attrGrowthEngine.executeEnemyAttack(&attrGrowthEngine.enemies[0], 4, attrGrowthAttack, []CombatSkillRole{attrGrowthAttack})
	if err != nil {
		return err
	}
	if len(attrGrowthFollowUp) != 2 || attrGrowthEngine.players[3].HP != 32000 ||
		!equalBattleArgs(attrGrowthFollowUp[0].Args, []int64{4, 0, -13000, 45000, 0, 4, 100, 0, 0, 5}) ||
		!equalBattleArgs(attrGrowthFollowUp[1].Args, []int64{4, 50000, 32000, 1}) {
		return fmt.Errorf("official attribute-defense level-one consumer is results=%+v player=%+v", attrGrowthFollowUp, attrGrowthEngine.players[3])
	}
	// Native ENEMY_CURSE producer FUN_0009955e creates action type 20 and the
	// type-20 consumer FUN_00080510 resolves p1 through FUN_00057ed8: the
	// acting enemy's enemy_lvup call-skill slot. The hold is CURSE card type 21
	// and executes that call skill at USER_ATTACK_END.
	curseRole, err := findEnemyRole(36401113, "ENEMY_CURSE", "")
	if err != nil {
		return err
	}
	if curseRole.Parameters[0] != "99" || curseRole.Parameters[1] != "0" || curseRole.Parameters[2] != "1" {
		return fmt.Errorf("official ENEMY_CURSE parameters changed: %+v", *curseRole)
	}
	curseLevel, exists := catalog.EnemyLevels[30950123]
	if !exists || curseLevel.CallSkillIDs[0] != 36401170 {
		return fmt.Errorf("official curse call-skill object graph changed: %+v", curseLevel)
	}
	curseVariants := catalog.Skills[36401170]
	if len(curseVariants) == 0 || curseVariants[0].AppendTrigger != "USER_ATTACK_END" || curseVariants[0].AppendCondition != "SELF_PLAY_MOST_LOW_COST" || curseVariants[0].AppendParameters[0] != "0" || curseVariants[0].AppendParameters[1] != "2" {
		return fmt.Errorf("official curse call-skill append contract changed: %+v", curseVariants)
	}
	curseEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1, holdMax: 5}
	for index := range curseEngine.players {
		curseEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, ArthurType: index + 1, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	curseEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Level: curseLevel}
	curseResults, err := curseEngine.executeEnemyRole(&curseEngine.enemies[0], 1, *curseRole, []CombatSkillRole{*curseRole})
	if err != nil {
		return err
	}
	if len(curseResults) != 1 || curseResults[0].Command != CommandHoldSet || curseResults[0].Args[2] != 21 || curseResults[0].Args[4] != 36401170 ||
		len(curseEngine.players[0].BlessHolds) != 1 || len(curseEngine.players[0].Effects) != 0 || curseEngine.players[0].BlessHolds[0].CardType != 21 ||
		curseEngine.players[0].BlessHolds[0].SourceMember != 5 || !curseEngine.players[0].BlessHolds[0].EnemySkill ||
		curseEngine.players[0].BlessHolds[0].Remaining != 99 || !curseEngine.players[0].BlessHolds[0].Repeat {
		return fmt.Errorf("official ENEMY_CURSE append-card state is results=%+v player=%+v", curseResults, curseEngine.players[0])
	}
	curseEngine.selectedPlays = map[int]cardPlaySubmission{1: {CardTypes: [5]int{1}}}
	curseEngine.turnActions = []battleAction{{memberType: 1, cardType: 1, skill: CombatSkillDefinition{Cost: 3}}}
	blockedCurse, err := curseEngine.executeBlessHolds("USER_ATTACK_END")
	if err != nil {
		return err
	}
	if len(blockedCurse) != 1 || blockedCurse[0].Command != CommandHoldSkillEnd || len(curseEngine.players[0].BlessHolds) != 1 {
		return fmt.Errorf("curse append condition false path is results=%+v holds=%+v", blockedCurse, curseEngine.players[0].BlessHolds)
	}
	curseEngine.turnActions[0].skill.Cost = 2
	executedCurse, err := curseEngine.executeBlessHolds("USER_ATTACK_END")
	if err != nil {
		return err
	}
	if len(executedCurse) != 11 || executedCurse[0].Command != CommandHoldSkill || executedCurse[1].Command != 88 || !equalBattleArgs(executedCurse[1].Args, []int64{5, 0}) || executedCurse[len(executedCurse)-1].Command != CommandHoldSkillEnd || len(curseEngine.players[0].BlessHolds) != 1 {
		return fmt.Errorf("curse call-skill execution is results=%+v holds=%+v", executedCurse, curseEngine.players[0].BlessHolds)
	}
	for index := range curseEngine.players {
		member := int64(index + 1)
		if executedCurse[2+index*2].Command != CommandBuff || executedCurse[2+index*2].Args[0] != member ||
			executedCurse[3+index*2].Command != CommandBattleParam ||
			!equalBattleArgs(executedCurse[3+index*2].Args, []int64{member, 10000, 10000, 0, 0, 0, 0, 0, 99999, 99999, 99999}) {
			return fmt.Errorf("curse status/parameter child order changed for member %d: %+v", member, executedCurse)
		}
		if len(curseEngine.players[index].Effects) != 1 || curseEngine.players[index].Effects[0].Function != "DEAL_PENALTY" || curseEngine.players[index].Effects[0].Value != 1 {
			return fmt.Errorf("curse USER_ALL deal penalty missed member %d: %+v", index+1, curseEngine.players[index])
		}
	}

	// Native role 16 reuses the common regenerate action type 4/subtype 1.
	// The only active enemy row is four turns of the acting enemy's MND at a
	// 1000-per-mille coefficient; the target may be another enemy member.
	regenerateRole, err := findEnemyRole(40000079, "REGENERATE_BY_SELF_PARAM", "MND")
	if err != nil {
		return err
	}
	if regenerateRole.Parameters[0] != "4" || regenerateRole.Parameters[2] != "1000" || regenerateRole.Parameters[3] != "0" || regenerateRole.Parameters[4] != "0" || regenerateRole.Parameters[5] != "0" {
		return fmt.Errorf("official self-parameter regenerate contract changed: %+v", *regenerateRole)
	}
	regenerateEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 2}
	regenerateEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Recovery: 4200, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	regenerateEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 5000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	regenerateResults, err := regenerateEngine.executeEnemyRole(&regenerateEngine.enemies[0], 6, *regenerateRole, []CombatSkillRole{*regenerateRole})
	if err != nil {
		return err
	}
	if len(regenerateResults) != 2 || regenerateResults[0].Command != CommandBuff || regenerateResults[0].Args[3] != int64(battleBuffCodes["REGENERATE_BY_SELF_PARAM"]) ||
		regenerateResults[1].Command != CommandBattleParam || !equalBattleArgs(regenerateResults[1].Args, []int64{6, 5000, 10000, 0, 0, 0, 0, 0, 99999, 99999, 99999}) ||
		len(regenerateEngine.enemies[1].Effects) != 1 || regenerateEngine.enemies[1].Effects[0].Value != 4200 || regenerateEngine.enemies[1].Effects[0].Remaining != 4 {
		return fmt.Errorf("official self-parameter regenerate state is results=%+v target=%+v", regenerateResults, regenerateEngine.enemies[1])
	}

	// Native role 56 is current-HP percentage damage, not a hard-coded kill.
	// FUN_00093e53 stores the current-HP p0 segment and the p1*skill-level
	// segment separately; FUN_0008127b divides only the first by 100 and then
	// adds the second. Ordinary enemy skills are managed level 1. The current
	// CN matrix happens to keep p1 at zero, but the runtime owns the complete
	// formula instead of failing on a valid non-zero producer.

	if EnemySkillLevel("DESTRUCT") != 1 {
		return errors.New("ordinary enemy DESTRUCT skill level is not calibrated to one")
	}
	nonlethalDestruct, err := findEnemyRole(49950208, "DESTRUCT", "")
	if err != nil {
		return err
	}
	levelProbe := *nonlethalDestruct
	levelProbe.Parameters[0] = "40"
	levelProbe.Parameters[1] = "25"
	if zero, one := nativeDestructDamage(1000, levelProbe, 0), nativeDestructDamage(1000, levelProbe, 1); zero != 400 || one != 425 {
		return fmt.Errorf("native DESTRUCT level segments are level0=%d level1=%d, want 400/425", zero, one)
	}
	destructEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	destructEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	destructResults, err := destructEngine.executeEnemyRole(&destructEngine.enemies[0], 5, *nonlethalDestruct, []CombatSkillRole{*nonlethalDestruct})
	if err != nil {
		return err
	}
	if len(destructResults) != 1 || destructResults[0].Command != 60 || destructResults[0].Args[2] != -9900 || destructResults[0].Args[3] != 100 || destructEngine.enemies[0].HP != 100 || destructEngine.enemies[0].Broken || destructEngine.enemies[0].DamageTaken != 9900 {
		return fmt.Errorf("official 99-percent DESTRUCT is results=%+v enemy=%+v", destructResults, destructEngine.enemies[0])
	}
	lethalDestruct, err := findEnemyRole(44071005, "DESTRUCT", "")
	if err != nil {
		return err
	}
	lethalEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	lethalEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Drops: []BattleDrop{{EnemyIndex: 0, RewardType: 4, Num: 100}}}
	lethalResults, err := lethalEngine.executeEnemyRole(&lethalEngine.enemies[0], 5, *lethalDestruct, []CombatSkillRole{*lethalDestruct})
	if err != nil {
		return err
	}
	// D-337 / 8127b: immediate 81 has the current drop flag (zero), and
	// leaves the reward available for a later ordinary kill after healing.
	if len(lethalResults) != 2 || lethalResults[0].Command != 60 || lethalResults[0].Args[2] != -10000 || lethalResults[0].Args[3] != 0 || lethalResults[1].Command != CommandEnemyBreak || lethalResults[1].Args[4] != 0 || lethalEngine.enemies[0].HP != 0 || lethalEngine.enemies[0].DropResolved || lethalEngine.enemies[0].DropReleased {
		return fmt.Errorf("official lethal DESTRUCT is results=%+v enemy=%+v", lethalResults, lethalEngine.enemies[0])
	}
	deferredDestruct, err := findEnemyRole(44281317, "DESTRUCT", "")
	if err != nil {
		return err
	}
	deferredEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	deferredEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Drops: []BattleDrop{{EnemyIndex: 0, RewardType: 4, Num: 100}}}
	deferredResults, err := deferredEngine.executeEnemyRole(&deferredEngine.enemies[0], 5, *deferredDestruct, []CombatSkillRole{*deferredDestruct})
	if err != nil {
		return err
	}
	if len(deferredResults) != 1 || deferredResults[0].Command != 60 || !deferredEngine.enemies[0].PendingBreak || deferredEngine.enemies[0].DropResolved || deferredEngine.enemies[0].DropReleased {
		return fmt.Errorf("official deferred DESTRUCT is results=%+v enemy=%+v", deferredResults, deferredEngine.enemies[0])
	}

	// Native role 61 producer FUN_00097aed creates action type 3 with
	// BATTLE_BUFF.ATTR_SEE(107). All four official rows are duration-only and
	// target the full enemy side, so no numeric modifier may be fabricated.
	attributeSeeRole, err := findPlayerRole(13600372, "ATTR_SEE")
	if err != nil {
		return err
	}
	attributeSeeEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 2}
	attributeSeeEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	attributeSeeEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	attributeSeeEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	attributeSeeResults, err := attributeSeeEngine.executePlayerRole(battleAction{memberType: 1, target: 5, cardLevel: 60}, *attributeSeeRole, 1)
	if err != nil {
		return err
	}
	if len(attributeSeeResults) != 4 || attributeSeeResults[0].Command != CommandBuff || attributeSeeResults[0].Args[3] != int64(battleBuffCodes["ATTR_SEE"]) || attributeSeeResults[0].Args[4] != 2 ||
		attributeSeeResults[1].Command != CommandBattleParam || !equalBattleArgs(attributeSeeResults[1].Args, []int64{5, 10000, 10000, 0, 0, 0, 0, 0, 99999, 99999, 99999}) ||
		attributeSeeResults[2].Command != CommandBuff || attributeSeeResults[2].Args[0] != 6 ||
		attributeSeeResults[3].Command != CommandBattleParam || !equalBattleArgs(attributeSeeResults[3].Args, []int64{6, 10000, 10000, 0, 0, 0, 0, 0, 99999, 99999, 99999}) {
		return fmt.Errorf("official ATTR_SEE projection is %+v", attributeSeeResults)
	}
	for index := 0; index < attributeSeeEngine.enemyCount; index++ {
		effects := attributeSeeEngine.enemies[index].Effects
		if len(effects) != 1 || effects[0].Function != "ATTR_SEE" || effects[0].Remaining != 9999 || effects[0].Value != 0 || effects[0].Rate != 0 || effects[0].Kind != 2 {
			return fmt.Errorf("official ATTR_SEE target %d state is %+v", index, effects)
		}
	}
	// Native enemy-trigger action type 14 keeps three distinct subtypes. The
	// two active official families below gate p1-driven bit clear/set, absence
	// of the unrelated ResultCmd94, and selected-enemy awake state.
	flagSetRole, err := findEnemyRole(32801122, "ENEMY_AI_TRIGGER_FLAG_SET", "1")
	if err != nil {
		return err
	}
	flagClearRole, err := findEnemyRole(32801108, "ENEMY_AI_TRIGGER_FLAG_SET", "0")
	if err != nil {
		return err
	}
	awakeRole, err := findEnemyRole(35809501, "ENEMY_AWAKE_FLAG_SET", "")
	if err != nil {
		return err
	}
	forceEndRole, err := findEnemyRole(35809501, "FORCE_BATTLE_END", "")
	if err != nil {
		return err
	}
	triggerEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 2}
	triggerEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 100, MaxHP: 100}
	triggerEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 100, MaxHP: 100}
	triggerResults, err := triggerEngine.executeEnemyRole(&triggerEngine.enemies[0], 6, *flagSetRole, []CombatSkillRole{*flagSetRole})
	if err != nil {
		return err
	}
	if len(triggerResults) != 0 || !triggerEngine.enemies[1].hasAIFlag(1) || triggerEngine.enemies[0].hasAIFlag(1) {
		return fmt.Errorf("official AI trigger-flag set projection/state is results=%+v flags=%b/%b", triggerResults, triggerEngine.enemies[0].AIFlags, triggerEngine.enemies[1].AIFlags)
	}
	triggerResults, err = triggerEngine.executeEnemyRole(&triggerEngine.enemies[0], 6, *flagClearRole, []CombatSkillRole{*flagClearRole})
	if err != nil {
		return err
	}
	if len(triggerResults) != 0 || triggerEngine.enemies[1].hasAIFlag(1) {
		return fmt.Errorf("official AI trigger-flag clear projection/state is results=%+v flags=%b", triggerResults, triggerEngine.enemies[1].AIFlags)
	}
	triggerResults, err = triggerEngine.executeEnemyRole(&triggerEngine.enemies[0], 6, *awakeRole, []CombatSkillRole{*awakeRole})
	if err != nil {
		return err
	}
	if len(triggerResults) != 0 || triggerEngine.enemies[0].Awake != 0 || triggerEngine.enemies[1].Awake != 1 {
		return fmt.Errorf("official selected-enemy awake projection/state is results=%+v enemies=%+v", triggerResults, triggerEngine.enemies)
	}
	triggerResults, err = triggerEngine.executeEnemyRole(&triggerEngine.enemies[0], 6, *forceEndRole, []CombatSkillRole{*awakeRole, *forceEndRole})
	if err != nil {
		return err
	}
	if len(triggerResults) != 0 || !triggerEngine.forceEndCheck || triggerEngine.endType != 0 {
		return fmt.Errorf("official force-end request projection/state is results=%+v force=%v end=%d", triggerResults, triggerEngine.forceEndCheck, triggerEngine.endType)
	}
	triggerEngine.resolveForcedBattleEnd()
	if triggerEngine.forceEndCheck || triggerEngine.endType != 4 {
		return fmt.Errorf("official awake force-end resolution is force=%v end=%d enemies=%+v", triggerEngine.forceEndCheck, triggerEngine.endType, triggerEngine.enemies)
	}
	// Native role 51's producer intentionally creates no action. Role 43 instead
	// owns BATTLE_BUFF.ATTR(202), emits ResultCmd32, replaces an older rewrite,
	// and restores the member's base attribute when the held status expires.
	outputTextRole, err := findEnemyRole(44071001, "OUTPUT_TEXT", "")
	if err != nil {
		return err
	}
	rewriteFireRole, err := findEnemyRole(40000081, "REWRITE", "FIRE")
	if err != nil {
		return err
	}
	rewriteIceRole, err := findEnemyRole(40000082, "REWRITE", "ICE")
	if err != nil {
		return err
	}
	rewriteEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	rewriteEngine.enemies[0] = battleEnemy{MemberType: 5, HP: 100, MaxHP: 100, BaseAttribute: "WIND", Attribute: "WIND"}
	outputResults, err := rewriteEngine.executeEnemyRole(&rewriteEngine.enemies[0], 5, *outputTextRole, []CombatSkillRole{*outputTextRole})
	if err != nil {
		return err
	}
	if len(outputResults) != 0 || rewriteEngine.enemies[0].Attribute != "WIND" {
		return fmt.Errorf("official OUTPUT_TEXT producer is results=%+v enemy=%+v", outputResults, rewriteEngine.enemies[0])
	}
	rewriteResults, err := rewriteEngine.executeEnemyRole(&rewriteEngine.enemies[0], 5, *rewriteFireRole, []CombatSkillRole{*rewriteFireRole})
	if err != nil {
		return err
	}
	if len(rewriteResults) != 3 || rewriteResults[0].Command != CommandRewrite || rewriteResults[0].Args[0] != 5 || rewriteResults[0].Args[1] != 1 || rewriteResults[1].Command != CommandBuff || rewriteResults[2].Command != CommandBattleParam || rewriteEngine.enemies[0].Attribute != "FIRE" || len(rewriteEngine.enemies[0].Effects) != 1 || rewriteEngine.enemies[0].Effects[0].Remaining != 999 {
		return fmt.Errorf("official first attribute rewrite projection/state is results=%+v enemy=%+v", rewriteResults, rewriteEngine.enemies[0])
	}
	rewriteResults, err = rewriteEngine.executeEnemyRole(&rewriteEngine.enemies[0], 5, *rewriteIceRole, []CombatSkillRole{*rewriteIceRole})
	if err != nil {
		return err
	}
	if len(rewriteResults) != 4 || rewriteResults[0].Command != 72 || rewriteResults[1].Command != CommandRewrite || rewriteResults[1].Args[1] != 2 || rewriteResults[2].Command != CommandBuff || rewriteResults[3].Command != CommandBattleParam || rewriteEngine.enemies[0].Attribute != "ICE" || len(rewriteEngine.enemies[0].Effects) != 1 {
		return fmt.Errorf("official replacement attribute rewrite projection/state is results=%+v enemy=%+v", rewriteResults, rewriteEngine.enemies[0])
	}
	rewriteEngine.enemies[0].Effects[0].Remaining = 1
	rewriteEngine.enemies[0].Effects[0].AppliedTurn = 0
	rewriteResults, tickErr = expireForEffectContract(rewriteEngine)
	if tickErr != nil {
		return tickErr
	}
	if len(rewriteResults) != 2 || rewriteResults[0].Command != 72 || rewriteResults[1].Command != CommandRewrite || rewriteResults[1].Args[1] != 3 || rewriteEngine.enemies[0].Attribute != "WIND" || len(rewriteEngine.enemies[0].Effects) != 0 || combatAttributeCode("NULL") != 0 {
		return fmt.Errorf("official expired attribute rewrite projection/state is results=%+v enemy=%+v null=%d", rewriteResults, rewriteEngine.enemies[0], combatAttributeCode("NULL"))
	}
	burstEnemyRole, err := findEnemyRole(38112444, "BURST_GAUGE_QUICK_UP", "")
	if err != nil {
		return err
	}
	burstEnemyEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	burstEnemyEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 100, MaxHP: 100}
	burstEnemyEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 100, MaxHP: 100, Burst: 5, BurstState: burstGaugeNormal}
	burstEnemyResults, err := burstEnemyEngine.executeEnemyRole(&burstEnemyEngine.enemies[0], 1, *burstEnemyRole, []CombatSkillRole{*burstEnemyRole})
	if err != nil {
		return err
	}
	if len(burstEnemyResults) != 2 || burstEnemyResults[0].Command != 207 || burstEnemyResults[0].Args[0] != 1 || burstEnemyResults[0].Args[2] != 30 || burstEnemyResults[0].Args[3] != 1 || burstEnemyResults[1].Command != CommandBurstGaugeState || burstEnemyResults[1].Args[0] != 1 || burstEnemyResults[1].Args[1] != 35 || burstEnemyEngine.players[0].Burst != 35 {
		return fmt.Errorf("official enemy burst-gauge projection/state is results=%+v player=%+v", burstEnemyResults, burstEnemyEngine.players[0])
	}
	turnDamageEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 2}
	turnDamageEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Attack: 1000, TurnDamage: 2000}
	turnDamageEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 10000, MaxHP: 10000, Attack: 1000, TurnDamage: 5000}
	turnDamageEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000, Attack: 1000, Defense: 1000, TurnDamage: 5000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	attackUpByDamage, err := findEnemyRole(44123007, "ATK_UP_BY_NOW_TURN_DAMAGE", "ATK")
	if err != nil {
		return err
	}
	turnDamageResults, err := turnDamageEngine.executeEnemyParameterRole(&turnDamageEngine.enemies[0], 6, *attackUpByDamage)
	if err != nil {
		return err
	}
	if len(turnDamageResults) != 2 || turnDamageEngine.enemies[1].Attack != 1200 || len(turnDamageEngine.enemies[1].Effects) != 1 || turnDamageEngine.enemies[1].Effects[0].Delta != 200 {
		return fmt.Errorf("current-turn-damage attack-up role=%+v state is results=%+v enemy=%+v", *attackUpByDamage, turnDamageResults, turnDamageEngine.enemies[1])
	}
	attackBreakByDamage, err := findEnemyRole(35101216, "ATK_BREAK_BY_NOW_TURN_DAMAGE", "ATK")
	if err != nil {
		return err
	}
	turnDamageEngine.enemies[0].TurnDamage = 2500
	turnDamageResults, err = turnDamageEngine.executeEnemyParameterRole(&turnDamageEngine.enemies[0], 1, *attackBreakByDamage)
	if err != nil {
		return err
	}
	if len(turnDamageResults) != 2 || turnDamageEngine.players[0].Attack != 800 || len(turnDamageEngine.players[0].Effects) != 1 || turnDamageEngine.players[0].Effects[0].Delta != -200 {
		return fmt.Errorf("current-turn-damage attack-break state is results=%+v player=%+v", turnDamageResults, turnDamageEngine.players[0])
	}
	guardBreakByDamage, err := findEnemyRole(44252004, "GUARD_BREAK_BY_NOW_TURN_DAMAGE", "DEF")
	if err != nil {
		return err
	}
	turnDamageEngine.enemies[0].TurnDamage = 2000
	turnDamageResults, err = turnDamageEngine.executeEnemyParameterRole(&turnDamageEngine.enemies[0], 1, *guardBreakByDamage)
	if err != nil {
		return err
	}
	if len(turnDamageResults) != 2 || turnDamageEngine.players[0].Defense != 900 || len(turnDamageEngine.players[0].Effects) != 2 || turnDamageEngine.players[0].Effects[1].Delta != -100 {
		return fmt.Errorf("current-turn-damage guard-break state is results=%+v player=%+v", turnDamageResults, turnDamageEngine.players[0])
	}

	// Target-parameter roles use the selected member's p3 source and carry a
	// positive p7 source cap. This matters for revive follow-ups whose MAX_HP
	// source is much larger than ordinary Arthur parameters.
	targetParameterEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	targetParameterEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 30000000, MaxHP: 30000000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	maxHPUp, err := findEnemyRole(36901105, "ATK_UP_BY_TARGET_PARAM", "MAX_HP")
	if err != nil {
		return err
	}
	targetParameterResults, err := targetParameterEngine.executeEnemyParameterRole(&targetParameterEngine.enemies[0], 5, *maxHPUp)
	if err != nil {
		return err
	}
	// The source20M cap produces4M, then JP948dc/949c2 wraps the neutral
	// int32 multiplication by1000 and clamps its negative magnitude to1.
	// 73e9f adds that committed MAX_HP delta to current HP as well.
	if len(targetParameterResults) != 2 || targetParameterEngine.enemies[0].MaxHP != 30000001 || targetParameterEngine.enemies[0].HP != 30000001 || targetParameterEngine.enemies[0].Effects[0].Delta != 1 {
		return fmt.Errorf("target-parameter capped MAX_HP-up state is results=%+v enemy=%+v", targetParameterResults, targetParameterEngine.enemies[0])
	}
	targetParameterEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000, Recovery: 200000, MDefense: 5000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	mindBreak, err := findEnemyRole(37701102, "ATK_BREAK_BY_TARGET_PARAM", "MND")
	if err != nil {
		return err
	}
	targetParameterResults, err = targetParameterEngine.executeEnemyParameterRole(&targetParameterEngine.enemies[0], 1, *mindBreak)
	if err != nil {
		return err
	}
	if len(targetParameterResults) != 2 || targetParameterEngine.players[0].BaseRecovery != 200000 || targetParameterEngine.players[0].Recovery != 99999 || targetParameterEngine.players[0].Effects[0].Delta != -89999 {
		return fmt.Errorf("target-parameter capped MND-break state is results=%+v player=%+v", targetParameterResults, targetParameterEngine.players[0])
	}
	magicGuardBreak, err := findEnemyRole(37601101, "GUARD_BREAK_BY_TARGET_PARAM", "MDEF")
	if err != nil {
		return err
	}
	targetParameterResults, err = targetParameterEngine.executeEnemyParameterRole(&targetParameterEngine.enemies[0], 1, *magicGuardBreak)
	if err != nil {
		return err
	}
	if len(targetParameterResults) != 2 || targetParameterEngine.players[0].MDefense != 3000 || targetParameterEngine.players[0].Effects[1].Delta != -2000 {
		return fmt.Errorf("target-parameter MDEF-break state is results=%+v player=%+v", targetParameterResults, targetParameterEngine.players[0])
	}
	maxHPHeal, err := findEnemyRole(44002214, "HEAL_BY_TARGET_MAXHP", "")
	if err != nil {
		return err
	}
	targetParameterEngine.enemies[0].HP = 10000000
	targetParameterResults, err = targetParameterEngine.executeEnemyHeal(&targetParameterEngine.enemies[0], 5, *maxHPHeal)
	if err != nil {
		return err
	}
	if len(targetParameterResults) != 1 || targetParameterEngine.enemies[0].HP != 16000000 || targetParameterResults[0].Command != 61 || targetParameterResults[0].Args[2] != 6000000 {
		return fmt.Errorf("target-MAX_HP heal state is results=%+v enemy=%+v", targetParameterResults, targetParameterEngine.enemies[0])
	}

	// Enemy role 11 is not an enemy-only shortcut. Its native producer
	// FUN_000a23f3 selects the concrete action target as the MAX_HP source and
	// stores p0+p1*level, p2*level and the positive p3 source cap independently.
	// EnemyLevelupDataContainer assigns level 1 to every ordinary enemy skill;
	// FUN_0007adb0 projects that same value in ResultCmd50 and dispatches the
	// same skill object to the role producer. Concrete controls below guard
	// against the old enemy-only, level-zero approximation.

	selfTargetMaxHPHeal, err := findEnemyRole(44175004, "HEAL_BY_TARGET_MAXHP", "")
	if err != nil {
		return err
	}
	selfHealEngine := &BattleEngine{catalog: catalog, enemyCount: 2}
	selfHealEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10, MaxHP: 100}
	selfHealEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 20, MaxHP: 200}
	selfHealResults, err := selfHealEngine.executeEnemyHeal(&selfHealEngine.enemies[0], 6, *selfTargetMaxHPHeal)
	if err != nil {
		return err
	}
	if len(selfHealResults) != 1 || selfHealEngine.enemies[0].HP != 40 || selfHealEngine.enemies[1].HP != 20 ||
		!equalBattleArgs(selfHealResults[0].Args, []int64{5, int64(selfTargetMaxHPHeal.RoleIndex), 30, 40}) {
		return fmt.Errorf("official SELF target-MAX_HP heal is results=%+v enemies=%+v", selfHealResults, selfHealEngine.enemies[:2])
	}

	allTargetMaxHPHeal, err := findEnemyRole(44188011, "HEAL_BY_TARGET_MAXHP", "")
	if err != nil {
		return err
	}
	allHealEngine := &BattleEngine{catalog: catalog, enemyCount: 2}
	allHealEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10, MaxHP: 100}
	allHealEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 20, MaxHP: 200}
	allHealResults, err := allHealEngine.executeEnemyHeal(&allHealEngine.enemies[0], 5, *allTargetMaxHPHeal)
	if err != nil {
		return err
	}
	if len(allHealResults) != 2 || allHealEngine.enemies[0].HP != 30 || allHealEngine.enemies[1].HP != 60 ||
		!equalBattleArgs(allHealResults[0].Args, []int64{5, int64(allTargetMaxHPHeal.RoleIndex), 20, 30}) ||
		!equalBattleArgs(allHealResults[1].Args, []int64{6, int64(allTargetMaxHPHeal.RoleIndex), 40, 60}) {
		return fmt.Errorf("official ENEMY_ALL target-MAX_HP heal is results=%+v enemies=%+v", allHealResults, allHealEngine.enemies[:2])
	}

	growthTargetMaxHPHeal, err := findEnemyRole(44094011, "HEAL_BY_TARGET_MAXHP", "")
	if err != nil {
		return err
	}
	growthLevel, ok := catalog.EnemyLevels[40009112]
	if !ok {
		return errors.New("official target-MAX_HP growth enemy level 40009112 is missing")
	}
	var growthAction CombatEnemyAction
	for _, action := range growthLevel.Actions {
		if action.SkillID == 44094011 {
			growthAction = action
			break
		}
	}
	if growthLevel.HPBars != 1 || growthLevel.ActionsPerTurn != 2 || growthAction.Slot != 8 || growthAction.Category != "skill" ||
		growthAction.AIConditionID != 40093011 || growthAction.Priority != 9 || growthAction.Target != "RANDOM" ||
		growthAction.ActionCost != 1 || growthAction.MaxUses != 1000 || growthAction.Rate != 100 || growthAction.CountOnMiss {
		return fmt.Errorf("official target-MAX_HP growth action changed: level=%+v action=%+v", growthLevel, growthAction)
	}
	growthHealEngine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), enemyCount: 1}
	for index := range growthHealEngine.players {
		growthHealEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 1000, MaxHP: 1000}
	}
	growthHealEngine.players[3].HP = 10
	growthHealEngine.players[3].MaxHP = 100
	growthHealEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000, Level: growthLevel}
	growthHealResults, err := growthHealEngine.executeEnemyActionCandidate(&growthHealEngine.enemies[0], enemyActionCandidate{action: growthAction})
	if err != nil {
		return err
	}
	if targetHPHealRoleValue(*growthTargetMaxHPHeal, 0, 1, 100) != 3000 || targetHPHealRoleValue(*growthTargetMaxHPHeal, 1, 1, 100) != 3101 ||
		len(growthHealResults) != 2 || growthHealEngine.players[3].HP != 10 || growthHealEngine.enemies[0].HP != 1000 ||
		growthHealEngine.players[0].HP != 1000 || growthHealEngine.players[1].HP != 1000 || growthHealEngine.players[2].HP != 1000 ||
		!equalBattleArgs(growthHealResults[0].Args, []int64{5, 44094011, 4, 1, 4, 44094011, 0}) ||
		// The random AI member remains in row50 but ENEMY_ALL/SELECT heals
		// the enemy side, using each actual recipient's MaxHP.
		!equalBattleArgs(growthHealResults[1].Args, []int64{5, int64(growthTargetMaxHPHeal.RoleIndex), 31001, 1000}) {
		return fmt.Errorf("official target-MAX_HP level-one action is results=%+v players=%+v", growthHealResults, growthHealEngine.players)
	}

	// Native HP_CUT consumer FUN_000818d8 adds 0.9 then truncates,
	// caps it at current HP-1 and emits the signed negative delta in command 64.
	if delta, hp := nativeHPCut(102, 25); delta != -26 || hp != 76 {
		return fmt.Errorf("native rounded HP cut is delta=%d hp=%d, want -26/76", delta, hp)
	}
	if delta, hp := nativeHPCut(7, 100); delta != -6 || hp != 1 {
		return fmt.Errorf("native nonlethal HP cut is delta=%d hp=%d, want -6/1", delta, hp)
	}

	playerHPCutRole, err := findPlayerRole(14401334, "HP_CUT")
	if err != nil {
		return err
	}
	if playerHPCutRole.Target != "SELF" || playerHPCutRole.Parameters[0] != "25" {
		return fmt.Errorf("official player SELF HP_CUT changed: %+v", playerHPCutRole)
	}
	hpCutEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	hpCutEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 102, MaxHP: 102}
	hpCutEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 102, MaxHP: 102}
	hpCutResults, err := hpCutEngine.executePlayerHPCut(battleAction{memberType: 1, target: 5}, *playerHPCutRole)
	if err != nil {
		return err
	}
	if len(hpCutResults) != 1 || hpCutEngine.players[0].HP != 76 || hpCutEngine.enemies[0].HP != 102 || hpCutResults[0].Command != 64 || hpCutResults[0].Args[1] != int64(playerHPCutRole.RoleIndex) || hpCutResults[0].Args[2] != -26 || hpCutResults[0].Args[3] != 76 {
		return fmt.Errorf("player SELF HP cut state is results=%+v player=%+v enemy=%+v", hpCutResults, hpCutEngine.players[0], hpCutEngine.enemies[0])
	}

	userOneHPCutRole, err := findEnemyRole(32001114, "HP_CUT", "")
	if err != nil {
		return err
	}
	if userOneHPCutRole.Target != "SELECT" || userOneHPCutRole.Parameters[0] != "20" || len(catalog.Skills[32001114]) == 0 || catalog.Skills[32001114][0].Target != "USER_ONE" {
		return fmt.Errorf("official enemy USER_ONE HP_CUT changed: role=%+v skills=%+v", userOneHPCutRole, catalog.Skills[32001114])
	}
	userOneEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	userOneEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 102, MaxHP: 102}
	userOneEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 102, MaxHP: 102}
	userOneResults, err := userOneEngine.executeEnemyHPCut(&userOneEngine.enemies[0], 1, *userOneHPCutRole)
	if err != nil {
		return err
	}
	if len(userOneResults) != 1 || userOneEngine.players[0].HP != 81 || userOneEngine.enemies[0].HP != 102 ||
		userOneResults[0].Command != 64 || userOneResults[0].Args[0] != 1 || userOneResults[0].Args[1] != int64(userOneHPCutRole.RoleIndex) || userOneResults[0].Args[2] != -21 || userOneResults[0].Args[3] != 81 {
		return fmt.Errorf("official enemy USER_ONE HP_CUT is results=%+v player=%+v enemy=%+v", userOneResults, userOneEngine.players[0], userOneEngine.enemies[0])
	}

	selfHPCutRole, err := findEnemyRole(37601120, "HP_CUT", "")
	if err != nil {
		return err
	}
	if selfHPCutRole.Target != "SELECT" || selfHPCutRole.Parameters[0] != "10" || len(catalog.Skills[37601120]) == 0 || catalog.Skills[37601120][0].Target != "SELF" {
		return fmt.Errorf("official enemy SELF HP_CUT changed: role=%+v skills=%+v", selfHPCutRole, catalog.Skills[37601120])
	}
	selfEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	selfEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 102, MaxHP: 102}
	selfEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 102, MaxHP: 102}
	selfResults, err := selfEngine.executeEnemyHPCut(&selfEngine.enemies[0], 5, *selfHPCutRole)
	if err != nil {
		return err
	}
	if len(selfResults) != 1 || selfEngine.players[0].HP != 102 || selfEngine.enemies[0].HP != 91 ||
		selfResults[0].Command != 64 || selfResults[0].Args[0] != 5 || selfResults[0].Args[1] != int64(selfHPCutRole.RoleIndex) || selfResults[0].Args[2] != -11 || selfResults[0].Args[3] != 91 {
		return fmt.Errorf("official enemy SELF HP_CUT is results=%+v player=%+v enemy=%+v", selfResults, selfEngine.players[0], selfEngine.enemies[0])
	}

	userAllHPCutRole, err := findEnemyRole(38301104, "HP_CUT", "")
	if err != nil {
		return err
	}
	if userAllHPCutRole.Target != "FRIEND_ALL" || userAllHPCutRole.Parameters[0] != "40" || len(catalog.Skills[38301104]) == 0 || catalog.Skills[38301104][0].Target != "USER_ALL" {
		return fmt.Errorf("official enemy USER_ALL HP_CUT changed: role=%+v skills=%+v", userAllHPCutRole, catalog.Skills[38301104])
	}
	userAllEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	for index := range userAllEngine.players {
		userAllEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 101 + index, MaxHP: 101 + index}
	}
	userAllEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 102, MaxHP: 102}
	userAllResults, err := userAllEngine.executeEnemyHPCut(&userAllEngine.enemies[0], 0, *userAllHPCutRole)
	if err != nil {
		return err
	}
	wantUserAllHP := [4]int{60, 61, 61, 62}
	if len(userAllResults) != 4 || userAllEngine.enemies[0].HP != 102 {
		return fmt.Errorf("official enemy USER_ALL HP_CUT shape is results=%+v enemy=%+v", userAllResults, userAllEngine.enemies[0])
	}
	for index := range userAllEngine.players {
		result := userAllResults[index]
		if userAllEngine.players[index].HP != wantUserAllHP[index] || result.Command != 64 || result.Args[0] != int64(index+1) || result.Args[1] != int64(userAllHPCutRole.RoleIndex) || result.Args[3] != int64(wantUserAllHP[index]) {
			return fmt.Errorf("official enemy USER_ALL HP_CUT member %d is result=%+v player=%+v", index+1, result, userAllEngine.players[index])
		}
	}

	nonlethalHPCutRole, err := findEnemyRole(32401102, "HP_CUT", "")
	if err != nil {
		return err
	}
	nonlethalEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	nonlethalEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 7, MaxHP: 7}
	nonlethalEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 7, MaxHP: 7}
	nonlethalResults, err := nonlethalEngine.executeEnemyHPCut(&nonlethalEngine.enemies[0], 1, *nonlethalHPCutRole)
	if err != nil {
		return err
	}
	if len(nonlethalResults) != 1 || nonlethalEngine.players[0].HP != 1 || nonlethalEngine.enemies[0].HP != 7 || nonlethalResults[0].Args[2] != -6 || nonlethalResults[0].Args[3] != 1 {
		return fmt.Errorf("official enemy 100-percent nonlethal HP_CUT is results=%+v player=%+v enemy=%+v", nonlethalResults, nonlethalEngine.players[0], nonlethalEngine.enemies[0])
	}

	// FUN_00073e9f/FUN_000722ee clamp positive overheal in HP while command 61
	// retains the requested amount. FUN_000452d0 sums HEAL_REVERSE rates up to
	// 100 percent and turns the heal into a nonlethal negative delta.
	officialReverseRole, err := findEnemyRole(33801618, "HEAL_REVERSE", "")
	if err != nil {
		return err
	}
	if officialReverseRole.Target != "SELECT" || officialReverseRole.Parameters[0] != "2" ||
		officialReverseRole.Parameters[1] != "100" || officialReverseRole.Parameters[2] != "0" {
		return fmt.Errorf("official HEAL_REVERSE role changed: %+v", officialReverseRole)
	}
	officialReverseEngine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	for index := range officialReverseEngine.players {
		officialReverseEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 30000, MaxHP: 50000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	officialReverseEngine.players[0].Recovery = 10000
	officialReverseEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	reverseApply, err := officialReverseEngine.executeEnemyPersistentEffect(&officialReverseEngine.enemies[0], 1, *officialReverseRole)
	if err != nil {
		return err
	}
	if len(reverseApply) != 1 || reverseApply[0].Command != CommandBuff || len(reverseApply[0].Args) != 11 ||
		reverseApply[0].Args[0] != 1 || reverseApply[0].Args[3] != int64(battleBuffCodes["HEAL_REVERSE"]) ||
		reverseApply[0].Args[7] != 0 || len(officialReverseEngine.players[0].Effects) != 1 ||
		officialReverseEngine.players[0].Effects[0].Rate != 100 || officialReverseEngine.players[0].Effects[0].Remaining != 2 ||
		officialReverseEngine.players[0].Effects[0].Source != 5 || officialReverseEngine.players[0].Effects[0].Kind != 2 {
		return fmt.Errorf("official HEAL_REVERSE application is results=%+v effects=%+v", reverseApply, officialReverseEngine.players[0].Effects)
	}
	officialHealValue := fixedHealRoleValue(*fixedHealRole, 60, 4, officialReverseEngine.players[0].Recovery)
	officialReverseHeal, err := officialReverseEngine.executeFixedHeal(battleAction{memberType: 1, target: 1, cardLevel: 60}, *fixedHealRole, 4)
	if err != nil {
		return err
	}
	if officialHealValue != 30563 || len(officialReverseHeal) != maxRoomMembers ||
		officialReverseHeal[0].Command != 61 || officialReverseHeal[0].Args[2] != -29999 ||
		officialReverseHeal[0].Args[3] != 1 || officialReverseEngine.players[0].HP != 1 ||
		officialReverseEngine.players[1].HP != 50000 || officialReverseEngine.players[2].HP != 50000 ||
		officialReverseEngine.players[3].HP != 50000 || officialReverseEngine.turnStats.Heal != 91689 {
		return fmt.Errorf("official HEAL_REVERSE/full-party HEAL_FIXED is value=%d results=%+v players=%+v stats=%+v", officialHealValue, officialReverseHeal, officialReverseEngine.players, officialReverseEngine.turnStats)
	}
	if delta, hp := nativeHealCommit(90, 100, 40, nil, true); delta != 40 || hp != 100 {
		return fmt.Errorf("native overheal commit is delta=%d hp=%d, want 40/100", delta, hp)
	}
	reverseEffects := []battleEffect{
		{Function: "HEAL_REVERSE", Rate: 60, Remaining: 2},
		{Function: "HEAL_REVERSE", Rate: 70, Remaining: 2},
	}
	if delta, hp := nativeHealCommit(80, 100, 100, reverseEffects, true); delta != -79 || hp != 1 {
		return fmt.Errorf("native reversed heal commit is delta=%d hp=%d, want -79/1", delta, hp)
	}
	healEngine := &BattleEngine{catalog: catalog, turn: 1}
	healEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 80, MaxHP: 100, Effects: reverseEffects}
	healResults, err := healEngine.healPlayerTargets(battleAction{memberType: 1, target: 1}, CombatSkillRole{RoleIndex: 4, Target: "SELECT"}, 100)
	if err != nil {
		return err
	}
	if len(healResults) != 1 || healEngine.players[0].HP != 1 || healEngine.turnStats.Heal != 0 || healResults[0].Command != 61 || healResults[0].Args[2] != -79 || healResults[0].Args[3] != 1 {
		return fmt.Errorf("reversed direct-heal state is results=%+v player=%+v stats=%+v", healResults, healEngine.players[0], healEngine.turnStats)
	}
	healEngine.turn = 2
	healEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 50, MaxHP: 100, Effects: []battleEffect{
		{Function: "REGENERATE_FIXED", Value: 20, RoleIndex: 5, Remaining: 2, AppliedTurn: 1},
		{Function: "HEAL_REVERSE", Rate: 50, Remaining: 2, AppliedTurn: 1},
	}}
	regenResults, tickErr := expireForEffectContract(healEngine)
	if tickErr != nil {
		return tickErr
	}
	regenResults = append(regenResults, healEngine.regenerateMembers()...)
	if len(regenResults) != 2 || regenResults[0].Command != 53 || healEngine.players[0].HP != 40 || regenResults[1].Command != 61 || regenResults[1].Args[2] != -10 || regenResults[1].Args[3] != 40 {
		return fmt.Errorf("reversed regeneration state is results=%+v player=%+v", regenResults, healEngine.players[0])
	}

	// FUN_000838d0 treats attack p7 and CRITICAL_UP/DOWN as tenths of one
	// percent, clamps their signed sum to +/-1000 and rolls once per hit.
	criticalEngine := &BattleEngine{rng: newXorShift128(1)}
	if flag, multiplier := criticalEngine.nativeCriticalOutcome(nil, 1000); flag != 1 || multiplier != 150 {
		return fmt.Errorf("forced positive critical is flag=%d multiplier=%d, want 1/150", flag, multiplier)
	}
	criticalEngine.rng = newXorShift128(1)
	if flag, multiplier := criticalEngine.nativeCriticalOutcome([]battleEffect{{Function: "CRITICAL_DOWN", Rate: 1000, Remaining: 1}}, 0); flag != -1 || multiplier != 50 {
		return fmt.Errorf("forced negative critical is flag=%d multiplier=%d, want -1/50", flag, multiplier)
	}
	criticalRole := CombatSkillRole{Function: "CRITICAL_UP", ChainRate: 20}
	criticalRole.Parameters[1] = "100"
	criticalRole.Parameters[2] = "1"
	if value := persistentRoleValue(criticalRole, 60, &battlePlayer{}, 2); value != 192 {
		return fmt.Errorf("level/chain critical-up value is %d, want 192", value)
	}
	criticalEffect := persistentBattleEffect(criticalRole, 192, 2, 1, 1, 1, 60)
	if criticalEffect.Rate != 192 || criticalEffect.Parameters[0] != 19 {
		return fmt.Errorf("critical-up durable/projection value is %+v, want rate 192 display 19", criticalEffect)
	}

	// FUN_00096ba7 keeps weakness as a per-mille internal value and exposes
	// value/10 to ResultCmd62; 927b0 uses the full value before the separate
	// elemental multiplier. That display value is not an attribute-rate delta.
	weakPlayer := &battlePlayer{Effects: []battleEffect{{Function: "WEAKNESS", Rate: 375, Remaining: 1}}}
	if rate := playerAttributeRateWithEffects(weakPlayer, "FIRE"); rate != 100 || weaknessAdjustedPower(1000, weakPlayer.Effects) != 1375 {
		return fmt.Errorf("weakness changed elemental rate or lost per-mille precision: rate %d", rate)
	}

	// FUN_00098adb computes fixed enchant damage as
	// ((p3+p4*level)*p2)/1000+p5*level and applies the common chain scale.
	enchantRole := CombatSkillRole{Function: "ENCHANT", ChainRate: 20}
	enchantRole.Parameters[1] = "563"
	enchantRole.Parameters[2] = "1000"
	enchantRole.Parameters[3] = "0"
	enchantRole.Parameters[4] = "50"
	enchantRole.Parameters[5] = "LIGHT"
	if value := persistentRoleValue(enchantRole, 60, &battlePlayer{}, 1); value != 3563 {
		return fmt.Errorf("native enchant value is %d, want 3563", value)
	}
	enchantEffect := persistentBattleEffect(enchantRole, 3563, 2, 1, 1, 1, 60)
	if value, attribute, _ := activeEnchantEffect([]battleEffect{enchantEffect, {Function: "ENCHANT", Value: 437, Attribute: "DARK", Remaining: 1}}); value != 4000 || attribute != "DARK" {
		return fmt.Errorf("summed enchant is value=%d attribute=%s, want 4000/DARK", value, attribute)
	}
	damageResult := battleDamageResult(5, 7, -4000, 10000, 0, "DARK", 120, 0, 1, 1)
	if len(damageResult.Args) != 10 || damageResult.Args[1] != 7 || damageResult.Args[7] != 0 || damageResult.Args[8] != 1 || damageResult.Args[9] != 1 {
		return fmt.Errorf("enchant ResultCmd60 projection is %+v", damageResult)
	}

	// The native attack loop emits every ResultCmd60 against the same HP and
	// commits their sum once. A lethal first hit therefore still emits the
	// second hit, followed by exactly one ResultCmd3 HP projection.
	multiHitEngine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), enemyCount: 1}
	multiHitEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 1, HP: 100, MaxHP: 100}
	multiHitEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 50, MaxHP: 50}
	multiHitRole := CombatSkillRole{RoleIndex: 9, Function: "ATTACK_AA", Target: "SELECT"}
	multiHitRole.Parameters[0] = "100"
	multiHitRole.Parameters[4] = "2"
	multiHitRole.Parameters[7] = "FIRE"
	multiHitRole.Parameters[8] = "PHYSICS"
	multiHitResults, err := multiHitEngine.executePlayerAttack(battleAction{memberType: 1, target: 5}, multiHitRole, 1)
	if err != nil {
		return err
	}
	damageCommands := 0
	hpCommands := 0
	for _, result := range multiHitResults {
		switch result.Command {
		case 60:
			damageCommands++
			if len(result.Args) != 10 || result.Args[3] != 50 {
				return fmt.Errorf("multi-hit ResultCmd60 is %+v, want pre-commit HP 50", result)
			}
		case CommandHP:
			if len(result.Args) > 0 && result.Args[0] == 5 {
				hpCommands++
			}
		}
	}
	if damageCommands != 2 || hpCommands != 1 || multiHitEngine.enemies[0].HP != 0 || multiHitEngine.enemies[0].DamageTaken != 200 {
		return fmt.Errorf("native multi-hit commit is damage_cmds=%d hp_cmds=%d enemy=%+v", damageCommands, hpCommands, multiHitEngine.enemies[0])
	}

	// COVERING producer FUN_00096165 stores a level/chain-scaled per-mille
	// reduction and projects one-tenth of that value. Its common consumer
	// multiplies the remaining ratios of every matching cover effect.
	coverRole := CombatSkillRole{Function: "COVERING", ChainRate: 20}
	coverRole.Parameters[0] = "2"
	coverRole.Parameters[1] = "500"
	coverRole.Parameters[2] = "5"
	coverRole.Parameters[3] = "NULL"
	coverRole.Parameters[4] = "ALL"
	coverValue := persistentRoleValue(coverRole, 60, &battlePlayer{}, 2)
	coverEffect := persistentBattleEffect(coverRole, coverValue, 2, 1, 1, 2, 60)
	if coverValue != 960 || coverEffect.Rate != 960 || coverEffect.Parameters[0] != 96 || coverEffect.Attribute != "NULL" || coverEffect.DamageKind != "ALL" {
		return fmt.Errorf("covering durable/projection state is %+v value=%d", coverEffect, coverValue)
	}
	coverTarget := &battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 2, HP: 100, MaxHP: 100, Effects: []battleEffect{
		{Function: "COVERING", Rate: 500, Attribute: "NULL", DamageKind: "ALL", Remaining: 2},
		{Function: "COVERING", Rate: 200, Attribute: "FIRE", DamageKind: "PHYSICS", Remaining: 2},
		{Function: "COVERING", Rate: 900, Attribute: "ICE", DamageKind: "PHYSICS", Remaining: 2},
	}}
	if damage := playerCoveringDamage(coverTarget, 1000, "FIRE", "PHYSICS"); damage != 400 {
		return fmt.Errorf("multiplicative covering damage is %d, want 400", damage)
	}
	coverTargetEngine := &BattleEngine{rng: newXorShift128(1)}
	for index := range coverTargetEngine.players {
		coverTargetEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, ArthurType: index + 1, HP: 100, MaxHP: 100}
	}
	coverTargetEngine.players[1].Effects = []battleEffect{{Function: "COVERING", Rate: 500, Remaining: 2}}
	if target, ok := coverTargetEngine.selectEnemyActionTarget(&battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5}, CombatEnemyAction{Target: "MERCENARY"}); !ok || target != 2 {
		return fmt.Errorf("covering target restriction is target=%d ok=%t, want member 2", target, ok)
	}

	// Upgrade the synthetic COVERING producer/selector checks to an official
	// application and complete enemy attack using the NULL/ALL filter.
	// A single living cover owner redirects a concrete
	// single-user target with one xor128 draw, then its per-mille cut is
	// applied to the redirected owner's damage and HP/stat lifecycle.
	officialCoverRole, err := findPlayerRole(12101022, "COVERING")
	if err != nil {
		return err
	}
	coverEnemyAttack, err := findEnemyRole(32601107, "ATTACK_AA", "")
	if err != nil {
		return err
	}
	if officialCoverRole.Target != "SELF" || officialCoverRole.Parameters[0] != "2" ||
		officialCoverRole.Parameters[1] != "200" || officialCoverRole.Parameters[2] != "0" ||
		officialCoverRole.Parameters[3] != "NULL" || officialCoverRole.Parameters[4] != "ALL" {
		return fmt.Errorf("official COVERING role changed: %+v", officialCoverRole)
	}
	coverEngine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range coverEngine.players {
		coverEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, ArthurType: index + 1, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	coverEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, Magic: 3500, HP: 10000, MaxHP: 10000}
	coverApply, err := coverEngine.executePlayerRole(
		battleAction{memberType: 1, target: 1, cardLevel: 60, roles: []CombatSkillRole{*officialCoverRole}},
		*officialCoverRole, 1,
	)
	if err != nil {
		return err
	}
	if len(coverApply) != 2 || coverApply[0].Command != CommandBuff || len(coverApply[0].Args) != 11 ||
		coverApply[1].Command != CommandBattleParam || !equalBattleArgs(coverApply[1].Args, []int64{1, 10000, 10000, 0, 0, 0, 0, 0, 99999, 99999, 99999}) ||
		coverApply[0].Args[0] != 1 || coverApply[0].Args[3] != int64(battleBuffCodes["COVERING"]) ||
		coverApply[0].Args[4] != 4 || coverApply[0].Args[7] != 0 || len(coverEngine.players[0].Effects) != 1 ||
		coverEngine.players[0].Effects[0].Function != "COVERING" || coverEngine.players[0].Effects[0].Kind != 1 ||
		coverEngine.players[0].Effects[0].Rate != 200 || coverEngine.players[0].Effects[0].Remaining != 2 ||
		coverEngine.players[0].Effects[0].Source != 1 || coverEngine.players[0].Effects[0].Attribute != "NULL" ||
		coverEngine.players[0].Effects[0].DamageKind != "ALL" {
		return fmt.Errorf("official COVERING application is results=%+v player=%+v", coverApply, coverEngine.players[0])
	}
	redirected, ok := coverEngine.selectEnemyActionTarget(
		&coverEngine.enemies[0], CombatEnemyAction{Target: "MILLIONAIRE"},
	)
	if !ok || redirected != 1 {
		return fmt.Errorf("official single-owner COVERING redirect is target=%d ok=%t, want member 1", redirected, ok)
	}
	rngControl := newXorShift128(1)
	// 4f330 consumes once for the profession selector; 3d224 consumes once
	// more when redirecting from that non-cover owner, even to a sole owner.
	rngControl.next()
	rngControl.next()
	if next, expected := coverEngine.rng.next(), rngControl.next(); next != expected {
		return fmt.Errorf("official profession selector/COVERING RNG: next=%d, want %d", next, expected)
	}
	coverDamage, err := coverEngine.executeEnemyAttack(
		&coverEngine.enemies[0], redirected, *coverEnemyAttack, []CombatSkillRole{*coverEnemyAttack},
	)
	if err != nil {
		return err
	}
	if len(coverDamage) != 2 || coverDamage[0].Command != 60 || coverDamage[0].Args[0] != 1 ||
		coverDamage[0].Args[2] != -2800 || coverDamage[0].Args[3] != 10000 || coverDamage[0].Args[9] != 5 ||
		coverDamage[1].Command != CommandHP || coverEngine.players[0].HP != 7200 || coverEngine.players[1].HP != 10000 {
		return fmt.Errorf("official COVERING redirected attack is results=%+v players=%+v", coverDamage, coverEngine.players)
	}

	// Native role 94 uses action type 5/BATTLE_BUFF 311. Direction comes from
	// that BAD_STATUS identity even when an official player card targets SELF;
	// p0 duration and p1 blocked cost remain independent. TurnPhase ticks the
	// retained state before projecting current COST/COST_BLOCK.
	playerCostRole, err := findPlayerRole(11104652, "COST_BLOCK")
	if err != nil {
		return err
	}
	if playerCostRole.Target != "SELF" || playerCostRole.Parameters[0] != "2" || playerCostRole.Parameters[1] != "2" {
		return fmt.Errorf("official player COST_BLOCK role changed: %+v", playerCostRole)
	}
	playerCostEngine := &BattleEngine{catalog: catalog, turn: 1, costInitial: 3, phase: PhaseUserAttack, enemyCount: 1}
	for index := range playerCostEngine.players {
		playerCostEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	playerCostEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	playerCostApply, err := playerCostEngine.executePersistentEffect(battleAction{memberType: 1, target: 1, cardLevel: 60}, *playerCostRole, 60, 1)
	if err != nil {
		return err
	}
	if len(playerCostApply) != 1 || playerCostApply[0].Command != CommandBuff || len(playerCostApply[0].Args) != 11 ||
		playerCostApply[0].Args[0] != 1 || playerCostApply[0].Args[3] != int64(battleBuffCodes["COST_BLOCK"]) ||
		playerCostApply[0].Args[4] != 5 || playerCostApply[0].Args[7] != 0 || len(playerCostEngine.players[0].Effects) != 1 ||
		playerCostEngine.players[0].Effects[0].Kind != 2 || playerCostEngine.players[0].Effects[0].Value != 2 ||
		playerCostEngine.players[0].Effects[0].Remaining != 2 || playerCostEngine.players[0].Effects[0].Source != 1 {
		return fmt.Errorf("official player SELF COST_BLOCK application is results=%+v effects=%+v", playerCostApply, playerCostEngine.players[0].Effects)
	}
	playerCostEngine.phase = PhaseEnemy
	blockedTurn, err := nextTurnForBattleContract(playerCostEngine)
	if err != nil {
		return err
	}
	blockedCost, blockedValue := -1, -1
	for _, result := range blockedTurn {
		if len(result.Args) < 2 || result.Args[0] != 1 {
			continue
		}
		if result.Command == CommandCost {
			blockedCost = int(result.Args[1])
		}
		if result.Command == CommandCostBlock {
			blockedValue = int(result.Args[1])
		}
	}
	if playerCostEngine.turn != 2 || blockedCost != 4 || blockedValue != 2 || playerCostEngine.players[0].Cost != 2 || len(playerCostEngine.players[0].Effects) != 1 || playerCostEngine.players[0].Effects[0].Remaining != 1 {
		return fmt.Errorf("official player COST_BLOCK turn 2 is cost=%d blocked=%d results=%+v effects=%+v", blockedCost, blockedValue, blockedTurn, playerCostEngine.players[0].Effects)
	}
	playerCostEngine.phase = PhaseEnemy
	releasedTurn, err := nextTurnForBattleContract(playerCostEngine)
	if err != nil {
		return err
	}
	releasedCost, releasedValue, releasedStatus := -1, -1, false
	for _, result := range releasedTurn {
		if result.Command == 72 && len(result.Args) >= 3 && result.Args[0] == 1 && result.Args[2] == int64(battleBuffCodes["COST_BLOCK"]) {
			releasedStatus = true
		}
		if len(result.Args) < 2 || result.Args[0] != 1 {
			continue
		}
		if result.Command == CommandCost {
			releasedCost = int(result.Args[1])
		}
		if result.Command == CommandCostBlock {
			releasedValue = int(result.Args[1])
		}
	}
	if playerCostEngine.turn != 3 || !releasedStatus || releasedCost != 5 || releasedValue != 0 || len(playerCostEngine.players[0].Effects) != 0 {
		return fmt.Errorf("official player COST_BLOCK expiry is cost=%d blocked=%d released=%t results=%+v effects=%+v", releasedCost, releasedValue, releasedStatus, releasedTurn, playerCostEngine.players[0].Effects)
	}
	// Capping before blocking is observable after the eighth turn as well.
	playerCostEngine.turn, playerCostEngine.phase = 9, PhaseEnemy
	if _, err := playerCostEngine.executePersistentEffect(battleAction{memberType: 1, target: 1, cardLevel: 60}, *playerCostRole, 60, 1); err != nil {
		return err
	}
	if _, err := nextTurnForBattleContract(playerCostEngine); err != nil {
		return err
	}
	if playerCostEngine.turnCost() != 10 || playerCostEngine.players[0].Cost != 8 {
		return fmt.Errorf("late-turn cost block bypasses 10C cap: base=%d usable=%d", playerCostEngine.turnCost(), playerCostEngine.players[0].Cost)
	}

	enemyCostRole, err := findEnemyRole(44264302, "COST_BLOCK", "")
	if err != nil {
		return err
	}
	if enemyCostRole.Target != "FRIEND_ALL" || enemyCostRole.Parameters[0] != "99" || enemyCostRole.Parameters[1] != "5" {
		return fmt.Errorf("official enemy COST_BLOCK role changed: %+v", enemyCostRole)
	}
	enemyCostEngine := &BattleEngine{catalog: catalog, turn: 1}
	for index := range enemyCostEngine.players {
		enemyCostEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	enemyCostActor := &battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	enemyCostApply, err := enemyCostEngine.executeEnemyPersistentEffect(enemyCostActor, 1, *enemyCostRole)
	if err != nil {
		return err
	}
	if len(enemyCostApply) != maxRoomMembers {
		return fmt.Errorf("official enemy FRIEND_ALL COST_BLOCK results are %+v", enemyCostApply)
	}
	for index := range enemyCostEngine.players {
		effects := enemyCostEngine.players[index].Effects
		if enemyCostApply[index].Command != CommandBuff || enemyCostApply[index].Args[0] != int64(index+1) ||
			enemyCostApply[index].Args[3] != int64(battleBuffCodes["COST_BLOCK"]) || enemyCostApply[index].Args[4] != 5 ||
			enemyCostApply[index].Args[7] != 0 || len(effects) != 1 || effects[0].Kind != 2 || effects[0].Value != 5 ||
			effects[0].Remaining != 99 || effects[0].Source != 5 {
			return fmt.Errorf("official enemy FRIEND_ALL COST_BLOCK target %d is result=%+v effects=%+v", index+1, enemyCostApply[index], effects)
		}
	}

	// Native role 75 stores WEAKNESS in per-mille units, exposes value/10 in
	// ResultCmd62 but multiplies ordinary attack power independently of the
	// elemental rate. Bind both producer sides to official rows and wire fields.
	playerWeaknessRole, err := findPlayerRole(13201642, "WEAKNESS")
	if err != nil {
		return err
	}
	if playerWeaknessRole.Target != "SELECT" || playerWeaknessRole.Parameters[0] != "2" ||
		playerWeaknessRole.Parameters[1] != "100" || playerWeaknessRole.Parameters[2] != "0" {
		return fmt.Errorf("official player WEAKNESS role changed: %+v", playerWeaknessRole)
	}
	newPlayerWeaknessEngine := func() *BattleEngine {
		engine := &BattleEngine{catalog: catalog, rng: newXorShift128(3), turn: 1, enemyCount: 1}
		engine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, Attack: 1000, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		for index := 1; index < maxRoomMembers; index++ {
			engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1}
		}
		engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 20000, MaxHP: 20000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		engine.enemies[0].Level.AttributeRates[combatAttributeIndex("ICE")] = 100
		return engine
	}
	playerWeaknessEngine := newPlayerWeaknessEngine()
	playerWeaknessApply, err := playerWeaknessEngine.executePlayerRole(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*playerWeaknessRole}},
		*playerWeaknessRole, 1,
	)
	if err != nil {
		return err
	}
	if len(playerWeaknessApply) != 2 || playerWeaknessApply[0].Command != CommandBuff || len(playerWeaknessApply[0].Args) != 11 ||
		playerWeaknessApply[1].Command != CommandBattleParam || !equalBattleArgs(playerWeaknessApply[1].Args, []int64{5, 20000, 20000, 0, 0, 0, 0, 0, 99999, 99999, 99999}) ||
		playerWeaknessApply[0].Args[0] != 5 || playerWeaknessApply[0].Args[3] != int64(battleBuffCodes["WEAKNESS"]) ||
		playerWeaknessApply[0].Args[4] != 5 || playerWeaknessApply[0].Args[7] != 0 ||
		len(playerWeaknessEngine.enemies[0].Effects) != 1 || playerWeaknessEngine.enemies[0].Effects[0].Function != "WEAKNESS" ||
		playerWeaknessEngine.enemies[0].Effects[0].Kind != 2 || playerWeaknessEngine.enemies[0].Effects[0].Rate != 100 ||
		playerWeaknessEngine.enemies[0].Effects[0].Remaining != 2 || playerWeaknessEngine.enemies[0].Effects[0].Source != 1 {
		return fmt.Errorf("official player WEAKNESS application is results=%+v enemy=%+v", playerWeaknessApply, playerWeaknessEngine.enemies[0])
	}
	playerWeaknessDamage, err := playerWeaknessEngine.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if err != nil {
		return err
	}
	if len(playerWeaknessDamage) != 2 || playerWeaknessDamage[0].Command != 60 ||
		playerWeaknessDamage[0].Args[2] != -6337 || playerWeaknessDamage[0].Args[6] != 100 ||
		playerWeaknessDamage[0].Args[7] != 0 || playerWeaknessEngine.enemies[0].HP != 13663 ||
		playerWeaknessDamage[1].Command != CommandHP {
		return fmt.Errorf("official player WEAKNESS attack is results=%+v enemy=%+v", playerWeaknessDamage, playerWeaknessEngine.enemies[0])
	}
	playerWeaknessControl := newPlayerWeaknessEngine()
	playerWeaknessControlDamage, err := playerWeaknessControl.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if err != nil {
		return err
	}
	if len(playerWeaknessControlDamage) != 2 || playerWeaknessControlDamage[0].Args[2] != -5761 ||
		playerWeaknessControlDamage[0].Args[6] != 100 || playerWeaknessControlDamage[0].Args[7] != 0 ||
		playerWeaknessControl.enemies[0].HP != 14239 {
		return fmt.Errorf("official player WEAKNESS control is results=%+v enemy=%+v", playerWeaknessControlDamage, playerWeaknessControl.enemies[0])
	}

	enemyWeaknessRole, err := findEnemyRole(32601107, "WEAKNESS", "")
	if err != nil {
		return err
	}
	enemyWeaknessAttack, err := findEnemyRole(32601107, "ATTACK_AA", "")
	if err != nil {
		return err
	}
	if enemyWeaknessRole.Target != "SELECT" || enemyWeaknessRole.Parameters[0] != "2" ||
		enemyWeaknessRole.Parameters[1] != "300" || enemyWeaknessRole.Parameters[2] != "0" ||
		enemyWeaknessAttack.Target != "SELECT" || enemyWeaknessAttack.Parameters[0] != "0" ||
		enemyWeaknessAttack.Parameters[2] != "1000" || enemyWeaknessAttack.Parameters[4] != "1" ||
		enemyWeaknessAttack.Parameters[5] != "INT" || enemyWeaknessAttack.Parameters[6] != "0" ||
		enemyWeaknessAttack.Parameters[7] != "ICE" || enemyWeaknessAttack.Parameters[8] != "MAGIC" {
		return fmt.Errorf("official enemy WEAKNESS/attack rows changed: weakness=%+v attack=%+v", enemyWeaknessRole, enemyWeaknessAttack)
	}
	newEnemyWeaknessEngine := func() *BattleEngine {
		engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
		engine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		for index := 1; index < maxRoomMembers; index++ {
			engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1}
		}
		engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, Magic: 3500, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		return engine
	}
	enemyWeaknessEngine := newEnemyWeaknessEngine()
	enemyWeaknessApply, err := enemyWeaknessEngine.executeEnemyRole(
		&enemyWeaknessEngine.enemies[0], 1, *enemyWeaknessRole, []CombatSkillRole{*enemyWeaknessRole},
	)
	if err != nil {
		return err
	}
	if len(enemyWeaknessApply) != 2 || enemyWeaknessApply[0].Command != CommandBuff || len(enemyWeaknessApply[0].Args) != 11 ||
		enemyWeaknessApply[1].Command != CommandBattleParam || !equalBattleArgs(enemyWeaknessApply[1].Args, []int64{1, 10000, 10000, 0, 0, 0, 0, 0, 99999, 99999, 99999}) ||
		enemyWeaknessApply[0].Args[0] != 1 || enemyWeaknessApply[0].Args[3] != int64(battleBuffCodes["WEAKNESS"]) ||
		enemyWeaknessApply[0].Args[4] != 5 || enemyWeaknessApply[0].Args[7] != 0 ||
		len(enemyWeaknessEngine.players[0].Effects) != 1 || enemyWeaknessEngine.players[0].Effects[0].Function != "WEAKNESS" ||
		enemyWeaknessEngine.players[0].Effects[0].Kind != 2 || enemyWeaknessEngine.players[0].Effects[0].Rate != 300 ||
		enemyWeaknessEngine.players[0].Effects[0].Remaining != 2 || enemyWeaknessEngine.players[0].Effects[0].Source != 5 {
		return fmt.Errorf("official enemy WEAKNESS application is results=%+v player=%+v", enemyWeaknessApply, enemyWeaknessEngine.players[0])
	}
	enemyWeaknessDamage, err := enemyWeaknessEngine.executeEnemyAttack(
		&enemyWeaknessEngine.enemies[0], 1, *enemyWeaknessAttack, []CombatSkillRole{*enemyWeaknessAttack},
	)
	if err != nil {
		return err
	}
	if len(enemyWeaknessDamage) != 2 || enemyWeaknessDamage[0].Command != 60 ||
		enemyWeaknessDamage[0].Args[2] != -4550 || enemyWeaknessDamage[0].Args[6] != 100 ||
		enemyWeaknessDamage[0].Args[7] != 0 || enemyWeaknessEngine.players[0].HP != 5450 ||
		enemyWeaknessDamage[1].Command != CommandHP {
		return fmt.Errorf("official enemy WEAKNESS attack is results=%+v player=%+v", enemyWeaknessDamage, enemyWeaknessEngine.players[0])
	}
	enemyWeaknessControl := newEnemyWeaknessEngine()
	enemyWeaknessControlDamage, err := enemyWeaknessControl.executeEnemyAttack(
		&enemyWeaknessControl.enemies[0], 1, *enemyWeaknessAttack, []CombatSkillRole{*enemyWeaknessAttack},
	)
	if err != nil {
		return err
	}
	if len(enemyWeaknessControlDamage) != 2 || enemyWeaknessControlDamage[0].Args[2] != -3500 ||
		enemyWeaknessControlDamage[0].Args[6] != 100 || enemyWeaknessControlDamage[0].Args[7] != 0 ||
		enemyWeaknessControl.players[0].HP != 6500 {
		return fmt.Errorf("official enemy WEAKNESS control is results=%+v player=%+v", enemyWeaknessControlDamage, enemyWeaknessControl.players[0])
	}

	// Native role 77/action type 2 stores ENCHANT as an actor-side buff and
	// appends one independent damage_type=1 ResultCmd60 after each ordinary hit.
	// Use official producer rows on both sides so the formula-only synthetic
	// sample above is no longer the sole proof of source, attribute, status
	// projection, pre-commit HP, or the normal/enchant/HP command order.
	playerEnchantRole, err := findPlayerRole(11103532, "ENCHANT")
	if err != nil {
		return err
	}
	if playerEnchantRole.Target != "SELF" || playerEnchantRole.Parameters[0] != "2" ||
		playerEnchantRole.Parameters[1] != "563" || playerEnchantRole.Parameters[2] != "1000" ||
		playerEnchantRole.Parameters[3] != "0" || playerEnchantRole.Parameters[4] != "50" ||
		playerEnchantRole.Parameters[5] != "LIGHT" {
		return fmt.Errorf("official player ENCHANT role changed: %+v", playerEnchantRole)
	}
	newPlayerEnchantEngine := func() *BattleEngine {
		engine := &BattleEngine{catalog: catalog, rng: newXorShift128(3), turn: 1, enemyCount: 1}
		engine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, Attack: 1000, HP: 10000, MaxHP: 10000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		for index := 1; index < maxRoomMembers; index++ {
			engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1}
		}
		engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 20000, MaxHP: 20000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		engine.enemies[0].Level.AttributeRates[combatAttributeIndex("ICE")] = 100
		engine.enemies[0].Level.AttributeRates[combatAttributeIndex("LIGHT")] = 100
		return engine
	}
	playerEnchantEngine := newPlayerEnchantEngine()
	playerEnchantApply, err := playerEnchantEngine.executePlayerRole(
		battleAction{memberType: 1, target: 1, cardLevel: 60, roles: []CombatSkillRole{*playerEnchantRole}},
		*playerEnchantRole, 1,
	)
	if err != nil {
		return err
	}
	if len(playerEnchantApply) != 2 || playerEnchantApply[0].Command != CommandBuff || len(playerEnchantApply[0].Args) != 11 ||
		playerEnchantApply[1].Command != CommandBattleParam || !equalBattleArgs(playerEnchantApply[1].Args, []int64{1, 10000, 10000, 1000, 0, 0, 0, 0, 99999, 99999, 99999}) ||
		playerEnchantApply[0].Args[0] != 1 || playerEnchantApply[0].Args[3] != int64(battleBuffCodes["ENCHANT"]) ||
		playerEnchantApply[0].Args[4] != 1 || playerEnchantApply[0].Args[6] != 1<<combatAttributeCode("LIGHT") ||
		playerEnchantApply[0].Args[7] != 0 || len(playerEnchantEngine.players[0].Effects) != 1 ||
		playerEnchantEngine.players[0].Effects[0].Function != "ENCHANT" || playerEnchantEngine.players[0].Effects[0].Kind != 1 ||
		playerEnchantEngine.players[0].Effects[0].Value != 3563 || playerEnchantEngine.players[0].Effects[0].Attribute != "LIGHT" ||
		playerEnchantEngine.players[0].Effects[0].Remaining != 2 || playerEnchantEngine.players[0].Effects[0].Source != 1 {
		return fmt.Errorf("official player ENCHANT application is results=%+v player=%+v", playerEnchantApply, playerEnchantEngine.players[0])
	}
	playerEnchantDamage, err := playerEnchantEngine.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if err != nil {
		return err
	}
	if len(playerEnchantDamage) != 3 || playerEnchantDamage[0].Command != 60 || playerEnchantDamage[0].Args[2] != -5761 ||
		playerEnchantDamage[0].Args[3] != 20000 || playerEnchantDamage[0].Args[8] != 0 ||
		playerEnchantDamage[1].Command != 60 || playerEnchantDamage[1].Args[1] != int64(attrInvalidAttackRole.RoleIndex) ||
		playerEnchantDamage[1].Args[2] != -3563 || playerEnchantDamage[1].Args[3] != 20000 ||
		playerEnchantDamage[1].Args[5] != int64(combatAttributeCode("LIGHT")) || playerEnchantDamage[1].Args[6] != 100 ||
		playerEnchantDamage[1].Args[7] != 0 || playerEnchantDamage[1].Args[8] != 1 || playerEnchantDamage[1].Args[9] != 1 ||
		playerEnchantDamage[2].Command != CommandHP || playerEnchantEngine.enemies[0].HP != 10676 {
		return fmt.Errorf("official player ENCHANT attack is results=%+v enemy=%+v", playerEnchantDamage, playerEnchantEngine.enemies[0])
	}
	playerEnchantControl := newPlayerEnchantEngine()
	playerEnchantControlDamage, err := playerEnchantControl.executePlayerAttack(
		battleAction{memberType: 1, target: 5, cardLevel: 60, roles: []CombatSkillRole{*attrInvalidAttackRole}},
		*attrInvalidAttackRole, 1,
	)
	if err != nil {
		return err
	}
	if len(playerEnchantControlDamage) != 2 || playerEnchantControlDamage[0].Args[2] != -5761 ||
		playerEnchantControl.enemies[0].HP != 14239 {
		return fmt.Errorf("official player ENCHANT control is results=%+v enemy=%+v", playerEnchantControlDamage, playerEnchantControl.enemies[0])
	}

	enemyEnchantRole, err := findEnemyRole(33801623, "ENCHANT", "")
	if err != nil {
		return err
	}
	if enemyEnchantRole.Target != "SELECT" || enemyEnchantRole.Parameters[0] != "1" ||
		enemyEnchantRole.Parameters[1] != "10000" || enemyEnchantRole.Parameters[2] != "1000" ||
		enemyEnchantRole.Parameters[3] != "0" || enemyEnchantRole.Parameters[4] != "0" ||
		enemyEnchantRole.Parameters[5] != "FIRE" {
		return fmt.Errorf("official enemy ENCHANT role changed: %+v", enemyEnchantRole)
	}
	newEnemyEnchantEngine := func() *BattleEngine {
		engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
		engine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 20000, MaxHP: 20000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		for index := 1; index < maxRoomMembers; index++ {
			engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1}
		}
		engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, Magic: 3500, HP: 20000, MaxHP: 20000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
		return engine
	}
	enemyEnchantEngine := newEnemyEnchantEngine()
	enemyEnchantApply, err := enemyEnchantEngine.executeEnemyRole(
		&enemyEnchantEngine.enemies[0], 5, *enemyEnchantRole, []CombatSkillRole{*enemyEnchantRole},
	)
	if err != nil {
		return err
	}
	if len(enemyEnchantApply) != 2 || enemyEnchantApply[0].Command != CommandBuff || len(enemyEnchantApply[0].Args) != 11 ||
		enemyEnchantApply[1].Command != CommandBattleParam || !equalBattleArgs(enemyEnchantApply[1].Args, []int64{5, 20000, 20000, 0, 3500, 0, 0, 0, 99999, 99999, 99999}) ||
		enemyEnchantApply[0].Args[0] != 5 || enemyEnchantApply[0].Args[3] != int64(battleBuffCodes["ENCHANT"]) ||
		enemyEnchantApply[0].Args[4] != 1 || enemyEnchantApply[0].Args[6] != 1<<combatAttributeCode("FIRE") ||
		enemyEnchantApply[0].Args[7] != 0 || len(enemyEnchantEngine.enemies[0].Effects) != 1 ||
		enemyEnchantEngine.enemies[0].Effects[0].Function != "ENCHANT" || enemyEnchantEngine.enemies[0].Effects[0].Kind != 1 ||
		enemyEnchantEngine.enemies[0].Effects[0].Value != 10000 || enemyEnchantEngine.enemies[0].Effects[0].Attribute != "FIRE" ||
		enemyEnchantEngine.enemies[0].Effects[0].Remaining != 1 || enemyEnchantEngine.enemies[0].Effects[0].Source != 5 {
		return fmt.Errorf("official enemy ENCHANT application is results=%+v enemy=%+v", enemyEnchantApply, enemyEnchantEngine.enemies[0])
	}
	enemyEnchantDamage, err := enemyEnchantEngine.executeEnemyAttack(
		&enemyEnchantEngine.enemies[0], 1, *enemyWeaknessAttack, []CombatSkillRole{*enemyWeaknessAttack},
	)
	if err != nil {
		return err
	}
	if len(enemyEnchantDamage) != 3 || enemyEnchantDamage[0].Command != 60 || enemyEnchantDamage[0].Args[2] != -3500 ||
		enemyEnchantDamage[0].Args[3] != 20000 || enemyEnchantDamage[0].Args[8] != 0 ||
		enemyEnchantDamage[1].Command != 60 || enemyEnchantDamage[1].Args[1] != 0 ||
		enemyEnchantDamage[1].Args[2] != -10000 || enemyEnchantDamage[1].Args[3] != 20000 ||
		enemyEnchantDamage[1].Args[5] != int64(combatAttributeCode("FIRE")) || enemyEnchantDamage[1].Args[6] != 100 ||
		enemyEnchantDamage[1].Args[7] != 0 || enemyEnchantDamage[1].Args[8] != 1 || enemyEnchantDamage[1].Args[9] != 5 ||
		enemyEnchantDamage[2].Command != CommandHP || enemyEnchantEngine.players[0].HP != 6500 {
		return fmt.Errorf("official enemy ENCHANT attack is results=%+v player=%+v", enemyEnchantDamage, enemyEnchantEngine.players[0])
	}
	enemyEnchantControl := newEnemyEnchantEngine()
	enemyEnchantControlDamage, err := enemyEnchantControl.executeEnemyAttack(
		&enemyEnchantControl.enemies[0], 1, *enemyWeaknessAttack, []CombatSkillRole{*enemyWeaknessAttack},
	)
	if err != nil {
		return err
	}
	if len(enemyEnchantControlDamage) != 2 || enemyEnchantControlDamage[0].Args[2] != -3500 ||
		enemyEnchantControl.players[0].HP != 16500 {
		return fmt.Errorf("official enemy ENCHANT control is results=%+v player=%+v", enemyEnchantControlDamage, enemyEnchantControl.players[0])
	}
	// REVIVE emits SkillRevive without an additional lifecycle or HP row.
	reviveEngine := &BattleEngine{enemyCount: 2}
	reviveEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000}
	reviveEngine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 0, MaxHP: 1000, Broken: true}
	reviveRole := CombatSkillRole{Function: "REVIVE", Target: "DEAD_ENEMY_ONE", RoleIndex: 4}
	reviveRole.Parameters[0] = "600"
	reviveRole.Parameters[1] = "25"
	reviveResults, err := reviveEngine.executeEnemyRevive(&reviveEngine.enemies[0], 6, reviveRole)
	if err != nil {
		return err
	}
	if len(reviveResults) != 1 || reviveResults[0].Command != CommandSkillRevive || len(reviveResults[0].Args) != 4 || reviveEngine.enemies[1].HP != 625 || reviveEngine.enemies[1].Broken {
		return fmt.Errorf("native revive projection/state is results=%+v enemy=%+v", reviveResults, reviveEngine.enemies[1])
	}

	// BLESS is an APPEND_CARD_BLESS hold, not a ResultCmd62 buff. The card is
	// registered with HOLD_SET, executes under HOLD_SKILL without recursively
	// creating another hold, and survives only when p2 requests repetition.
	blessVariants := catalog.Skills[11107792]
	blessRoles := catalog.SkillRoles[11107792]
	if len(blessVariants) == 0 || len(blessRoles) == 0 {
		return errors.New("bless simulation skill 11107792 is unavailable")
	}
	var blessRole *CombatSkillRole
	for index := range blessRoles {
		if blessRoles[index].Function == "BLESS" {
			blessRole = &blessRoles[index]
			break
		}
	}
	if blessRole == nil {
		return errors.New("bless simulation role is unavailable")
	}
	blessEngine := &BattleEngine{catalog: catalog, holdMax: 5, turn: 1, enemyCount: 1, rng: newXorShift128(1)}
	blessEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 1000, MaxHP: 1000, Attack: 1000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	blessEngine.enemies[0] = battleEnemy{MemberType: 5, HP: 100000000, MaxHP: 100000000, Attribute: "FIRE"}
	blessAction := battleAction{
		memberType: 1, cardType: 1, cardLevel: 60, target: 0,
		skill: blessVariants[0], roles: blessRoles,
		callSkill: catalog.Skills[11107802][0], callRoles: catalog.SkillRoles[11107802],
	}
	blessSet, err := blessEngine.executePlayerBless(blessAction, *blessRole, 1)
	if err != nil {
		return err
	}
	if len(blessSet) != 1 || blessSet[0].Command != CommandHoldSet || len(blessSet[0].Args) != 9 ||
		len(blessEngine.players[0].BlessHolds) != 1 || blessEngine.players[0].BlessHolds[0].Remaining != 4 {
		return fmt.Errorf("bless HOLD_SET state/projection is results=%+v holds=%+v", blessSet, blessEngine.players[0].BlessHolds)
	}
	blessExec, err := blessEngine.executeBlessHolds("USER_ATTACK_END")
	if err != nil {
		return err
	}
	if len(blessExec) < 2 || blessExec[0].Command != CommandHoldSkill || len(blessExec[0].Args) != 11 ||
		blessExec[len(blessExec)-1].Command != CommandHoldSkillEnd || len(blessEngine.players[0].BlessHolds) != 1 {
		return fmt.Errorf("bless HOLD_SKILL state/projection is results=%+v holds=%+v", blessExec, blessEngine.players[0].BlessHolds)
	}
	for _, result := range blessExec[1:] {
		if result.Command == CommandHoldSet {
			return errors.New("append-card blessing recursively created a hold")
		}
	}
	blessEngine.turn = 2
	if lost := blessEngine.tickBlessHolds(); len(lost) != 0 || blessEngine.players[0].BlessHolds[0].Remaining != 3 {
		return fmt.Errorf("bless turn tick is lost=%+v holds=%+v", lost, blessEngine.players[0].BlessHolds)
	}
	oneShotEngine := &BattleEngine{catalog: catalog}
	oneShotEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 100, MaxHP: 100, BlessHolds: []battleBlessHold{{
		AppendIndex: 2, CardType: 22, SourceMember: 1,
		// Synthetic recursive payload checks only the APPEND_CARD_BLESS guard.
		Skill: CombatSkillDefinition{ID: 1, Target: "SELF"}, Roles: []CombatSkillRole{*blessRole}, CardLevel: 60,
		Remaining: 99, Repeat: false,
	}}}
	oneShotResults, err := oneShotEngine.executeBlessHolds("USER_ATTACK_END")
	if err != nil {
		return err
	}
	if len(oneShotEngine.players[0].BlessHolds) != 0 || len(oneShotResults) != 3 || oneShotResults[1].Command != CommandHoldLost {
		return fmt.Errorf("one-shot blessing removal is results=%+v holds=%+v", oneShotResults, oneShotEngine.players[0].BlessHolds)
	}

	// Native BLESS_TURN_UP/DOWN producers FUN_0009ae4a/FUN_0009af5e feed
	// append-card control subtype BLESS(3). FUN_00080070 emits ResultCmd206 only
	// when FUN_00043034 finds a matching blessing, then FUN_00042e00 changes
	// every matching hold and removes expired entries in vector order. A curse
	// by itself must not produce a blessing-turn direction.
	blessTurnUp, err := findPlayerRole(14305502, "BLESS_TURN_UP")
	if err != nil {
		return err
	}
	if blessTurnUp.Target != "FRIEND_ALL" || blessTurnUp.Parameters[0] != "1" ||
		(strings.TrimSpace(blessTurnUp.Parameters[1]) != "" && blessTurnUp.Parameters[1] != "NULL") {
		return fmt.Errorf("official BLESS_TURN_UP 14305502 changed: %+v", *blessTurnUp)
	}
	turnUpEngine := &BattleEngine{}
	for index := range turnUpEngine.players {
		turnUpEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 100, MaxHP: 100}
	}
	turnUpEngine.players[0].BlessHolds = []battleBlessHold{
		{AppendIndex: 0, CardType: 21, Skill: CombatSkillDefinition{Attribute: "FIRE"}, Remaining: 2},
		{AppendIndex: 1, CardType: 22, Skill: CombatSkillDefinition{Attribute: "FIRE"}, Remaining: 2},
		{AppendIndex: 2, CardType: 22, Skill: CombatSkillDefinition{Attribute: "ICE"}, Remaining: 1},
	}
	turnUpEngine.players[1].BlessHolds = []battleBlessHold{
		{AppendIndex: 0, CardType: 21, Skill: CombatSkillDefinition{Attribute: "DARK"}, Remaining: 2},
	}
	turnUpResults := turnUpEngine.changePlayerBlessTurns(
		battleAction{memberType: 1}, *blessTurnUp, combatParameterInt(blessTurnUp.Parameters[0]),
	)
	if len(turnUpResults) != 1 || turnUpResults[0].Command != CommandAppendTurnAdd || len(turnUpResults[0].Args) != 5 ||
		turnUpResults[0].Args[0] != 1 || turnUpResults[0].Args[1] != int64(blessTurnUp.RoleIndex) ||
		turnUpResults[0].Args[2] != 1 || turnUpResults[0].Args[3] != 0 || turnUpResults[0].Args[4] != 1 ||
		len(turnUpEngine.players[0].BlessHolds) != 3 || turnUpEngine.players[0].BlessHolds[0].Remaining != 2 ||
		turnUpEngine.players[0].BlessHolds[1].Remaining != 3 || turnUpEngine.players[0].BlessHolds[2].Remaining != 2 ||
		len(turnUpEngine.players[1].BlessHolds) != 1 || turnUpEngine.players[1].BlessHolds[0].Remaining != 2 {
		return fmt.Errorf("official BLESS_TURN_UP projection/state is results=%+v players=%+v", turnUpResults, turnUpEngine.players)
	}

	blessTurnDown, err := findEnemyRole(37601114, "BLESS_TURN_DOWN", "")
	if err != nil {
		return err
	}
	if blessTurnDown.Target != "SELECT" || blessTurnDown.Parameters[0] != "1" ||
		(strings.TrimSpace(blessTurnDown.Parameters[1]) != "" && blessTurnDown.Parameters[1] != "NULL") {
		return fmt.Errorf("official BLESS_TURN_DOWN 37601114 changed: %+v", *blessTurnDown)
	}
	turnDownEngine := &BattleEngine{enemyCount: 1}
	turnDownEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 100, MaxHP: 100, BlessHolds: []battleBlessHold{
		{AppendIndex: 0, CardType: 21, Skill: CombatSkillDefinition{Attribute: "FIRE"}, Remaining: 2},
		{AppendIndex: 1, CardType: 22, Skill: CombatSkillDefinition{Attribute: "ICE"}, Remaining: 1},
	}}
	turnDownEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 100, MaxHP: 100}
	turnDownResults := turnDownEngine.changeEnemyBlessTurns(
		&turnDownEngine.enemies[0], 1, *blessTurnDown, -combatParameterInt(blessTurnDown.Parameters[0]),
	)
	if len(turnDownResults) != 2 || turnDownResults[0].Command != CommandAppendTurnAdd ||
		len(turnDownResults[0].Args) != 5 || turnDownResults[0].Args[0] != 1 ||
		turnDownResults[0].Args[1] != int64(blessTurnDown.RoleIndex) || turnDownResults[0].Args[2] != 1 ||
		turnDownResults[0].Args[3] != 0 || turnDownResults[0].Args[4] != -1 ||
		turnDownResults[1].Command != CommandHoldLost || len(turnDownResults[1].Args) != 4 ||
		turnDownResults[1].Args[0] != 1 || turnDownResults[1].Args[1] != 22 || turnDownResults[1].Args[3] != 1 ||
		len(turnDownEngine.players[0].BlessHolds) != 1 || turnDownEngine.players[0].BlessHolds[0].CardType != 21 {
		return fmt.Errorf("official BLESS_TURN_DOWN projection/removal is results=%+v player=%+v", turnDownResults, turnDownEngine.players[0])
	}

	filteredTurnDown := *blessTurnDown
	filteredTurnDown.Parameters[1] = "FIRE"
	filteredEngine := &BattleEngine{enemyCount: 1}
	filteredEngine.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 100, MaxHP: 100, BlessHolds: []battleBlessHold{
		{AppendIndex: 3, CardType: 22, Skill: CombatSkillDefinition{Attribute: "FIRE"}, Remaining: 1},
		{AppendIndex: 4, CardType: 22, Skill: CombatSkillDefinition{Attribute: "ICE"}, Remaining: 1},
	}}
	filteredEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 100, MaxHP: 100}
	filteredResults := filteredEngine.changeEnemyBlessTurns(&filteredEngine.enemies[0], 1, filteredTurnDown, -1)
	if len(filteredResults) != 2 || filteredResults[0].Command != CommandAppendTurnAdd ||
		filteredResults[0].Args[3] != 1 || filteredResults[1].Command != CommandHoldLost ||
		filteredResults[1].Args[3] != 3 || len(filteredEngine.players[0].BlessHolds) != 1 ||
		filteredEngine.players[0].BlessHolds[0].Skill.Attribute != "ICE" || filteredEngine.players[0].BlessHolds[0].Remaining != 1 {
		return fmt.Errorf("attribute-filtered blessing turn control is results=%+v holds=%+v", filteredResults, filteredEngine.players[0].BlessHolds)
	}
	return nil
}
