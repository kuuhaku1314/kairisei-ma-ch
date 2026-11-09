package multiplayer

import "kairisei.local/server/battlecore"

type ContinuePlan = battlecore.ContinuePlan
type ContractResultCodec = battlecore.ContractResultCodec

func ValidateCoreContracts(catalog *CombatCatalog, codec ContractResultCodec) error {
	return battlecore.ValidateCoreContracts(catalog, codec)
}
func ValidBattleDrop(drop BattleDrop) bool { return battlecore.ValidBattleDrop(drop) }

type BattleCard = battlecore.BattleCard
type BattleSphere = battlecore.BattleSphere
type BattleBuddy = battlecore.BattleBuddy
type BattleDrop = battlecore.BattleDrop

type WaveOutcome = battlecore.WaveOutcome

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

type cardPlaySubmission = battlecore.CardSelection
type burstSkillSubmission = battlecore.BurstSelection

const maxRoomMembers = battlecore.ParticipantCount
const deckSphereSlots = battlecore.SphereSlots
