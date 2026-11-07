package battlecore

import (
	"errors"
	"fmt"
	"strings"
)

const (
	burstGaugeUnavailable = iota
	burstGaugeNormal
	burstGaugeBurst
	burstGaugeBreak
)

func burstGaugeStateCode(value string) (int, bool) {
	switch value {
	case "NONE":
		return burstGaugeUnavailable, true
	case "NORMAL":
		return burstGaugeNormal, true
	case "BURST":
		return burstGaugeBurst, true
	case "BREAK":
		return burstGaugeBreak, true
	}
	return 0, false
}

func (engine *BattleEngine) buddyResults(player *battlePlayer) []BattleResult {
	results := make([]BattleResult, 0, len(player.Buddies))
	for _, buddy := range player.Buddies {
		if buddy.BuddyID == 0 {
			continue
		}
		results = append(results, BattleResult{Command: CommandBuddy, Args: []int64{
			int64(player.MemberType), int64(buddy.BuddyType), int64(buddy.BuddyID),
		}})
	}
	return results
}

func (engine *BattleEngine) burstStateResult(player *battlePlayer) BattleResult {
	maximum := 0
	if player.BurstState == burstGaugeNormal || player.BurstState == burstGaugeBurst {
		maximum = engine.catalog.BurstGauge.Maximum
	}
	return BattleResult{Command: CommandBurstState, Args: []int64{
		int64(player.MemberType), int64(player.BurstState), int64(player.Burst), int64(maximum), int64(player.BurstBreak),
	}}
}

func (engine *BattleEngine) burstStateChangeResult(player *battlePlayer) BattleResult {
	maximum := 0
	if player.BurstState == burstGaugeNormal || player.BurstState == burstGaugeBurst {
		maximum = engine.catalog.BurstGauge.Maximum
	}
	return BattleResult{Command: CommandBurstStateChange, Args: []int64{
		int64(player.MemberType), int64(player.BurstState), int64(maximum), int64(player.BurstBreak),
	}}
}

// transitionBurstStates mirrors native FUN_000a8500. Gauge changes do not
// transition immediately: the state boundary is evaluated by TurnPhase.
func (engine *BattleEngine) transitionBurstStates() ([]BattleResult, error) {
	results := make([]BattleResult, 0, len(engine.players))
	var entered []*battlePlayer
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 || player.HP <= 0 {
			continue
		}
		switch player.BurstState {
		case burstGaugeBurst:
			if player.Burst < 1 {
				player.BurstState = burstGaugeBreak
				player.BurstBreak = engine.catalog.BurstGauge.BreakTurns
				results = append(results, engine.releaseBurstPassives(player)...)
				results = append(results, engine.burstStateChangeResult(player))
			}
		case burstGaugeBreak:
			if player.BurstBreak < 1 {
				player.BurstState = burstGaugeNormal
				results = append(results, engine.burstStateChangeResult(player))
			}
		case burstGaugeNormal:
			if player.Burst >= engine.catalog.BurstGauge.NormalThreshold {
				player.BurstState = burstGaugeBurst
				results = append(results, engine.burstStateChangeResult(player))
				entered = append(entered, player)
			}
		}
	}
	// FUN_000a8500 finishes all state transitions before FUN_0005f600 collects
	// and sorts the new entrants' buddy passives as one action list.
	passives, err := engine.executeBurstPassives(entered...)
	return append(results, passives...), err
}

