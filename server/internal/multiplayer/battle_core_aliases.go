package multiplayer

import "kairisei.local/server/battlecore"

// Existing server-facing names delegate to the single shared core.
// No battle implementation or mutable state is duplicated here.
func ValidateAppendSkillDefinition(source string, skill CombatSkillDefinition) error {
	return battlecore.ValidateAppendSkillDefinition(source, skill)
}
func SupportsAppendCondition(condition string) bool {
	return battlecore.SupportsAppendCondition(condition)
}
func CardIdentityResult(memberType int, card BattleCard) BattleResult {
	return battlecore.CardIdentityResult(memberType, card)
}

type ContinuePlan = battlecore.ContinuePlan
type ContractResultCodec = battlecore.ContractResultCodec

func ValidateCoreContracts(catalog *CombatCatalog, codec ContractResultCodec) error {
	return battlecore.ValidateCoreContracts(catalog, codec)
}
func ValidBattleDrop(drop BattleDrop) bool         { return battlecore.ValidBattleDrop(drop) }
func SupportsEnemyActionTarget(kind string) bool   { return battlecore.SupportsEnemyActionTarget(kind) }
func SupportsEnemyAITrigger(trigger string) bool   { return battlecore.SupportsEnemyAITrigger(trigger) }
func ValidEnemyAIOrderFields(fields []string) bool { return battlecore.ValidEnemyAIOrderFields(fields) }
func SupportsEnemyFunction(function string) bool   { return battlecore.SupportsEnemyFunction(function) }
func FunctionNeedsBuffCode(function string) bool   { return battlecore.FunctionNeedsBuffCode(function) }
func SupportsEnemyBranchCondition(condition string) bool {
	return battlecore.SupportsEnemyBranchCondition(condition)
}
func SupportsPlayerBranchCondition(condition string) bool {
	return battlecore.SupportsPlayerBranchCondition(condition)
}

type BattleCard = battlecore.BattleCard
type BattleSphere = battlecore.BattleSphere
type BattleBuddy = battlecore.BattleBuddy
type BattleDrop = battlecore.BattleDrop

func EnemySkillLevel(function string) int { return battlecore.EnemySkillLevel(function) }

type WaveOutcome = battlecore.WaveOutcome

func SupportsMainCardPassiveFunction(function string) bool {
	return battlecore.SupportsMainCardPassiveFunction(function)
}
func ValidateEnemyPassiveDefinitions(catalog *CombatCatalog) error {
	return battlecore.ValidateEnemyPassiveDefinitions(catalog)
}
func PreviewEnemyPlans(catalog *CombatCatalog, partyID int, seed uint32, snapshots []battlecore.EnemyPlanSnapshot) ([]battlecore.EnemyTurnPlan, error) {
	return battlecore.PreviewEnemyPlans(catalog, partyID, seed, snapshots)
}
func SupportsPlayerFunction(function string) bool { return battlecore.SupportsPlayerFunction(function) }

const CommandSkill = battlecore.CommandSkill
const CommandCardSkill = battlecore.CommandCardSkill
const CommandBuffEffect = battlecore.CommandBuffEffect
const CommandSphereSkill = battlecore.CommandSphereSkill
const CommandSphere = battlecore.CommandSphere
const CommandSphereCount = battlecore.CommandSphereCount
const CommandSphereCost = battlecore.CommandSphereCost
const CommandCardPlayPlan = battlecore.CommandCardPlayPlan
const CommandSpherePlay = battlecore.CommandSpherePlay
const CommandSpherePlayPlan = battlecore.CommandSpherePlayPlan
const CommandSphereUpdate = battlecore.CommandSphereUpdate
const CommandSphereHand = battlecore.CommandSphereHand
const CommandSpherePlayCondition = battlecore.CommandSpherePlayCondition
const CommandCardUpdate2 = battlecore.CommandCardUpdate2
const CommandSphereUpdate2 = battlecore.CommandSphereUpdate2
const CommandChaliceSphereReserve = battlecore.CommandChaliceSphereReserve
const CommandChaliceSpherePlayable = battlecore.CommandChaliceSpherePlayable

