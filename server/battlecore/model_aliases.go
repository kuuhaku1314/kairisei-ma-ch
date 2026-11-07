// Package battlecore provides the shared deterministic battle engine.
package battlecore

import "kairisei.local/server/battlemodel"

// Values shared with content and consumer adapters have one neutral owner.
// Public aliases keep engine callers independent of that storage detail.
type Config = battlemodel.Config
type StoryEndOrder = battlemodel.StoryEndOrder
type StoryHelpCard = battlemodel.StoryHelpCard
type WaveTakeover = battlemodel.WaveTakeover
type Encounter = battlemodel.Encounter
type EnemyInput = battlemodel.EnemyInput
type NPCRule = battlemodel.NPCRule
type NPCProgram = battlemodel.NPCProgram
type Participant = battlemodel.Participant
type Card = battlemodel.Card
type Summon = battlemodel.Summon
type Sphere = battlemodel.Sphere
type Buddy = battlemodel.Buddy
type Drop = battlemodel.Drop
type Definitions = battlemodel.Definitions
type CombatSphereDefinition = battlemodel.CombatSphereDefinition
type CombatBurstGaugeConfig = battlemodel.CombatBurstGaugeConfig
type CombatBuddyDefinition = battlemodel.CombatBuddyDefinition
type CombatBuddySkill = battlemodel.CombatBuddySkill
type CombatRoleParameterRule = battlemodel.CombatRoleParameterRule
type CombatCardDefinition = battlemodel.CombatCardDefinition
type CombatSkillDefinition = battlemodel.CombatSkillDefinition
type CombatSkillRole = battlemodel.CombatSkillRole
type CombatEnemyParty = battlemodel.CombatEnemyParty
type CombatEnemyPartySlot = battlemodel.CombatEnemyPartySlot
type CombatEnemyDefinition = battlemodel.CombatEnemyDefinition
type EnemyGaugeDefinition = battlemodel.EnemyGaugeDefinition
type EnemyGaugeReaction = battlemodel.EnemyGaugeReaction
type CombatEnemyLevel = battlemodel.CombatEnemyLevel
type CombatEnemyAction = battlemodel.CombatEnemyAction
type CombatEnemyAIOrder = battlemodel.CombatEnemyAIOrder
type Result = battlemodel.Result
type CardSelection = battlemodel.CardSelection
type BurstSelection = battlemodel.BurstSelection
type CombatantView = battlemodel.CombatantView
type StateView = battlemodel.StateView
type DeckOrderChange = battlemodel.DeckOrderChange
type EnemyPlanSnapshot = battlemodel.EnemyPlanSnapshot
type PlanReason = battlemodel.PlanReason
type EnemyActionPlanTrace = battlemodel.EnemyActionPlanTrace
type EnemyTurnPlan = battlemodel.EnemyTurnPlan

const ParticipantCount = battlemodel.ParticipantCount
const SphereSlots = battlemodel.SphereSlots
const PlanBroken = battlemodel.PlanBroken
const PlanDeathOnly = battlemodel.PlanDeathOnly
const PlanNotPreemptive = battlemodel.PlanNotPreemptive
const PlanExhausted = battlemodel.PlanExhausted
const PlanConditionFalse = battlemodel.PlanConditionFalse
const PlanOverBudget = battlemodel.PlanOverBudget
const PlanEligible = battlemodel.PlanEligible
const PlanSelected = battlemodel.PlanSelected