func (engine *BattleEngine) executeBurstPassives(players ...*battlePlayer) ([]BattleResult, error) {
	actions := make([]battleAction, 0, len(players)*5)
	for _, player := range players {
		for _, equipped := range player.Buddies {
			if equipped.BuddyID == 0 || equipped.Level <= 0 {
				continue
			}
			buddy, exists := engine.catalog.Buddies[equipped.BuddyID]
			if !exists || buddy.PassiveSkillID == 0 {
				continue
			}
			variants := engine.catalog.Skills[buddy.PassiveSkillID]
			if len(variants) == 0 {
				return nil, fmt.Errorf("combat burst passive skill %d is unavailable", buddy.PassiveSkillID)
			}
			skill := variants[0]
			roles := engine.catalog.SkillRoles[skill.FunctionID]
			if len(roles) == 0 {
				return nil, fmt.Errorf("combat burst passive role %d is unavailable", skill.FunctionID)
			}
			_, ok := combatSkillTargetCode(skill.Target)
			if !ok {
				return nil, fmt.Errorf("combat burst passive %d target %q is unsupported", skill.ID, skill.Target)
			}
			target := player.MemberType
			if skill.Target != "SELF" && skill.Target != "USER_ONE" {
				target = 0
			}
			actions = append(actions, battleAction{
				memberType: player.MemberType, cardLevel: equipped.ExecutionLevel(skill.ID), target: target,
				skill: skill, roles: roles,
			})
		}
	}
	engine.sortBurstPassiveActions(actions)
	results := make([]BattleResult, 0, len(actions)*4)
	for _, action := range actions {
		engine.player(action.memberType).ExecutionSource.setPhysical(0, 0)
		// 5f600 sorts the shared headers, then 7adb0 selects the passive's
		// extend branch at execution time, using the current member state.
		variants := engine.catalog.Skills[action.skill.ID]
		skill, branchIndex := engine.selectCombatSkillVariantsMode(variants, action, engine.turnActions, nil, true)
		if skill.ID == 0 {
			continue
		}
		action.skill, action.branchIndex = skill, branchIndex
		action.roles = engine.catalog.SkillRoles[skill.FunctionID]
		if len(action.roles) == 0 {
			return nil, fmt.Errorf("combat burst passive role %d is unavailable", skill.FunctionID)
		}
		action.skillBonus = &battleSkillBonus{}
		targetCode, _ := combatSkillTargetCode(action.skill.Target)
		results = append(results, BattleResult{Command: CommandBurstSkill, Args: []int64{
			int64(action.memberType), 0, int64(action.skill.ID), int64(action.target), int64(action.cardLevel),
			int64(targetCode), 0, 0, int64(action.skill.FunctionID), int64(branchIndex), 1,
		}})
		var selectedCards []int
		if action.skill.Target == "HAND_ALL" {
			var err error
			selectedCards, err = engine.selectBurstHandCards(engine.player(action.memberType), action.skill, burstSkillSubmission{})
			if err != nil {
				return nil, err
			}
		}
		for _, cardType := range selectedCards {
			results = append(results, BattleResult{Command: CommandCardBuff, Args: []int64{int64(action.memberType), int64(action.branchIndex), int64(cardType)}})
		}
		if err := engine.registerBurstCardModifiers(action, selectedCards); err != nil {
			return nil, err
		}
		skillResults, err := engine.executeSkillRoleSet(action.memberType, action.target, action.skill, action.roles, false, func(role CombatSkillRole) ([]BattleResult, error) {
			return engine.executeBurstRoleWithListType(action, role, selectedCards, skillEffectListType(action.skill.Type))
		})
		if err != nil {
			return nil, err
		}
		results = append(results, skillResults...)
		engine.nativeSkillSerial++
		display, err := engine.refreshBattleDisplayPowers()
		if err != nil {
			return nil, err
		}
		results = append(results, display...)
	}
	return results, nil
}

// FUN_0005cf95 differs from ordinary card ordering: every equal-priority
// comparison consumes a random bit, including two buddies of the same actor.
func (engine *BattleEngine) sortBurstPassiveActions(actions []battleAction) {
	for boundary := 0; boundary < len(actions)-1; boundary++ {
		for index := len(actions) - 2; index >= boundary; index-- {
			left, right := actions[index], actions[index+1]
			swap := left.skill.PriorityPVE > right.skill.PriorityPVE
			if left.skill.PriorityPVE == right.skill.PriorityPVE {
				swap = engine.rng.next()&1 != 0
			}
			if swap {
				actions[index], actions[index+1] = right, left
			}
		}
	}
}

func (engine *BattleEngine) releaseBurstPassives(player *battlePlayer) []BattleResult {
	ensurePlayerBaseParameters(player)
	kept := player.Effects[:0]
	results := make([]BattleResult, 0, 8)
	released := make(map[[3]int]struct{})
	for _, effect := range player.Effects {
		if effect.ListType != 6 && effect.ListType != 7 {
			kept = append(kept, effect)
			continue
		}
		revertPlayerEffect(player, effect)
		if code, exists := battleBuffCodes[effect.Function]; exists && effect.ListType == 6 {
			key, result := naturalEffectReleaseResult(player.MemberType, effect, code)
			result.Args[1] = 6
			if _, duplicate := released[key]; !duplicate {
				results = append(results, result)
				released[key] = struct{}{}
			}
		}
	}
	player.Effects = kept
	refreshPlayerBattleParameters(player)
	refreshPlayerAttribute(player)
	// FUN_000735e4 clears BURST_PASSIVE and CARD, with grouped 72 followed
	// unconditionally by 5/6. No per-entry 71; 504 comes afterwards.
	return append(results,
		playerBaseParameterResult(player),
		BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(
			player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
			player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery,
		)},
	)
}

func (engine *BattleEngine) tickBurstCounters() {
	for _, player := range engine.playerStates() {
		tickPlayerBurstCounters(player)
	}
}

func tickPlayerBurstCounters(player *battlePlayer) {
	if player.GameOver {
		return
	}
	for slot := range player.BurstRecharge {
		if player.BurstRecharge[slot] > 0 {
			player.BurstRecharge[slot]--
		}
	}
	if player.BurstBreak > 0 {
		player.BurstBreak--
	}
}

