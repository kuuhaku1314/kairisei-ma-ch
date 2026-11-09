package battlecore

import (
	"errors"
	"fmt"
	"math/bits"

	"kairisei.local/server/battlemodel"
)

// JP8 sizeof_SKILL_ROLE_KIND_BUFF, shared by execution/release/AI statistics.
const (
	skillRoleBuffKindCount   = 84
	skillRoleDebuffKindCount = 33
)

type BattleEngine struct {
	alchemist         bool
	pvp               bool
	enemyType         int
	summons           [10]battleSummon
	catalog           *CombatCatalog
	seed              uint32
	rng               xorShift128
	turn              int
	endTurn           int
	memberDeadEnd     bool // Original battle5_api_member_dead_end_set; SCORE mode.
	enemyStartTurn    int  // Visible turn at this encounter's start; AI orders use a local clock.
	elapsedWaveTurns  int  // 5d08e's total turn survives Start; 5d077's wave turn does not.
	costInitial       int
	costTurnOffset    int
	holdMax           int
	players           [4]battlePlayer
	opponents         *[4]battlePlayer // Allocated only for a PVP roster.
	enemies           [4]battleEnemy
	enemyCount        int
	phase             Phase
	selectedPlays     map[int]cardPlaySubmission
	selectionOrder    [8]int // Native selection records are appended in call order.
	npcSimulatedHP    [8]int // c6d6a: shared by candidate evaluations until the next UserPhase.
	npcSimulatedMaxHP [8]int
	enemyUses         map[int]int
	// Native AI trigger predicates can retain the matched battle member for a
	// later TRIGGER_TARGET action. It is captured per candidate in
	// selectEnemyAction so evaluating another condition cannot overwrite it.
	enemyTriggerTarget    int
	turnStats             battleTurnStats
	damageHistory         [29][]battleDamageEvent // Relative ages 1..29; current turn lives in turnStats.
	turnActions           []battleAction
	forceEndCheck         bool
	storyHelp             *battleStoryHelp
	explore               bool
	story                 bool
	storyEnd              StoryEndOrder
	endType               int
	continueAllowed       bool
	gameOverByCaller      bool
	continuePending       bool
	continueCount         int
	tokenCursor           int // Global rotating token slot; occupancy belongs to each player.
	nativeSkillSerial     int // 5d982: completed 7adb0 calls; card buffs share their action serial.
	waveRecovery          []BattleResult
	turnDrawn             [8][5]bool          // 5dd72 prepares hands in TurnPhase; UserPhase emits DEAL.
	resumeSide            int                 // Native 5d0a5; retained after a terminal action.
	skillTargets          *battleSkillTargets // Scoped to one 7adb0 invocation; nested death/CALL skills restore it.
	awakeTakeover         WaveTakeover
	initialDealPending    bool
	openingShufflePending bool // Pending first-battle shuffle, after Start applies all passives.
	deckShuffleOff        bool
	deckObserver          func(DeckOrderChange)
}

type Phase uint8

const (
	PhaseCreated Phase = iota
	PhaseStarted
	PhaseTurn
	PhaseUser
	PhaseUserAttack
	PhaseChaliceUser
	PhaseEnemy
	PhaseChaliceEnemy
	PhaseEnded
)

