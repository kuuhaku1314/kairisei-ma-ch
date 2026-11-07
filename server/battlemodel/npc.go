package battlemodel

// NPCRule is a content rule, independent of the original packed layout. The
// selector uses the canonical skill-selection vocabulary. Priority order is
// mercenary, millionaire, thief, singer, alchemist, null, matching the modern
// table's named fields. Parameters retain signed 64-bit values or enums.
type NPCRule struct {
	ID         int
	EnemyType  int
	Situation  string
	Parameters [5]string
	Selector   string
	Priority   [6]int
}

type NPCProgram struct{ Rules []NPCRule }