func (engine *BattleEngine) burstPlayConditionResults() []BattleResult {
	results := make([]BattleResult, 0, len(engine.players)*3)
	for _, player := range engine.playerStates() {
		leader := player.Buddies[0]
		definition, exists := engine.catalog.Buddies[leader.BuddyID]
		if !exists {
			continue
		}
		for skillIndex, skill := range definition.Skills {
			if skill.SkillID == 0 {
				continue
			}
			playable := skill.RequiredLevel <= leader.Level && player.BurstState == burstGaugeBurst &&
				player.Burst >= skill.GaugeCost && player.BurstRecharge[skillIndex] < 1
			playableValue := int64(0)
			if playable {
				playableValue = 1
			}
			results = append(results, BattleResult{Command: CommandBurstCondition, Args: []int64{
				int64(player.MemberType), int64(skillIndex + 1), playableValue,
				int64(player.BurstRecharge[skillIndex]), int64(skill.GaugeCost),
			}})
		}
	}
	return results
}

func (engine *BattleEngine) addBurstGauge(player *battlePlayer, amount int) bool {
	if player.GameOver || player.BurstState == burstGaugeUnavailable || player.BurstBreak > 0 ||
		(player.BurstState == burstGaugeBurst && player.Burst < 1) {
		return false
	}
	player.Burst = maxInt(0, int(int32(player.Burst)+int32(amount)))
	if player.BurstState == burstGaugeNormal || player.BurstState == burstGaugeBurst {
		player.Burst = minInt(engine.catalog.BurstGauge.Maximum, player.Burst)
	}
	return true
}

// 5ee36 visits the registered selection before action sorting/execution.
// 49c83 is the original card cost, before the independent Buddy cost discount.
func (engine *BattleEngine) burstSelectionGaugeResults(actions []battleAction) []BattleResult {
	var rows []BattleResult
	for _, player := range engine.playerStates() {
		if player.HP <= 0 || player.GameOver {
			continue
		}
		for _, action := range actions {
			if action.memberType != player.MemberType {
				continue
			}
			bucket := minInt(4, maxInt(0, action.skill.Cost-1))
			if engine.addBurstGauge(player, engine.catalog.BurstGauge.ReactionRates[bucket]) {
				rows = append(rows, BattleResult{Command: CommandBurstGaugeState, Args: []int64{int64(player.MemberType), int64(player.Burst)}})
			}
		}
	}
	return rows
}

// 5ef2e is called once per incoming attack role (all normal/enchant hits),
// and separately by opponent HP-cut, DOT and card traps. It does not run for
// every HP mutation or for reflected damage back to a player attacker.
func (engine *BattleEngine) burstDamageGaugeResults(player *battlePlayer, damage int) []BattleResult {
	if damage <= 0 || player.MaxHP <= 0 || player.BurstState != burstGaugeBurst {
		return nil
	}
	percent := int64(damage) * 100 / int64(player.MaxHP)
	decrease := maxInt(1, int(percent*int64(engine.catalog.BurstGauge.DamageReduction)/100))
	if !engine.addBurstGauge(player, -decrease) {
		return nil
	}
	return []BattleResult{{Command: CommandBurstGaugeState, Args: []int64{int64(player.MemberType), int64(player.Burst)}}}
}

func burstRoleRegistersCard(function string) bool {
	switch function {
	case "NEED_COST_DOWN_BURST", "ATTACK_MULTISTAGE", "ADD_ATK_OP_PIERCING",
		"DAMAGE_BOOST_ORDER_TRIBAL", "ATK_UP_BOOST_ORDER_TRIBAL", "DEF_UP_BOOST_ORDER_TRIBAL", "DAMAGE_BOOST":
		return true
	default:
		return false
	}
}

func burstRoleUsesCardTarget(function, target string) bool {
	if function == "DAMAGE_BOOST" {
		return target == "HAND_SELECT" || target == "HAND_ALL"
	}
	return burstRoleRegistersCard(function)
}

// Automatic HAND_ALL and active hand skills retain the same card-local program.
func (engine *BattleEngine) registerBurstCardModifiers(action battleAction, selectedCards []int) error {
	for _, role := range action.roles {
		if !burstRoleUsesCardTarget(role.Function, action.skill.Target) {
			continue
		}
		for _, cardType := range selectedCards {
			if err := engine.registerBurstCardSkill(engine.player(action.memberType), cardType, action.skill.ID, action.branchIndex); err != nil {
				return err
			}
		}
		break
	}
	return nil
}

func (engine *BattleEngine) registerBurstCardSkill(player *battlePlayer, cardType int, skillID, branchIndex int) error {
	if cardType < 1 || cardType >= len(player.CardBurstSkills) {
		return fmt.Errorf("combat burst card type %d is invalid", cardType)
	}
	order := 0
	for _, entries := range player.CardBurstSkills {
		order += len(entries)
	}
	player.CardBurstSkills[cardType] = append(player.CardBurstSkills[cardType], battleCardBurstSkill{SkillID: skillID, BranchIndex: branchIndex, Serial: engine.nativeSkillSerial, Order: order})
	return nil
}