type battlePlayer struct {
	ExecutionSource       battleExecutionSource  // Retained native CALL lookup context.
	statusDisplay         battleStatusProjection // Last507/508 publication belongs to this actor.
	Alchemist             bool                   // Initial parameter ceilings belong to the battle mode.
	SummonSource          int                    // Source card1..10; summoned action cards are not physical hand cards.
	SummonTurn            int
	MemberType            int
	ExecutedBuffKinds     [skillRoleBuffKindCount]uint32 // d0889 reads this caster's successful good-status executions.
	TurnSkillStats        battleSkillStatistics
	PlayedCardTypes       [11]bool // CardData +2ffc: ordinary cards that have started executing in this battle.
	AppendExecutions      [2][13]battleAppendExecution
	AppendExecutionCounts [2]int
	AttributeUses         [10]uint32 // JP +859d8: ordinary action attributes, consumed by the next PVP turn.
	ArthurType            int
	JobType               int
	HP                    int
	MaxHP                 int
	BaseMaxHP             int
	Attack                int
	BaseAttack            int
	Magic                 int
	BaseMagic             int
	Recovery              int
	BaseRecovery          int
	Defense               int
	BaseDefense           int
	MDefense              int
	BaseMDefense          int
	LimitAttack           int
	LimitMagic            int
	LimitRecovery         int
	Cost                  int // Remaining selectable cost, not the native COST wire base.
	Burst                 int
	BurstState            int
	BurstBreak            int
	BurstRecharge         [3]int
	CardCostDown          [33]int
	CardCostIncreases     [33][]battleCardCostIncrease
	CardBurstSkills       [33][]battleCardBurstSkill
	DamageTaken           int
	TurnDamage            int
	BaseAttribute         string
	Attribute             string
	Deck                  [10]BattleCard
	SupportDeck           [10]BattleCard
	Spheres               [3]battleSphere
	Buddies               [5]BattleBuddy
	DeckOrder             [10]int // Native slots: zero-based Deck index, -1 for a null slot.
	DrawIndex             int     // Scan cursor; drawn slots before it are no longer in the pool.
	DrawCount             int     // Slot extent, not the number of non-null cards.
	ContinueDraw          bool    // Native +59ab0: refill hand once after a paid continuation.
	Discard               []int
	Hand                  [5]int // Deck slots1..10 or runtime token types23..32.
	Tokens                [10]battleToken
	TokenStock            [5]int
	// CardDisplay is the native per-CARD_TYPE cache compared by FUN_0003fa8a
	// before it projects ResultCmd314. It is presentation state owned by the
	// battle engine, not a reconstruction from previously emitted CSV rows.
	CardDisplay     [33]battleDisplayPower
	Effects         []battleEffect
	BlessHolds      []battleBlessHold
	CardHolds       []battleCardHold
	GameOver        bool
	ReservedChalice int
	// Native GameMasterTeamBattle stores eleven signed 64-bit hate buckets per
	// user. Bucket zero is the current turn; TurnPhase shifts older values one
	// slot and FUN_000576ec weights them 100,90,...,0 percent for HATE targets.
	HateHistory [11]int64
}

type battleSphere struct {
	Slot            int
	SphereID        int
	Level           int
	SkillLevels     [3]battlemodel.SkillLevel
	Type            string
	Count           int
	Maximum         int
	Playable        bool
	Remaining       int
	ChalicePlayable bool // cf186 post-selection gate, distinct from 309's turn condition.
	// Display is the FUN_00041a42 comparison cache; 315 sends power and marker.
	Display battleDisplayPower
}

type battleDisplayPower struct {
	Power  int
	Marker int
	Known  bool
}

type battleEnemy struct {
	statusDisplay        battleStatusProjection // A new wave starts with a new actor/cache.
	Trance               battleEnemyTrance
	StatusCooldown       [15]int
	MemberType           int
	EnemyID              int
	TribeIDs             []int
	Parent               int
	HP                   int
	MaxHP                int
	BaseMaxHP            int
	Attack               int
	BaseAttack           int
	Magic                int
	BaseMagic            int
	Recovery             int
	BaseRecovery         int
	Defense              int
	BaseDefense          int
	MDefense             int
	BaseMDefense         int
	DamageReduction      int
	AttributeFixed       [5]int
	LimitAttack          int
	LimitMagic           int
	LimitRecovery        int
	DamageTaken          int
	TurnDamage           int
	TurnPhysical         int
	TurnMagic            int
	AITurn               battleEnemyAITurnStats
	AIFlags              uint32   // 4cee1+13b24; preserved by TurnPhase's 56dea reset.
	AIVariables          [5]int32 // 4cee1+13b28; actor-local, preserved across turns.
	ExecutedBuffKinds    [skillRoleBuffKindCount]uint32
	TurnSkillStats       battleSkillStatistics
	DiedTurn             int
	DeathCount           int
	BaseAttribute        string
	Attribute            string
	Level                CombatEnemyLevel
	Awake                int
	Broken               bool
	SelfDestructed       bool // DESTRUCT death keeps the registered actor in the expiry sweep.
	PendingBreak         bool // 59b54 is consumed after all roles, even if a later heal restored HP.
	ActionConsumed       int
	ChargedActions       [5]enemyActionCandidate
	ChargedActionCount   int
	DeathActionTriggered bool
	Drops                []BattleDrop
	DropResolved         bool
	DropReleased         bool
	Effects              []battleEffect
}