type CardSelectionBatch = battlecore.CardSelectionBatch

const SphereTypeNormal = battlecore.SphereTypeNormal
const SphereTypeChalice = battlecore.SphereTypeChalice

var ErrChaliceSphereUnavailable = battlecore.ErrChaliceSphereUnavailable

func SupportsSupportFunction(function string) bool {
	return battlecore.SupportsSupportFunction(function)
}

type BattleEngine = battlecore.BattleEngine
type Phase = battlecore.Phase

const PhaseCreated = battlecore.PhaseCreated
const PhaseStarted = battlecore.PhaseStarted
const PhaseTurn = battlecore.PhaseTurn
const PhaseUser = battlecore.PhaseUser
const PhaseUserAttack = battlecore.PhaseUserAttack
const PhaseChaliceUser = battlecore.PhaseChaliceUser
const PhaseEnemy = battlecore.PhaseEnemy
const PhaseChaliceEnemy = battlecore.PhaseChaliceEnemy
const PhaseEnded = battlecore.PhaseEnded

func NewBattleEngine(catalog *CombatCatalog, spec battlecore.Config, members []battlecore.Participant) (*BattleEngine, error) {
	return battlecore.NewBattleEngine(catalog, spec, members)
}
func ValidateSupportCards(cards []BattleCard) error { return battlecore.ValidateSupportCards(cards) }
func NativeSupportSkillIndex(partitions [3]int, loveRate int) int {
	return battlecore.NativeSupportSkillIndex(partitions, loveRate)
}

const CommandTurn = battlecore.CommandTurn
const CommandEnd = battlecore.CommandEnd
const CommandHP = battlecore.CommandHP
const CommandBaseParam = battlecore.CommandBaseParam
const CommandBattleParam = battlecore.CommandBattleParam
const CommandWaitAndSee = battlecore.CommandWaitAndSee
const CommandContinue = battlecore.CommandContinue
const CommandContinueWait = battlecore.CommandContinueWait
const CommandCost = battlecore.CommandCost
const CommandEnableTarget = battlecore.CommandEnableTarget
const CommandCostBlock = battlecore.CommandCostBlock
const CommandCardDeal = battlecore.CommandCardDeal
const CommandCardDeck = battlecore.CommandCardDeck
const CommandCardPlay = battlecore.CommandCardPlay
const CommandCardIdentity = battlecore.CommandCardIdentity
const CommandCardWeak = battlecore.CommandCardWeak
const CommandCardUpdate = battlecore.CommandCardUpdate
const CommandCardCost = battlecore.CommandCardCost
const CommandCardPass = battlecore.CommandCardPass
const CommandCardState = battlecore.CommandCardState
const CommandStun = battlecore.CommandStun
const CommandRewrite = battlecore.CommandRewrite
const CommandRevive = battlecore.CommandRevive
const CommandBuff = battlecore.CommandBuff
const CommandPassiveBuff = battlecore.CommandPassiveBuff
const CommandHPCut = battlecore.CommandHPCut
const CommandDebuffFailed = battlecore.CommandDebuffFailed
const CommandBuffRelease = battlecore.CommandBuffRelease
const CommandBuffReleaseFailed = battlecore.CommandBuffReleaseFailed
const CommandSkillRevive = battlecore.CommandSkillRevive
const CommandBuffPartition = battlecore.CommandBuffPartition
const CommandBuffLostOne = battlecore.CommandBuffLostOne
const CommandChargeStart = battlecore.CommandChargeStart
const CommandPartsBreak = battlecore.CommandPartsBreak
const CommandEnemyBreak = battlecore.CommandEnemyBreak
const CommandGameOver = battlecore.CommandGameOver
const CommandEnemyFirstAttack = battlecore.CommandEnemyFirstAttack
const CommandAppendTurnAdd = battlecore.CommandAppendTurnAdd
const CommandHoldSet = battlecore.CommandHoldSet
const CommandHoldRemainder = battlecore.CommandHoldRemainder
const CommandHoldMax = battlecore.CommandHoldMax
const CommandHoldStore = battlecore.CommandHoldStore
const CommandHoldLost = battlecore.CommandHoldLost
const CommandAppendRelease = battlecore.CommandAppendRelease
const CommandBurstCondition = battlecore.CommandBurstCondition
const CommandBuddy = battlecore.CommandBuddy
const CommandBuddySlot = battlecore.CommandBuddySlot
const CommandBurstDiscard = battlecore.CommandBurstDiscard
const CommandAddCardBuff = battlecore.CommandAddCardBuff
const CommandCardBuff = battlecore.CommandCardBuff
const CommandAttackPartition = battlecore.CommandAttackPartition
const CommandLimitTurn = battlecore.CommandLimitTurn
const CommandBurstStateChange = battlecore.CommandBurstStateChange
const CommandBurstState = battlecore.CommandBurstState
const CommandBurstGaugeState = battlecore.CommandBurstGaugeState
const CommandBurstSkill = battlecore.CommandBurstSkill
const CommandEnemyAttackSign = battlecore.CommandEnemyAttackSign
const CommandEnemyAIVariables = battlecore.CommandEnemyAIVariables
const CommandPartsBreakDrop = battlecore.CommandPartsBreakDrop
const CommandEnemyBreakDrop = battlecore.CommandEnemyBreakDrop
const CommandPlayLogState = battlecore.CommandPlayLogState
const CommandHoldSkill = battlecore.CommandHoldSkill
const CommandHoldSkillEnd = battlecore.CommandHoldSkillEnd
const CommandResumeTurn = battlecore.CommandResumeTurn
const CommandResumeCardHand = battlecore.CommandResumeCardHand
const CommandResumeCardDeck = battlecore.CommandResumeCardDeck
const CommandResumeBuff = battlecore.CommandResumeBuff
const CommandResumeEnemy = battlecore.CommandResumeEnemy
const CommandResumeGameOver = battlecore.CommandResumeGameOver
const CommandResumeEnemyDrop = battlecore.CommandResumeEnemyDrop
const CommandResumeHaveDrop = battlecore.CommandResumeHaveDrop
const CommandResumeHold = battlecore.CommandResumeHold