func (engine *BattleEngine) attachBurstCardModifiers(action *battleAction) error {
	if action == nil || !engine.playerMember(action.memberType) || action.cardType < 1 {
		return nil
	}
	player := engine.player(action.memberType)
	if len(player.CardBurstSkills[action.cardType]) > 0 {
		// Callers may hold a catalog slice. Modifiers must remain local to this
		// action, including in display previews and simultaneous rooms.
		action.roles = append([]CombatSkillRole(nil), action.roles...)
	}
	for _, entry := range player.CardBurstSkills[action.cardType] {
		burstSkillID := entry.SkillID
		roles := engine.cardModifierRoles(entry)
		if len(roles) == 0 {
			return fmt.Errorf("combat burst card skill %d branch %d is unavailable", burstSkillID, entry.BranchIndex)
		}
		for _, burstRole := range roles {
			if !activeCardModifier(burstRole, entry.Age) {
				continue
			}
			switch burstRole.Function {
			case "DAMAGE_BOOST":
				// Native list7 retains the marker but the damage getter excludes
				// CARD-list DAMAGE_BOOST. Do not turn it into a global effect.
			case "NEED_COST_DOWN_BURST":
				// Cost was committed at BurstSkillExec and is consumed by Submit.
			case "ATTACK_MULTISTAGE":
				for index := range action.roles {
					if action.roles[index].Function != "ATTACK_AA" ||
						!damagePhysicsMatches(burstRole.Parameters[1], action.skill.DamageKind) {
						continue
					}
					action.roles[index].Parameters[4] = fmt.Sprintf("%d", maxInt(
						combatParameterInt(action.roles[index].Parameters[4]), combatParameterInt(burstRole.Parameters[2]),
					))
				}
			case "ADD_ATK_OP_PIERCING":
				operator := burstRole
				operator.Function = "ATK_OP_PIERCING"
				operator.Parameters = [10]string{}
				operator.Parameters[0] = burstRole.Parameters[1]
				action.cardAttackOperators = append(action.cardAttackOperators, operator)
			case "DAMAGE_BOOST_ORDER_TRIBAL", "ATK_UP_BOOST_ORDER_TRIBAL", "DEF_UP_BOOST_ORDER_TRIBAL":
				action.roles = append(action.roles, burstRole)
			default:
				return fmt.Errorf("combat burst card modifier %q is unsupported", burstRole.Function)
			}
		}
	}
	return nil
}

