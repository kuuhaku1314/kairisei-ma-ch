package battlemodel

// Definitions contains battle content projected by a content adapter. The
// caller finishes construction before sharing it with battles and must not
// mutate its maps or slices while a battle uses them. It owns no file paths,
// accounts, room state or parser behavior.
type Definitions struct {
	NPC         NPCProgram
	Cards       map[int]CombatCardDefinition
	Summons     map[int]CombatSummonDefinition
	Spheres     map[int]CombatSphereDefinition
	Buddies     map[int]CombatBuddyDefinition
	BurstGauge  CombatBurstGaugeConfig
	TranceRates [4][2][5]int
	// All source tables use the client's shared program and role namespaces.
	Skills     map[int][]CombatSkillDefinition
	SkillRoles map[int][]CombatSkillRole

	SupportLoveRates [3]int
	RoleParamRules   map[string]CombatRoleParameterRule
	EnemyParties     map[int]CombatEnemyParty
	Enemies          map[int]CombatEnemyDefinition
	EnemyLevels      map[int]CombatEnemyLevel
	EnemyAIOrders    map[int]CombatEnemyAIOrder
}

type CombatSphereDefinition struct {
	ID             int
	SameSphereID   int
	Name           string
	Type           string
	MaxLevel       int
	SkillID        int
	PassiveSkillID int
	CallSkillID    int
	Count          int
	PlayCondition  string
	PlayParam      int
}

type CombatBurstGaugeConfig struct {
	NormalThreshold int
	Maximum         int
	BreakTurns      int
	DamageReduction int
	ReactionRates   [5]int
}

type CombatBuddyDefinition struct {
	ID             int
	SameBuddyID    int
	Name           string
	Rarity         string
	MaxLevel       int
	PassiveSkillID int
	Skills         [3]CombatBuddySkill
}

type CombatBuddySkill struct {
	SkillID       int
	CallSkillID   int
	RequiredLevel int
	Genre         string
	GaugeCost     int
	RechargeTurn  int
}

// CombatRoleParameterRule preserves the role-wide flags and ten parameter
// types declared by skill_role_param_rule. CSV roles inherit these flags;
// registered native roles carry their own already-resolved values.
type CombatRoleParameterRule struct {
	Function      string
	PVPRate       int
	ValueBoostUse int
	Parameters    [10]string
}

type CombatCardDefinition struct {
	// CardCsvData.profile.group_ids, not SkillData's target-hand groups.
	ProfileTags          [8]int
	LoveMax              int
	SupportSkillIDs      [4]int
	ID                   int
	BaseCardID           int
	Rarity               string
	MaxLevel             int
	NormalSkillID        int
	ArthurSkillID        int
	CallSkillID          int
	PassiveSkillID       int
	ArthurPassiveSkillID int
	SummonID             int
	HasParameterFamilies bool // the source row declares the modern parameter columns
	RarityPlus           int
	// Original JP alchemist PARAM triples: initial, maximum and bonus, each
	// ordered HP, physical attack, magic attack and recovery. Storage only;
	// alchemist rule execution and parameter selection are engine concerns.
	AlchemistParameters [3][4]int
	HPBase              int
	HPGrowth            int
	AttackBase          int
	AttackGrowth        int
	MagicBase           int
	MagicGrowth         int
	RecoveryBase        int
	RecoveryGrowth      int
}

// CombatSummonDefinition retains the original summons.csv parameter and skill
// identity. Loading this definition does not enable summon battle submission.
type CombatSummonDefinition struct {
	ID         int
	Name       string
	Initial    [6]int32 // HP, physical attack, magic attack, recovery, defense, magic defense
	Maximum    [6]int32
	Ability    int // SUMMONS_ABILITY: NONE=0, QUICK=1
	UseSkillID int
}

