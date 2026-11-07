package battlemodel

// EnemyGaugeDefinition describes the per-enemy reaction-table contract.
// It is independent of the older global damage/debuff reaction rates. Content
// adapters select the actual source contract, not a region flag in handlers.
type EnemyGaugeDefinition struct {
	InitialState int
	InitialRate  int // Signed percent, not basis points.
	Reactions    []EnemyGaugeReaction
}

// Parameters use semantic names for skill kind, attribute and profession;
// costs, profile tags and native sentinel values remain decimal strings.
// The slice is the original valid-count prefix, including holes within it.
type EnemyGaugeReaction struct {
	Kind                   string
	Parameters             []string
	NormalRate, TranceRate int
	Targets                []string
}