// 466e2/464e4 pass 6eeb6's enemy-party vector to 43534. It checks presence
// and tribal identity, not the selected target or current HP. A destroyed
// part can therefore still satisfy this condition for the current wave.
func (engine *BattleEngine) enemyPartyHasRace(raceID int) bool {
	if raceID <= 0 {
		return true
	}
	if engine.catalog == nil {
		return false
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		if enemy.MemberType == 0 {
			continue
		}
		if enemyHasTribe(enemy, raceID) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) deckSkillBonusCount(player *battlePlayer, attribute string, tag int) int {
	if player == nil || engine.catalog == nil {
		return 0
	}
	count := 0
	for _, card := range player.Deck {
		definition, exists := engine.catalog.Cards[card.CardID]
		if !exists {
			continue
		}
		// 7ea92 checks both raw skills, regardless of the actor's job;
		// either skill's attribute OR a positive profile tag is enough.
		matches := tag > 0 && combatCardHasProfileTag(definition, tag)
		for _, skillID := range [...]int{definition.NormalSkillID, definition.ArthurSkillID} {
			if variants := engine.catalog.Skills[skillID]; len(variants) > 0 {
				for _, component := range splitCombatAttributes(attribute) {
					matches = matches || combatAttributeMatches(variants[0].Attribute, component)
				}
			}
		}
		if matches {
			count++
		}
	}
	return count
}

type battleSkillBonus struct {
	active    bool
	limit     bool
	parameter string
	attribute string
	tag       int
	percent   int32
	fixed     int32
}

func (engine *BattleEngine) fixedParameterActionValue(action battleAction, role CombatSkillRole, chainCount int) int {
	first, second := fixedBuffRoleSegmentsScaled(role, action.cardLevel, engine.roleScale(role))
	bonus := action.skillBonus
	if bonus != nil && bonus.active && bonus.limit == (role.Function == "PARAM_LIMIT_BREAK_FIXED") &&
		strings.EqualFold(bonus.parameter, role.Parameters[1]) {
		// 7ea92 modifies ONLY first, with per-card truncation. p1=0 uses
		// p2+p3*level as the flat amount per matching card. Consume even
		// when no card matches, but do not consume on the wrong parameter.
		count := engine.deckSkillBonusCount(engine.player(action.memberType), bonus.attribute, bonus.tag)
		first = deckBonusSegment(first, bonus.percent, bonus.fixed, count)
		bonus.active = false
	}
	value := first + second
	if chainCount > 1 {
		value += int32(role.ChainRate) * int32(chainCount-1)
	}
	return int(value)
}

func deckBonusSegment(value, percent, fixed int32, count int) int32 {
	perCard := fixed
	if percent != 0 {
		perCard = (percent * value) / 100
	}
	return value + perCard*int32(count)
}

// JP BURST parameter producers emit an ordinary parameter effect and a separate
// self-targeted gauge action. No gauge proportion changes the parameter value.
func burstParameterRole(role CombatSkillRole) (CombatSkillRole, int) {
	switch role.Function {
	case "BURST_ATK_UP_FIXED":
		role.Function = "ATK_UP_FIXED"
	case "BURST_DEF_UP_FIXED":
		role.Function = "DEF_UP_FIXED"
	case "BURST_ATK_BREAK_FIXED":
		role.Function = "ATK_BREAK_FIXED"
	case "BURST_DEF_UP_BY_SELF_PARAM":
		role.Function = "DEF_UP_BY_SELF_PARAM"
		return role, 7
	default:
		return role, -1
	}
	return role, 6
}

func (engine *BattleEngine) burstParameterPayment(action battleAction, role CombatSkillRole, slot int) []BattleResult {
	role.Target = "SELF"
	var results []BattleResult
	for _, member := range engine.playerRoleTargets(action, role) {
		amount := int(-int32(combatParameterInt(role.Parameters[slot])))
		rows := engine.addPlayerBurstGauge(member, role.RoleIndex, amount, false)
		if len(rows) > 0 {
			rows[0].Args[4] = 1 // Original207 distinguishes a parameter skill's gauge cost.
		}
		results = append(results, rows...)
	}
	return results
}

func (engine *BattleEngine) parameterBoostedValue(action battleAction, role CombatSkillRole, function string, value int) int {
	fixed, rate := engine.parameterBoostTerms(action, role, function)
	return parameterEffectMagnitude(value, fixed, rate)
}

func (engine *BattleEngine) parameterBoostTerms(action battleAction, role CombatSkillRole, function string) (int, int) {
	actor := engine.player(action.memberType)
	fixed, rate := sphereSupportBoostTerms(actor.Effects, function, action.skill.Attribute, role.Parameters[1], action.skill.Cost, action.cardType)
	for _, modifier := range action.roles {
		if modifier.Function != function+"_ORDER_TRIBAL" ||
			!strings.EqualFold(role.Parameters[1], modifier.Parameters[6]) ||
			!supportAttributeMatches(modifier.Parameters[5], action.skill.Attribute) ||
			!engine.enemyPartyHasRace(combatParameterInt(modifier.Parameters[9])) {
			continue
		}
		costMin, costMax := combatParameterInt(modifier.Parameters[7]), combatParameterInt(modifier.Parameters[8])
		if action.skill.Cost < costMin || (costMax != 0 && action.skill.Cost > costMax) {
			continue
		}
		// 9b908: p1+p2*L/1000 is fixed; p3+p4*L is the rate. All
		// active registered CN parameter tribal rows have zero p2/p4 growth.
		fixed += combatParameterInt(modifier.Parameters[1]) + combatParameterInt(modifier.Parameters[2])*action.cardLevel/1000
		rate += combatParameterInt(modifier.Parameters[3]) + combatParameterInt(modifier.Parameters[4])*action.cardLevel
	}
	// 4641c + 464e4 accumulate into the same fixed/rate pair before 45070.
	return fixed, rate
}

func (engine *BattleEngine) ExecuteBurst(memberType int, submission burstSkillSubmission) ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != PhaseUser {
		return nil, errors.New("combat burst skill phase is unavailable")
	}
	if memberType < 1 || memberType > maxRoomMembers {
		return nil, errors.New("combat burst skill member is invalid")
	}
	if submission.SkillSlot < 1 || submission.SkillSlot > 3 {
		return nil, errors.New("combat burst skill slot is invalid")
	}
	player := engine.player(memberType)
	if player.HP <= 0 || player.BurstState != burstGaugeBurst {
		return nil, errors.New("combat burst skill gauge state is unavailable")
	}
	leader := player.Buddies[0]
	definition, exists := engine.catalog.Buddies[leader.BuddyID]
	if !exists {
		return nil, errors.New("combat burst skill leader buddy is unavailable")
	}
	buddySkill := definition.Skills[submission.SkillSlot-1]
	if buddySkill.SkillID == 0 || buddySkill.RequiredLevel > leader.Level {
		return nil, errors.New("combat burst skill is not released")
	}
	if player.BurstRecharge[submission.SkillSlot-1] > 0 || player.Burst < 1 || player.Burst < buddySkill.GaugeCost {
		return nil, errors.New("combat burst skill gauge or recharge is unavailable")
	}
	variants := engine.catalog.Skills[buddySkill.SkillID]
	if len(variants) == 0 {
		return nil, fmt.Errorf("combat burst skill %d is unavailable", buddySkill.SkillID)
	}
	skill := variants[0]
	selectedCards, err := engine.selectBurstHandCards(player, skill, submission)
	if err != nil {
		return nil, err
	}
	target := submission.Target
	if skill.Target == "HAND_SELECT" || skill.Target == "HAND_ALL" {
		target = 0
	} else if err := engine.validateTarget(skill.Target, memberType, target); err != nil {
		return nil, err
	}
	targetCode, ok := combatSkillTargetCode(skill.Target)
	if !ok {
		return nil, fmt.Errorf("combat burst skill %d has unsupported target %q", skill.ID, skill.Target)
	}

	actions, err := engine.selectedBattleActions()
	if err != nil {
		return nil, err
	}
	player.Burst = maxInt(0, player.Burst-buddySkill.GaugeCost)
	player.ExecutionSource.setPhysical(0, 0)
	player.ExecutionSource.BuddySkillSlot = submission.SkillSlot
	results := []BattleResult{
		{Command: CommandBurstGaugeState, Args: []int64{int64(memberType), int64(player.Burst)}},
	}
	// 67320 spends gauge before 7adb0 selects a branch through d7578.
	// Hand fields belong to the shared SkillData header, not to each branch.
	action := battleAction{memberType: memberType, cardLevel: leader.ExecutionLevel(skill.ID), target: target, skill: skill, skillBonus: &battleSkillBonus{}}
	skill, branchIndex := engine.selectCombatSkillVariantsMode(variants, action, actions, nil, true)
	if skill.ID == 0 {
		return results, errors.New("combat burst skill has no matching branch")
	}
	roles := engine.catalog.SkillRoles[skill.FunctionID]
	if len(roles) == 0 {
		return nil, fmt.Errorf("combat burst skill role %d is unavailable", skill.FunctionID)
	}
	results = append(results, BattleResult{Command: CommandBurstSkill, Args: []int64{
		int64(memberType), int64(submission.SkillSlot), int64(skill.ID), int64(submission.Target), int64(leader.ExecutionLevel(skill.ID)),
		int64(targetCode), 0, 0, int64(skill.FunctionID), int64(branchIndex), 0,
	}})
	for _, cardType := range selectedCards {
		results = append(results, BattleResult{Command: CommandCardBuff, Args: []int64{int64(memberType), int64(branchIndex), int64(cardType)}})
	}
	action.skill, action.burst = skill, true
	results = append(results, engine.reactiveGaugeSkillStart(action, "BURST")...)
	action.skill, action.branchIndex, action.roles = skill, branchIndex, roles
	if err := engine.registerBurstCardModifiers(action, selectedCards); err != nil {
		return nil, err
	}
	skillRows, err := engine.executeSkillRoleSet(action.memberType, action.target, action.skill, roles, false, func(role CombatSkillRole) ([]BattleResult, error) {
		return engine.executeBurstRole(action, role, selectedCards)
	})
	if err != nil {
		return nil, err
	}
	results = append(results, skillRows...)
	engine.nativeSkillSerial++
	displayResults, err := engine.refreshBattleDisplayPowers()
	if err != nil {
		return nil, err
	}
	results = append(results, displayResults...)
	player.BurstRecharge[submission.SkillSlot-1] = buddySkill.RechargeTurn
	for _, deckSlot := range player.Hand {
		if deckSlot == 0 {
			continue
		}
		card := player.handSlotCard(deckSlot)
		cardSkill, _, err := engine.handCardSkill(player, card)
		if err != nil {
			return nil, err
		}
		results = append(results, engine.playerCardCostResult(player, card.CardType, cardSkill.Cost))
	}
	results = append(results, engine.handDarknessResults()...)
	// JP refreshes each registered member's hand state after an active Burst.
	for _, member := range engine.playerStates() {
		for _, deckSlot := range member.Hand {
			if deckSlot != 0 {
				results = append(results, playerCardStateResult(member, member.handSlotCard(deckSlot).CardType))
			}
		}
	}
	// 67320 -> 3ef0c includes ordinary and BLESS/CURSE holds after the hand.
	handDisplay, err := engine.selectionCardDisplayResults(memberType, memberType)
	if err != nil {
		return nil, err
	}
	results = append(results, handDisplay...)
	results = append(results, BattleResult{Command: CommandAttackPartition})
	results = append(results, engine.burstPlayConditionResults()...)
	return results, nil
}