type BattleResult = battlecore.BattleResult
type CombatCatalog = battlecore.CombatCatalog
type CombatSphereDefinition = battlecore.CombatSphereDefinition
type CombatBurstGaugeConfig = battlecore.CombatBurstGaugeConfig
type CombatBuddyDefinition = battlecore.CombatBuddyDefinition
type CombatBuddySkill = battlecore.CombatBuddySkill
type CombatRoleParameterRule = battlecore.CombatRoleParameterRule
type CombatCardDefinition = battlecore.CombatCardDefinition
type CombatSkillDefinition = battlecore.CombatSkillDefinition
type CombatSkillRole = battlecore.CombatSkillRole
type CombatEnemyParty = battlecore.CombatEnemyParty
type CombatEnemyPartySlot = battlecore.CombatEnemyPartySlot
type CombatEnemyDefinition = battlecore.CombatEnemyDefinition
type CombatEnemyLevel = battlecore.CombatEnemyLevel
type CombatEnemyAction = battlecore.CombatEnemyAction
type CombatEnemyAIOrder = battlecore.CombatEnemyAIOrder

func ArthurJob(arthurType int) string { return battlecore.ArthurJob(arthurType) }
func NativeClientUserParameters(member int, user [18]int32) (BattleResult, error) {
	return battlecore.NativeClientUserParameters(member, user, false)
}
func NativeClientSkillInfo(wire []byte) ([250]int64, error) {
	return battlecore.NativeClientSkillInfo(wire)
}

type cardPlaySubmission = battlecore.CardSelection
type burstSkillSubmission = battlecore.BurstSelection

const maxRoomMembers = battlecore.ParticipantCount
const deckSphereSlots = battlecore.SphereSlots