// battleEffect is the durable state shared by the rule families in the
// official CN skill-role tables. Values remain typed here; ResultCmd rows are
// only a transport projection and are never used as the state itself.
type battleEffect struct {
	Function      string
	ListType      int
	Parameter     string
	Attribute     string
	DamageKind    string
	SkillKind     string
	CardType      int
	ProfileTags   [2]int
	CostMin       int
	CostMax       int
	CostCardType  int // NEED_COST_DOWN source-card filter; not a consumable CARD-list status.
	HateRate      int
	HateLimit     int
	Mask          int
	Value         int
	Delta         int
	Kind          int
	Rate          int
	Uses          int
	Parameters    [4]int
	Remaining     int
	AppliedTurn   int
	Source        int
	SourceSkillID int // Original SkillData identity; not caster or role-function ID.
	RoleIndex     int
	BurstPassive  bool
}

type battleTurnStats struct {
	Damage              int
	Physical            int
	Magic               int
	Enchant             int
	DamageHits          int
	DamageHitsByPhysics [3]int
	Heal                int
	HealReceived        [8]int64
	DrainByUser         [8]int64
	ExecutedAttributes  uint32
	MaxChain            int
	DamageByUser        [8]int
	PlayedByUser        [8]int
	KindsByUser         [8]map[string]int
	DamageEvents        []battleDamageEvent
}

// GameMaster enemy AI block, not the whole party's damage totals or the
// member's HP-loss history. Parent propagation and DOT use other counters.
type battleEnemyAITurnStats struct {
	MaxSkillDamage  [10][3]int64
	Damage          [3]int64 // PHYSICS, MAGIC, ALL (enchant/reflection)
	Hits            [3]int
	BadStatus       uint32
	DebuffKinds     uint32
	PlayerBuffKinds [skillRoleBuffKindCount]uint32
}

type battleDamageEvent struct {
	Target    int
	Source    int
	Value     int
	Attribute string
	Physics   string
	DOT       string
	Enchant   bool
	Special   bool // Trap/self-HP-cut/destruct/reflection: independent of attribute/physics/DOT filters.
}

// xorShift128 is the RNG family confirmed in libbattle5 FUN_000a8e57.
type xorShift128 struct {
	x uint32
	y uint32
	z uint32
	w uint32
}

func newXorShift128(seed uint32) xorShift128 {
	// libbattle5 FUN_000a8f54 seeds the four standard xor128 words from the
	// caller value, three rotations and the canonical Marsaglia constants.
	// Keep uint32 rotations explicit so seed zero retains the native constants.
	return xorShift128{
		x: seed ^ 0x075bcd15,
		y: bits.RotateLeft32(seed, 8) ^ 0x159a55e5,
		z: bits.RotateLeft32(seed, 16) ^ 0x1f123bb5,
		w: bits.RotateLeft32(seed, 24) ^ 0x05491333,
	}
}

func (random *xorShift128) next() uint32 {
	temporary := random.x ^ (random.x << 11)
	random.x, random.y, random.z = random.y, random.z, random.w
	random.w = random.w ^ (random.w >> 19) ^ temporary ^ (temporary >> 8)
	return random.w
}

func NewBattleEngine(catalog *CombatCatalog, spec Config, members []Participant) (*BattleEngine, error) {
	return newBattleEngine(catalog, spec, members, false)
}

// Candidate PVP construction shares player initialization; execution is under recovery.
func NewPVPBattleEngine(catalog *CombatCatalog, spec Config, members []Participant) (*BattleEngine, error) {
	return newBattleEngine(catalog, spec, members, true)
}