func (engine *BattleEngine) selectBurstHandCards(player *battlePlayer, skill CombatSkillDefinition, submission burstSkillSubmission) ([]int, error) {
	selected := make([]int, 0, len(submission.CardTypes))
	seen := make(map[int]struct{}, len(submission.CardTypes))
	for _, cardType := range submission.CardTypes {
		if cardType == 0 {
			continue
		}
		if _, duplicate := seen[cardType]; duplicate {
			return nil, errors.New("combat burst skill card is duplicated")
		}
		seen[cardType] = struct{}{}
		_, found := player.cardInHand(cardType)
		if !found {
			return nil, fmt.Errorf("combat burst skill card type %d is not in hand", cardType)
		}
		selected = append(selected, cardType)
	}
	if skill.Target == "HAND_SELECT" && len(selected) > skill.HandSelectCount {
		return nil, errors.New("combat burst skill hand condition is not satisfied")
	}
	if skill.Target == "HAND_ALL" {
		selected = nil
		for _, deckSlot := range player.Hand {
			if deckSlot == 0 {
				continue
			}
			card := player.handSlotCard(deckSlot)
			matches, err := engine.burstHandCardMatches(player, card, skill)
			if err != nil {
				return nil, err
			}
			if matches {
				selected = append(selected, card.CardType)
			}
		}
		return selected, nil
	}
	for _, cardType := range selected {
		card, _ := player.cardInHand(cardType)
		matches, err := engine.burstHandCardMatches(player, card, skill)
		if err != nil {
			return nil, err
		}
		if !matches {
			return nil, errors.New("combat burst skill hand condition is not satisfied")
		}
	}
	return selected, nil
}

