package battlemodel

// EnemyPlanSnapshot is an explicit diagnostic situation, not a saved battle.
// Each snapshot supplies a turn, enemy HP percentages and native AI state.
type EnemyPlanSnapshot struct {
	Turn      int
	HP        [4]int
	Flags     [4]uint32
	Variables [4][5]int32
	Damage    [4][3]int64
}

type PlanReason uint8

const (
	PlanBroken PlanReason = iota + 1
	PlanDeathOnly
	PlanNotPreemptive
	PlanExhausted
	PlanConditionFalse
	PlanOverBudget
	PlanEligible
	PlanSelected
)

// EnemyActionPlanTrace records scheduler facts. Consumers add display names
// and localized descriptions; the trace owns no live engine references.
type EnemyActionPlanTrace struct {
	Actor     int
	EnemyID   int
	Slot      int
	Skill     int
	Category  string
	Condition bool
	Selected  int
	Priority  int
	Cost      int
	Uses      int
	MaxUses   int
	Reason    PlanReason
}

type EnemyTurnPlan struct {
	Turn   int
	Checks []EnemyActionPlanTrace
	Order  []EnemyActionPlanTrace
}