func newBattleEngine(catalog *CombatCatalog, spec Config, members []Participant, pvp bool) (*BattleEngine, error) {
	if catalog == nil {
		return nil, errors.New("combat catalog is unavailable")
	}
	memberLimit := maxRoomMembers
	if pvp {
		memberLimit *= 2
		if spec.Alchemist || spec.EnemyPartyID != 0 || spec.Encounter != nil || len(spec.Drops) != 0 {
			return nil, errors.New("PVP cannot also contain monster encounters")
		}
	}
	if (!spec.Alchemist && len(members) != memberLimit) || (spec.Alchemist && (len(members) != 1 || members[0].MemberType != 1 || members[0].ArthurType != 5)) {
		return nil, errors.New("combat party is incomplete")
	}
	party, exists := catalog.EnemyParties[spec.EnemyPartyID]
	if spec.Encounter != nil && (spec.EnemyPartyID != 0 || len(spec.Drops) != 0) {
		return nil, errors.New("direct encounter cannot also select a catalog party or separate drops")
	}
	if !pvp && spec.Encounter == nil && !exists {
		return nil, fmt.Errorf("enemy party %d is unavailable", spec.EnemyPartyID)
	}
	engine := &BattleEngine{
		alchemist: spec.Alchemist, deckShuffleOff: spec.DeckShuffleOff, explore: spec.Explore,
		enemyType: spec.EnemyType,
		catalog:   catalog, seed: uint32(spec.Seed), rng: newXorShift128(uint32(spec.Seed)),
		costInitial: spec.CostInitial, holdMax: spec.HoldMax, continueAllowed: spec.ContinueAllowed,
		phase: PhaseCreated, selectedPlays: make(map[int]cardPlaySubmission, maxRoomMembers),
		enemyUses: make(map[int]int), openingShufflePending: true, initialDealPending: true, awakeTakeover: spec.AwakeTakeover,
	}
	engine.pvp = pvp
	if pvp {
		engine.opponents = new([4]battlePlayer)
	}
	engine.endTurn = spec.EndTurn
	engine.memberDeadEnd = spec.MemberDeadEnd
	engine.gameOverByCaller = spec.GameOverByCaller
	engine.story, engine.storyEnd = spec.Story, spec.StoryEnd
	if spec.StoryHelp != nil {
		engine.storyHelp = &battleStoryHelp{Input: *spec.StoryHelp, Actor: battlePlayer{MemberType: storyHelpMember, MaxHP: 1, BaseMaxHP: 1, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999, Attribute: "NULL", BaseAttribute: "NULL"}}
	}
	for _, summon := range spec.Summons {
		if summon.CardType < 1 || summon.CardType > len(engine.summons) {
			return nil, errors.New("summon source card slot is invalid")
		}
		engine.summons[summon.CardType-1].Input = summon
	}
	seenMemberTypes := make(map[int]struct{}, maxRoomMembers)
	for _, member := range members {
		if len(member.DeckCards) != 10 {
			return nil, errors.New("combat member deck must contain ten cards")
		}
		if member.MemberType < 1 || member.MemberType > memberLimit {
			return nil, errors.New("combat member type is invalid")
		}
		if _, duplicate := seenMemberTypes[member.MemberType]; duplicate {
			return nil, errors.New("combat member type is duplicated")
		}
		seenMemberTypes[member.MemberType] = struct{}{}
		player := engine.player(member.MemberType)
		player.Alchemist = spec.Alchemist
		player.MemberType = member.MemberType
		player.ArthurType = member.ArthurType
		player.JobType = member.ArthurType
		if member.JobType != nil {
			player.JobType = *member.JobType
		}
		player.HP = member.HP
		player.MaxHP = member.HP
		player.BaseMaxHP = member.HP
		if member.MaxHP != nil {
			player.MaxHP, player.BaseMaxHP = *member.MaxHP, *member.MaxHP
		}
		player.Attack = member.Attack
		player.BaseAttack = member.Attack
		player.Magic = member.Magic
		player.BaseMagic = member.Magic
		player.Recovery = member.Mind
		player.BaseRecovery = member.Mind
		player.BaseDefense = player.Defense
		player.BaseMDefense = player.MDefense
		// battle5_api_user_set initializes member+0x44 to ATTR.NEUTRAL=9.
		player.BaseAttribute = "NEUTRAL"
		player.Attribute = player.BaseAttribute
		player.LimitAttack = initialParameterLimit(spec.Alchemist)
		player.LimitMagic = player.LimitAttack
		player.LimitRecovery = player.LimitAttack
		player.Cost = spec.CostInitial
		initialBurst := spec.BurstGaugeInitial
		if member.InitialBurst != nil {
			initialBurst = *member.InitialBurst
		}
		player.Burst = minInt(catalog.BurstGauge.Maximum, maxInt(0, initialBurst))
		for cardIndex, card := range member.DeckCards {
			definition, exists := catalog.Cards[card.CardID]
			if !exists {
				return nil, fmt.Errorf("combat card %d is unavailable", card.CardID)
			}
			var err error
			_, _, err = catalog.CardMainPassive(definition, player.ArthurType)
			if err != nil {
				return nil, err
			}
			player.Deck[cardIndex] = card
			player.DeckOrder[cardIndex] = cardIndex
		}
		if err := ValidateSupportCards(member.SupportCards); err != nil {
			return nil, err
		}
		for _, card := range member.SupportCards {
			if _, _, err := catalog.CardSupportSkill(card); err != nil {
				return nil, err
			}
			player.SupportDeck[card.CardType-11] = card
		}
		for _, sphere := range member.DeckSpheres {
			if sphere.SphereType < 1 || sphere.SphereType > len(player.Spheres) || sphere.SphereID <= 0 || sphere.Level <= 0 {
				return nil, errors.New("combat sphere slot is invalid")
			}
			if player.Spheres[sphere.SphereType-1].SphereID != 0 {
				return nil, errors.New("combat sphere slot is duplicated")
			}
			definition, exists := catalog.Spheres[sphere.SphereID]
			if !exists {
				return nil, fmt.Errorf("combat sphere %d is unavailable", sphere.SphereID)
			}
			if sphere.Level > definition.MaxLevel {
				return nil, fmt.Errorf("combat sphere %d level %d exceeds max level %d", sphere.SphereID, sphere.Level, definition.MaxLevel)
			}
			player.Spheres[sphere.SphereType-1] = battleSphere{
				Slot: sphere.SphereType, SphereID: sphere.SphereID, Level: sphere.Level,
				SkillLevels: sphere.SkillLevels,
				Type:        definition.Type, Count: definition.Count, Maximum: definition.Count,
				Display: battleDisplayPower{Known: pvp},
			}
		}
		for _, buddy := range member.DeckBuddies {
			if buddy.BuddyType < 1 || buddy.BuddyType > len(player.Buddies) || buddy.BuddyID <= 0 || buddy.Level <= 0 {
				return nil, errors.New("combat buddy slot is invalid")
			}
			if player.Buddies[buddy.BuddyType-1].BuddyID != 0 {
				return nil, errors.New("combat buddy slot is duplicated")
			}
			definition, exists := catalog.Buddies[buddy.BuddyID]
			if !exists {
				return nil, fmt.Errorf("combat buddy %d is unavailable", buddy.BuddyID)
			}
			if buddy.Level > definition.MaxLevel {
				return nil, fmt.Errorf("combat buddy %d level %d exceeds max level %d", buddy.BuddyID, buddy.Level, definition.MaxLevel)
			}
			player.Buddies[buddy.BuddyType-1] = buddy
		}
		if player.Buddies[0].BuddyID != 0 {
			player.BurstState = burstGaugeNormal
		} else {
			player.Burst = 0
		}
		player.DrawCount = len(player.DeckOrder)
	}
	if pvp {
		return engine, nil
	}
	var err error
	if spec.Encounter != nil {
		err = engine.loadEncounter(*spec.Encounter)
	} else {
		err = engine.loadEnemyParty(party, spec.Drops)
	}
	if err != nil {
		return nil, err
	}
	return engine, nil
}