// 79358 is shared by HAND_SELECT validation and HAND_ALL filtering.
// Each of the three nonzero profile selectors must match independently.
func (engine *BattleEngine) burstHandCardMatches(player *battlePlayer, card BattleCard, skill CombatSkillDefinition) (bool, error) {
	cardSkill, _, err := engine.handCardSkill(player, card)
	if err != nil {
		return false, err
	}
	if !combatNullValue(skill.HandAttribute) && !combatAttributeMatches(cardSkill.Attribute, skill.HandAttribute) ||
		!combatNullValue(skill.HandKind) && !strings.EqualFold(cardSkill.Kind, skill.HandKind) ||
		skill.HandMinCost > 0 && cardSkill.Cost < skill.HandMinCost || skill.HandMaxCost > 0 && cardSkill.Cost > skill.HandMaxCost {
		return false, nil
	}
	definition := engine.catalog.Cards[card.CardID]
	for _, tag := range skill.Groups {
		if tag > 0 && !combatCardHasProfileTag(definition, tag) {
			return false, nil
		}
	}
	return true, nil
}

func (engine *BattleEngine) executeBurstRole(action battleAction, role CombatSkillRole, selectedCards []int) ([]BattleResult, error) {
	// Official active burst skills are NORMAL. BURST_PASSIVE alone uses 6;
	// the activation API does not convert every active effect to list 5.
	return engine.executeBurstRoleWithListType(action, role, selectedCards, skillEffectListType(action.skill.Type))
}

// Original SKILL_TYPE owns the durable list independently of the entry that
// executes it; a Buddy passive header can still declare NORMAL effects.
func skillEffectListType(kind string) int {
	switch kind {
	case "PASSIVE":
		return 1
	case "EVENT":
		return 2
	case "FIELD_NORMAL":
		return 3
	case "FIELD_PASSIVE":
		return 4
	case "BURST_NORMAL":
		return 5
	case "BURST_PASSIVE":
		return 6
	default:
		return 0
	}
}

func (engine *BattleEngine) executeBurstRoleWithListType(action battleAction, role CombatSkillRole, selectedCards []int, listType int) (results []BattleResult, err error) {
	action.buffListType = listType
	// Keep the skill's list identity on both durable state and projected rows.
	var playerLengths [2 * maxRoomMembers]int
	var enemyLengths [maxRoomMembers]int
	for i, player := range engine.playerStates() {
		playerLengths[i] = len(player.Effects)
	}
	for i := range engine.enemies {
		enemyLengths[i] = len(engine.enemies[i].Effects)
	}
	defer func() {
		for i, player := range engine.playerStates() {
			for j := playerLengths[i]; j < len(player.Effects); j++ {
				effect := &player.Effects[j]
				effect.ListType = listType
				if listType == 6 {
					effect.BurstPassive = true
				}
			}
		}
		for i := range engine.enemies {
			for j := enemyLengths[i]; j < len(engine.enemies[i].Effects); j++ {
				engine.enemies[i].Effects[j].ListType = listType
			}
		}
		for i := range results {
			if (results[i].Command == CommandBuff || results[i].Command == CommandPassiveBuff) && len(results[i].Args) == 11 {
				results[i].Args[2] = int64(listType)
				if listType == 6 {
					results[i].Command = CommandPassiveBuff
				}
			}
		}
	}()
	if burstRoleUsesCardTarget(role.Function, action.skill.Target) {
		// Each native modifier role emits 324; the whole skill's roles are
		// retained once per activation for subsequent card execution.
		engine.recordExecutedBuffKind(action.memberType, combatTriggerBuffKind(role.Function))
		for _, cardType := range selectedCards {
			results = append(results, BattleResult{Command: CommandAddCardBuff, Args: []int64{
				int64(action.memberType), int64(cardType), int64(action.skill.ID), int64(engine.nativeSkillSerial),
			}})
		}
	}
	if role.Function == "NEED_COST_DOWN_BURST" {
		amount := maxInt(0, combatParameterInt(role.Parameters[1]))
		for _, cardType := range selectedCards {
			actionPlayer := engine.player(action.memberType)
			if engine.cardDiscountMatches(actionPlayer, cardType, role) {
				actionPlayer.CardCostDown[cardType] += amount
			}
		}
		return results, nil
	}
	if burstRoleUsesCardTarget(role.Function, action.skill.Target) {
		return results, nil
	}
	if role.Function == "DISCARD_DRAW" {
		return engine.executeBurstDiscardDraw(action.memberType, selectedCards, maxInt(0, combatParameterInt(role.Parameters[0])))
	}
	return engine.executePlayerRole(action, role, 1)
}