type CombatSkillDefinition struct {
	Type              string // Original SKILL_TYPE selects the effect list independently of the entry point.
	ArthurJob         string // SkillData+20; distinct from its job_type at+16.
	DisplayRole       int    // skill_value_role_no, one-based 1..5; zero means no value
	ID                int
	Name              string
	Kind              string
	Attribute         string
	Job               string
	DamageKind        string
	Cost              int
	PriorityPVE       int
	PriorityPVP       int
	Rank              string
	HateRatio         int
	Target            string
	HandSelectCount   int
	HandAttribute     string
	HandKind          string
	HandMinCost       int
	HandMaxCost       int
	Groups            [3]int
	AppendTrigger     string
	AppendDuration    int
	AppendCondition   string
	AppendParameters  [5]string
	BranchCondition   string
	BranchParameters  [5]string
	BranchCondition2  string
	BranchParameters2 [5]string
	BranchPriority    int
	FunctionID        int
}

type CombatSkillRole struct {
	SourceSkillID int // Bound on an execution copy; immutable CSV SkillID is the role-set key.
	SkillID       int
	RoleIndex     int
	Effect2D      string
	Effect3D      string
	HitEffect     string
	HitPosition   string
	Function      string
	Target        string
	ExcludeSelf   bool
	Attributes    [9]bool
	// CSV masks are explicit, including an empty mask. Programmatic operator
	// roles without a target rule leave this false (not a native NULL mask).
	HasTargetAttributes bool
	Parameters          [10]string
	ChainRate           int
	HateLimit           int
	PVPRate             int
	ValueBoostUse       int
}

type CombatEnemyParty struct {
	ID        int
	Placement string
	Slots     [4]CombatEnemyPartySlot
	BGMID     int
	FieldID   int
	HintID    int
}

type CombatEnemyPartySlot struct {
	EnemyID     int
	HPRate      int
	ParentIndex int
}

type CombatEnemyDefinition struct {
	// Original modern master identity/initial display fields. Reaction rules
	// are resolved separately before this definition can enter a battle.
	GaugeReactionID   int
	InitialGaugeState int
	InitialGaugeRate  int
	TranceLimit       int
	OverheatLimit     int
	OverheatTurns     int
	OverheatResist    int
	ReactiveGauge     *EnemyGaugeDefinition
	ID                int
	// One explicit CN slot or up to three JP slots. Zero and duplicate slots
	// are retained; adapters must not invent extra zero memberships.
	TribeIDs        []int
	ModelID         string
	ImageID         int
	Live2DName      string
	Name            string
	Attribute       string
	HP              int
	Attack          int
	Magic           int
	Recovery        int
	Defense         int
	MagicDefense    int
	DamageReduction int
	Size            string
	AttributeFixed  [5]int
}

type CombatEnemyLevel struct {
	ID             int
	HPBars         int
	AttributeRates [9]int
	// Indexed by native BAD_STATUS, including NULL=0. CSV has only 1..11.
	StatusResistances [15]int
	DOTReductions     [5]int
	InitialAwake      int
	ActionsPerTurn    int
	PassiveSkillID    int
	CallSkillIDs      [10]int
	CallSkillLevels   [10]SkillLevel
	GaugeActions      [3]CombatEnemyAction // NORMAL, TRANCE, OVER_HEAT transition skills.
	Actions           []CombatEnemyAction
}

type CombatEnemyAction struct {
	Slot          int
	Category      string
	SkillID       int
	AIConditionID int
	Order         *CombatEnemyAIOrder // Resolved condition for directly registered actions.
	Priority      int
	Target        string
	// EnemySkillData.target_param0..4 are Int64 after managed conversion, but
	// their CSV contract is symbolic for selectors such as USER_DEBUFF
	// (DARKNESS/CARD_TRAP_DAMAGE/WEAKNESS). Keep the source text and parse it
	// only in selectors whose native contract is numeric.
	TargetParams [5]string
	ActionCost   int
	MaxUses      int
	Rate         int
	CountOnMiss  bool
}

type CombatEnemyAIOrder struct {
	ID     int
	Fields []string
}