func (engine *BattleEngine) loadEnemyParty(party CombatEnemyParty, drops []BattleDrop) error {
	var input Encounter
	for index, slot := range party.Slots {
		if slot.EnemyID == 0 {
			continue
		}
		definition, exists := engine.catalog.Enemies[slot.EnemyID]
		if !exists {
			return fmt.Errorf("enemy %d is unavailable", slot.EnemyID)
		}
		level, exists := engine.catalog.EnemyLevels[slot.EnemyID]
		if !exists {
			return fmt.Errorf("enemy level %d is unavailable", slot.EnemyID)
		}
		hp := definition.HP * slot.HPRate
		input.Enemies[index] = EnemyInput{Definition: definition, Level: level, HP: hp, MaxHP: hp, Parent: slot.ParentIndex}
	}
	for _, drop := range drops {
		if drop.EnemyIndex < 0 || drop.EnemyIndex >= len(input.Enemies) || input.Enemies[drop.EnemyIndex].Definition.ID == 0 {
			return fmt.Errorf("combat drop enemy slot %d is unavailable", drop.EnemyIndex)
		}
		input.Enemies[drop.EnemyIndex].Drops = append(input.Enemies[drop.EnemyIndex].Drops, drop)
	}
	return engine.loadEncounter(input)
}