func (engine *BattleEngine) executeBurstDiscardDraw(memberType int, retainedCardTypes []int, handMaximum int) ([]BattleResult, error) {
	player := engine.player(memberType)
	// 7ee5e removes the member's selection before publishing its empty plans.
	delete(engine.selectedPlays, memberType)
	keptActions := engine.turnActions[:0]
	for _, action := range engine.turnActions {
		if action.memberType != memberType {
			keptActions = append(keptActions, action)
		}
	}
	engine.turnActions = keptActions
	retained := make(map[int]struct{}, len(retainedCardTypes))
	for _, cardType := range retainedCardTypes {
		retained[cardType] = struct{}{}
	}
	discarded := make([]int, 0, len(player.Hand))
	discardArgs := []int64{int64(memberType), 0, 0, 0, 0, 0}
	for slotIndex, deckSlot := range player.Hand {
		if deckSlot == 0 {
			continue
		}
		cardType := player.handSlotCard(deckSlot).CardType
		if _, keep := retained[cardType]; !keep {
			discarded = append(discarded, cardType)
			discardArgs[slotIndex+1] = int64(cardType)
		}
	}
	results := make([]BattleResult, 0, 20)
	for range player.Hand {
		results = append(results, BattleResult{Command: CommandCardPlayPlan, Args: []int64{int64(memberType), 0, 0, 0, 0, 0, 0}})
	}
	results = append(results,
		BattleResult{Command: CommandSpherePlayPlan, Args: []int64{int64(memberType), 0, 0}},
		BattleResult{Command: CommandCardPlayReset, Args: []int64{int64(memberType)}},
		BattleResult{Command: CommandBurstDiscard, Args: discardArgs},
	)
	for _, cardType := range discarded {
		// FUN_0007ee5e removes both good- and bad-list entries owned by each
		// discarded card before it removes that card from the hand. Every
		// non-empty removal list goes through FUN_00072a18 independently, so a
		// later draw of the same card type cannot inherit an obsolete trap or
		// seal. Each entry gets 71/6; 72 waits until its status kind is gone.
		buffs, debuffs := removePlayerCardBoundEffects(player, cardType)
		for _, removed := range [][]battleEffect{buffs, debuffs} {
			if len(removed) == 0 {
				continue
			}
			ensurePlayerBaseParameters(player)
			refreshPlayerBattleParameters(player)
			refreshPlayerAttribute(player)
			results = appendNaturalExpiryRows(results, memberType, removed, player.Effects, player.Attribute)
			results = append(results, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(
				player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
				player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery,
			)})
		}
		engine.removeCardFromHand(memberType, cardType)
	}
	handMaximum = minInt(len(player.Hand), maxInt(0, handMaximum))
	for slotIndex := range player.Hand {
		if player.Hand[slotIndex] != 0 || playerHandCount(player) >= handMaximum {
			continue
		}
		deckSlot := engine.drawCard(player)
		if deckSlot == 0 {
			break
		}
		player.Hand[slotIndex] = deckSlot
		card := player.handSlotCard(deckSlot)
		skill, _, err := engine.handCardSkill(player, card)
		if err != nil {
			return nil, err
		}
		results = append(results,
			BattleResult{Command: CommandCardDeal, Args: []int64{int64(memberType), int64(card.CardType), int64(card.Level)}},
			engine.playerCardCostResult(player, card.CardType, skill.Cost),
			playerCardStateResult(player, card.CardType),
		)
	}
	// 7ee5e refreshes every member's cards through 3d680 before cost/deck rows.
	display, err := engine.selectionCardDisplayResults(0, memberType)
	if err != nil {
		return nil, err
	}
	results = append(results, display...)
	results = append(results,
		BattleResult{Command: CommandCost, Args: []int64{int64(memberType), int64(player.Cost + engine.playerEffectValue(player, "COST_BLOCK"))}},
		BattleResult{Command: CommandCardDeck, Args: []int64{int64(memberType), int64(player.remainingDeckCount()), 0}},
	)
	return results, nil
}

func playerHandCount(player *battlePlayer) int {
	count := 0
	for _, deckSlot := range player.Hand {
		if deckSlot != 0 {
			count++
		}
	}
	return count
}
