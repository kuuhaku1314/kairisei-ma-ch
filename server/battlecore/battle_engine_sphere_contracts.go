package battlecore

import (
	"fmt"
)

// validateSphereContracts keeps the CN 6.0.2 sphere protocol beside its
// implementation instead of growing the general battle contract audit. It is
// run by both server startup and kairi-battle-audit against the complete
// official CN master.
func validateSphereContracts(catalog *CombatCatalog) error {
	if err := validateSphereSupportContracts(catalog); err != nil {
		return err
	}
	normal, normalExists := catalog.Spheres[15000010]
	chalice, chaliceExists := catalog.Spheres[16000014]
	if !normalExists || normal.Type != SphereTypeNormal || normal.SkillID != 15000010 || normal.Count != 1 || normal.PlayCondition != "TURN_AFTER" || normal.PlayParam != 3 {
		return fmt.Errorf("official normal sphere 15000010 contract is %+v", normal)
	}
	if !chaliceExists || chalice.Type != SphereTypeChalice || chalice.SkillID != 16000010 || chalice.PassiveSkillID != 62000265 || chalice.Count != 1 || chalice.PlayCondition != "TURN_AFTER" || chalice.PlayParam != 2 {
		return fmt.Errorf("official chalice sphere 16000014 contract is %+v", chalice)
	}

	update := sphereUpdateResult(2, 3, 4567, 1)
	wantUpdate := []int64{2, 3, 0, 0, 0, 0, 0, 4567, 1}
	if update.Command != CommandSphereUpdate || !equalBattleArgs(update.Args, wantUpdate) {
		return fmt.Errorf("SPHR_UPDATE projection is %+v, want command 307 args %v", update, wantUpdate)
	}

	normalSkill, normalRoles, err := catalog.SphereSkill(normal.ID)
	if err != nil {
		return err
	}
	header, err := playerSphereSkillResult(battleAction{
		memberType: 2, sphereSlot: 1, cardLevel: normal.MaxLevel, target: 5,
		skill: normalSkill, roles: normalRoles, branchIndex: 2,
	}, 3)
	if err != nil {
		return err
	}
	targetCode, ok := combatSkillTargetCode(normalSkill.Target)
	if !ok {
		return fmt.Errorf("official normal sphere target %q has no managed code", normalSkill.Target)
	}
	wantHeader := []int64{
		2, 1, int64(normalSkill.ID), 5, int64(normal.MaxLevel), int64(targetCode),
		0, 3, int64(normalSkill.FunctionID), 2, 0,
	}
	if header.Command != CommandSphereSkill || !equalBattleArgs(header.Args, wantHeader) {
		return fmt.Errorf("SPHR_SKILL projection is %+v, want command 56 args %v", header, wantHeader)
	}

	engine := newSphereContractEngine(catalog)
	engine.players[0].Spheres[0] = battleSphere{
		Slot: 1, SphereID: normal.ID, Level: normal.MaxLevel,
		Type: normal.Type, Count: normal.Count, Maximum: normal.Count,
	}
	engine.players[1].Spheres[1] = battleSphere{
		Slot: 2, SphereID: chalice.ID, Level: chalice.MaxLevel,
		Type: chalice.Type, Count: chalice.Count, Maximum: chalice.Count,
	}
	var identity []BattleResult
	for _, row := range engine.setupIdentityResults() {
		if row.Command == CommandSphere || row.Command == CommandSphereHand {
			identity = append(identity, row)
		}
	}
	if len(identity) != 6 || identity[0].Command != CommandSphere || !equalBattleArgs(identity[0].Args, []int64{1, 1, int64(normal.ID)}) ||
		identity[1].Command != CommandSphere || !equalBattleArgs(identity[1].Args, []int64{2, 2, int64(chalice.ID)}) {
		return fmt.Errorf("sphere identity prefix is %+v", identity)
	}
	for index := 2; index < len(identity); index++ {
		if identity[index].Command != CommandSphereHand || !equalBattleArgs(identity[index].Args, []int64{int64(index - 1), 1, 2, 3}) {
			return fmt.Errorf("sphere hand identity row %d is %+v", index, identity[index])
		}
	}

	state, err := engine.sphereStateResults()
	if err != nil {
		return err
	}
	if len(state) != 6 {
		return fmt.Errorf("sphere state projection returned %d rows, want 6", len(state))
	}
	stateSpheres := []struct {
		row    int
		member int
		slot   int
		def    CombatSphereDefinition
		player *battlePlayer
	}{
		{row: 0, member: 1, slot: 1, def: normal, player: &engine.players[0]},
		{row: 1, member: 2, slot: 2, def: chalice, player: &engine.players[1]},
	}
	for _, current := range stateSpheres {
		skill, roles, skillErr := catalog.SphereSkill(current.def.ID)
		if skillErr != nil {
			return skillErr
		}
		power := engine.cardDisplayPower(current.player, current.def.MaxLevel, skill, roles)
		if state[current.row].Command != CommandSphereCount || !equalBattleArgs(state[current.row].Args, []int64{int64(current.member), int64(current.slot), 1, 1}) ||
			state[current.row+2].Command != CommandSphereCost || !equalBattleArgs(state[current.row+2].Args, []int64{int64(current.member), int64(current.slot), int64(skill.Cost)}) ||
			state[current.row+4].Command != CommandSphereUpdate || !equalBattleArgs(state[current.row+4].Args, []int64{int64(current.member), int64(current.slot), 0, 0, 0, 0, 0, int64(power), 0}) {
			return fmt.Errorf("sphere state rows at %d are %+v", current.row, state[current.row:current.row+3])
		}
	}

	conditionSphere := &engine.players[0].Spheres[0]
	engine.turn = normal.PlayParam
	engine.refreshSphereCondition(conditionSphere, normal)
	if conditionSphere.Playable || conditionSphere.Remaining != 1 {
		return fmt.Errorf("normal sphere pre-boundary state is %+v", conditionSphere)
	}
	engine.turn++
	engine.refreshSphereCondition(conditionSphere, normal)
	if !conditionSphere.Playable || conditionSphere.Remaining != 0 {
		return fmt.Errorf("normal sphere boundary state is %+v", conditionSphere)
	}

	action := battleAction{
		memberType: 1, sphereSlot: 1, cardLevel: normal.MaxLevel, target: 5,
		skill: normalSkill, roles: normalRoles,
	}
	actionResults, err := engine.executeSphereAction(action, 1)
	if err != nil {
		return fmt.Errorf("simulate official normal sphere 15000010: %w", err)
	}
	if len(actionResults) < 3 || actionResults[0].Command != CommandSphereSkill || len(actionResults[0].Args) != 11 ||
		conditionSphere.Count != 0 || !conditionSphere.Playable {
		return fmt.Errorf("normal sphere execution is results=%+v sphere=%+v", actionResults, conditionSphere)
	}
	for _, row := range actionResults {
		if row.Command == CommandSphereCount {
			return fmt.Errorf("sphere count published before next UserPhase: %+v", row)
		}
	}

	chaliceEngine := newSphereContractEngine(catalog)
	chaliceEngine.phase = PhaseUserAttack
	chaliceEngine.turn = chalice.PlayParam + 1
	chaliceSphere := &chaliceEngine.players[0].Spheres[1]
	*chaliceSphere = battleSphere{
		Slot: 2, SphereID: chalice.ID, Level: chalice.MaxLevel,
		Type: chalice.Type, Count: chalice.Count, Maximum: chalice.Count,
	}
	chaliceEngine.refreshSphereCondition(chaliceSphere, chalice)
	if _, err := chaliceEngine.ReserveChaliceSphere(1, 2); err == nil {
		return fmt.Errorf("chalice reserve ignored unsatisfied hold condition")
	}
	chaliceEngine.turnActions = []battleAction{
		{memberType: 1, cardType: 1, skill: CombatSkillDefinition{Attribute: "LIGHT"}},
		{memberType: 2, cardType: 1, skill: CombatSkillDefinition{Attribute: "LIGHT"}},
	}
	if _, err := chaliceEngine.chalicePlayableResults(); err != nil {
		return err
	}
	reserve, err := chaliceEngine.ReserveChaliceSphere(1, 2)
	if err != nil {
		return fmt.Errorf("reserve official chalice sphere 16000014: %w", err)
	}
	if len(reserve) != 1 || reserve[0].Command != CommandChaliceSphereReserve || !equalBattleArgs(reserve[0].Args, []int64{1, 2}) {
		return fmt.Errorf("chalice reserve projection is %+v", reserve)
	}
	chaliceResults, err := chaliceEngine.ExecuteChaliceUserPhase()
	if err != nil {
		return fmt.Errorf("simulate official chalice sphere 16000014: %w", err)
	}
	if len(chaliceResults) < 3 || chaliceResults[0].Command != CommandSphereSkill || len(chaliceResults[0].Args) != 11 ||
		chaliceSphere.Count != 0 || chaliceSphere.ChalicePlayable || chaliceEngine.players[0].ReservedChalice != 0 || chaliceEngine.phase != PhaseChaliceUser {
		return fmt.Errorf("chalice execution is results=%+v sphere=%+v phase=%d", chaliceResults, chaliceSphere, chaliceEngine.phase)
	}

	// Managed BattleDefs writes SphrCsvData.call_skill_id into the distinct
	// SphrData.call_skill object, and native FUN_000ceec0 resolves that object
	// for APPEND_CARD_BLESS. Every populated call skill must therefore be
	// validated as the held payload rather than treated as another main role.
	for sphereID, definition := range catalog.Spheres {
		if definition.CallSkillID == 0 {
			continue
		}
		callSkill, callRoles, callErr := catalog.SphereCallSkill(sphereID)
		if callErr != nil {
			return callErr
		}
		if callSkill.ID != definition.CallSkillID || len(callRoles) == 0 {
			return fmt.Errorf("official sphere %d call-skill projection is skill=%+v roles=%d", sphereID, callSkill, len(callRoles))
		}
		if err := ValidateAppendSkillDefinition("sphere call", callSkill); err != nil {
			return err
		}
	}

	callDefinition, exists := catalog.Spheres[16000220]
	if !exists || callDefinition.CallSkillID != 16000222 {
		return fmt.Errorf("official call-skill sphere 16000220 contract is %+v", callDefinition)
	}
	callEngine := newSphereContractEngine(catalog)
	callEngine.turn = callDefinition.PlayParam + 1
	callEngine.holdMax = 5
	callSphere := &callEngine.players[0].Spheres[0]
	*callSphere = battleSphere{
		Slot: 1, SphereID: callDefinition.ID, Level: callDefinition.MaxLevel,
		Type: callDefinition.Type, Count: callDefinition.Count, Maximum: callDefinition.Count, Playable: true,
	}
	callMainSkill, callMainRoles, err := catalog.SphereSkill(callDefinition.ID)
	if err != nil {
		return err
	}
	callAction := battleAction{
		memberType: 1, sphereSlot: 1, cardLevel: callDefinition.MaxLevel,
		target: 0, skill: callMainSkill, roles: callMainRoles,
	}
	if err := callEngine.attachSphereCallSkill(&callAction, callDefinition.ID); err != nil {
		return err
	}
	callResults, err := callEngine.executeSphereAction(callAction, 1)
	if err != nil {
		return fmt.Errorf("simulate official call-skill sphere 16000220: %w", err)
	}
	if len(callResults) < 3 || len(callEngine.players[0].BlessHolds) != 1 {
		return fmt.Errorf("call-skill sphere did not create one blessing: results=%+v holds=%+v", callResults, callEngine.players[0].BlessHolds)
	}
	hold := callEngine.players[0].BlessHolds[0]
	if hold.Skill.ID != 16000222 || hold.Skill.AppendTrigger != "USER_ATTACK_END" || hold.Skill.AppendCondition != "SELF_OTHER_PLAY_NUM" {
		return fmt.Errorf("call-skill sphere stored wrong blessing payload %+v", hold)
	}
	for _, role := range hold.Roles {
		if role.Function == "BLESS" {
			return fmt.Errorf("call-skill sphere recursively stored its main BLESS role %+v", hold)
		}
	}
	// SELF_OTHER_PLAY_NUM=2 counts the other cards selected by the blessing
	// owner. Execute the hold once as well as inspecting its stored payload: a
	// projection-only check would not detect executeBlessHolds accidentally
	// switching back to the sphere's main BLESS roles.
	callEngine.turnActions = []battleAction{
		{memberType: 1, cardType: 1, skill: CombatSkillDefinition{ID: 1, Attribute: "FIRE"}},
		{memberType: 1, cardType: 2, skill: CombatSkillDefinition{ID: 2, Attribute: "ICE"}},
	}
	followUpResults, err := callEngine.executeBlessHolds("USER_ATTACK_END")
	if err != nil {
		return fmt.Errorf("execute official sphere call skill 16000222: %w", err)
	}
	foundFollowUp := false
	for _, result := range followUpResults {
		if result.Command == CommandHoldSkill && len(result.Args) == 11 && result.Args[3] == 16000222 {
			foundFollowUp = true
		}
	}
	if !foundFollowUp || len(callEngine.players[0].BlessHolds) != 1 || callEngine.players[0].BlessHolds[0].Skill.ID != 16000222 {
		return fmt.Errorf("call-skill sphere follow-up is results=%+v holds=%+v", followUpResults, callEngine.players[0].BlessHolds)
	}
	return nil
}

func newSphereContractEngine(catalog *CombatCatalog) *BattleEngine {
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(602), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{
			MemberType: index + 1, ArthurType: index + 1,
			BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
			HP: 100000, MaxHP: 100000, BaseMaxHP: 100000,
			Attack: 10000, BaseAttack: 10000, Magic: 10000, BaseMagic: 10000,
			Recovery: 10000, BaseRecovery: 10000, Defense: 1000, BaseDefense: 1000,
			MDefense: 1000, BaseMDefense: 1000,
			LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999,
		}
	}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, HP: 1000000000, MaxHP: 1000000000, BaseMaxHP: 1000000000,
		Attack: 1000, BaseAttack: 1000, Magic: 1000, BaseMagic: 1000,
		Recovery: 1000, BaseRecovery: 1000, Defense: 1000, BaseDefense: 1000,
		MDefense: 1000, BaseMDefense: 1000,
		LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999,
		BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
	}
	return engine
}

func equalBattleArgs(got []int64, want []int64) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