func (engine *BattleEngine) shuffleDeck(player *battlePlayer) {
	// CardCollector<10>::shuffle (49ed8) always swaps ten native slots,
	// including null entries. Compacting the pool first changes both the card
	// order and the nine RNG words consumed by every ordinary reshuffle.
	for index := player.DrawCount; index < len(player.DeckOrder); index++ {
		player.DeckOrder[index] = -1
	}
	player.DrawCount, player.DrawIndex = len(player.DeckOrder), 0
	if engine.deckShuffleOff {
		return
	}
	for index := len(player.DeckOrder) - 1; index > 0; index-- {
		swapIndex := int(engine.rng.next() % uint32(index+1))
		player.DeckOrder[index], player.DeckOrder[swapIndex] = player.DeckOrder[swapIndex], player.DeckOrder[index]
	}
}

func (player *battlePlayer) remainingDeckCount() int {
	count := 0
	for _, slot := range player.DeckOrder[player.DrawIndex:player.DrawCount] {
		if slot >= 0 {
			count++
		}
	}
	return count
}

func (engine *BattleEngine) recycleDrawPool(player *battlePlayer) {
	// FUN_00049830 pops trash in order into the FIRST empty native slot.
	// Unconsumed cards retain their positions, including across waves.
	for index := range player.DeckOrder {
		if index < player.DrawIndex || index >= player.DrawCount {
			player.DeckOrder[index] = -1
		}
	}
	index := 0
	for _, slot := range player.Discard {
		if slot < 0 {
			continue
		}
		for index < len(player.DeckOrder) && player.DeckOrder[index] >= 0 {
			index++
		}
		if index < len(player.DeckOrder) {
			player.DeckOrder[index] = slot
		}
	}
	player.Discard = nil
	player.DrawCount = len(player.DeckOrder)
	engine.shuffleDeck(player)
	engine.notifyDeckOrder(player, false)
}

func (engine *BattleEngine) drawCard(player *battlePlayer) int {
	if !engine.alchemist && player.remainingDeckCount() == 0 && len(player.Discard) > 0 {
		engine.recycleDrawPool(player)
	}
	// FUN_00048d00 pops the first non-null slot without compacting others.
	for player.DrawIndex < player.DrawCount && player.DeckOrder[player.DrawIndex] < 0 {
		player.DrawIndex++
	}
	if player.DrawIndex == player.DrawCount {
		return 0
	}
	deckSlot := player.DeckOrder[player.DrawIndex] + 1
	player.DeckOrder[player.DrawIndex] = -1
	player.DrawIndex++
	return deckSlot
}
