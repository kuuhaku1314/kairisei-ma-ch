// Package battlemodel defines inputs owned by the shared battle library.
// Account, room, transport and reward-publication data belong to its callers.
package battlemodel

const ParticipantCount = 4
const SphereSlots = 3

// Config contains deterministic battle configuration. Content adapters supply
// the enemy party identity; callers decide whether continuation is permitted.
type Config struct {
	Alchemist         bool // ALCHEMIST/STORY_ALCHEMIST share this combat rule set.
	Summons           []Summon
	EnemyPartyID      int
	EnemyType         int        // Original start classification for internal NPC actions.
	Encounter         *Encounter // Direct combat inputs; mutually exclusive with EnemyPartyID.
	CostInitial       int
	HoldMax           int
	BurstGaugeInitial int
	Seed              uint32
	ContinueAllowed   bool
	GameOverByCaller  bool // The original client explicitly resolves retirement.
	EndTurn           int
	MemberDeadEnd     bool
	AwakeTakeover     WaveTakeover
	Explore           bool // EXPLORE settles all-KO defeat before caller retirement.
	Story             bool
	StoryEnd          StoryEndOrder
	StoryHelp         *StoryHelpCard
	DeckShuffleOff    bool
	Drops             []Drop
}

// StoryHelpCard is the independent neutral helper, not a party/deck slot.
type StoryHelpCard struct {
	CardID, SkillID, Level, Target int
	Turns                          [10]int16
}

// StoryEndOrder is the copied STORY2 script end condition; zero fields disable it.
type StoryEndOrder struct {
	Turn, EnemyHPPercent int
	UserHPZero           bool
}

// WaveTakeover is the original six independent EXPLORE handover settings.
type WaveTakeover struct {
	KeepTurn, KeepCost, KeepBuff, KeepSphereCount, KeepDeckTrash, KeepDeal bool
}

// Encounter supports clients and authoring tools which already resolved the
// enemy party. Each slot owns its current/max HP and action schedule, even when
// two slots share the same content identity. It carries no account or network.
type Encounter struct{ Enemies [4]EnemyInput }

type EnemyInput struct {
	Definition CombatEnemyDefinition
	Level      CombatEnemyLevel
	HP, MaxHP  int
	Parent     int
	Drops      []Drop
}

// Participant is a combatant, independent of an account or room membership.
// MemberType is the one-based battle slot; ArthurType selects the combat job.
type Participant struct {
	MemberType   int
	ArthurType   int
	JobType      *int // nil uses ArthurType; native NULL job is a distinct value.
	HP           int
	MaxHP        *int // nil initializes maximum HP from HP; registered input can differ.
	Attack       int
	Magic        int
	Mind         int
	InitialBurst *int // nil uses the encounter's common initial value.
	DeckCards    []Card
	SupportCards []Card
	DeckSpheres  []Sphere
	DeckBuddies  []Buddy
}

// Card identifies an owned deck slot and the content definition to evaluate.
// CardType remains distinct from CardID; support slots use the same identity.
type Card struct {
	CardType    int
	CardID      int
	Level       int
	Fame        int
	Love        int
	LoveRate    int
	HasLoveRate bool // Original registered CardData already contains a percentage.
	SkillLevels [6]SkillLevel
}

// Summon is one source-card registration. The action card is independent of
// that source card's ordinary skill, even when their displayed CardID matches.
type Summon struct {
	CardType int  `json:"card_type"`
	ID       int  `json:"id"`
	HP       int  `json:"hp"`
	Attack   int  `json:"attack"`
	Magic    int  `json:"magic"`
	Recovery int  `json:"recovery"`
	Defense  int  `json:"defense"`
	MDefense int  `json:"magic_defense"`
	Level    int  `json:"level"`
	Ability  int  `json:"ability"`
	Card     Card `json:"card"`
}

type Sphere struct {
	SphereType  int
	SphereID    int
	Level       int
	SkillLevels [3]SkillLevel
}

type Buddy struct {
	BuddyType   int
	BuddyID     int
	Level       int
	SkillLevels [7]SkillLevel
}

// SkillLevel separates the displayed owned-item level from the level of its
// registered executable skill. Zero is a valid explicitly supplied level.
type SkillLevel struct{ ID, Value int }

func executionLevel(level int, skills []SkillLevel, id int) int {
	for _, skill := range skills {
		if id != 0 && skill.ID == id {
			return skill.Value
		}
	}
	return level
}

func (card Card) ExecutionLevel(id int) int {
	return executionLevel(card.Level, card.SkillLevels[:], id)
}
func (sphere Sphere) ExecutionLevel(id int) int {
	return executionLevel(sphere.Level, sphere.SkillLevels[:], id)
}
func (buddy Buddy) ExecutionLevel(id int) int {
	return executionLevel(buddy.Level, buddy.SkillLevels[:], id)
}

// Drop is a predetermined battle presentation input. The engine releases it
// with the enemy event; granting inventory or currency remains outside it.
type Drop struct {
	EnemyIndex   int
	RewardType   int
	Num          int
	RewardTypeID int
}
