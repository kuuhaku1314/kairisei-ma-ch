package battlecore

import (
	"errors"
	"math"
)

// StandaloneBattleConfig contains battle inputs only. Room credentials, user
// identity, reward publication and persistence remain in the server adapters.
type StandaloneBattleConfig struct {
	AwakeTakeover     WaveTakeover   `json:"-"`
	Explore           bool           `json:"-"`
	Story             bool           `json:"-"`
	StoryHelp         *StoryHelpCard `json:"-"`
	StoryEnd          StoryEndOrder  `json:"-"`
	DeckShuffleOff    bool           `json:"-"`
	Alchemist         bool           `json:"alchemist,omitempty"`
	Summons           []Summon       `json:"summons,omitempty"`
	EnemyPartyID      int            `json:"enemy_party_id"`
	EnemyType         int            `json:"-"`
	Encounter         *Encounter     `json:"-"`
	CostInitial       int            `json:"cost_initial"`
	HoldMax           int            `json:"hold_max"`
	BurstGaugeInitial int            `json:"burst_gauge_initial"`
	Seed              uint32         `json:"seed"`
	ContinueAllowed   bool           `json:"continue_allowed"`
	GameOverByCaller  bool           `json:"-"`
	EndTurn           int            `json:"end_turn,omitempty"`
	MemberDeadEnd     bool           `json:"member_dead_end,omitempty"`
	Drops             []BattleDrop   `json:"drops,omitempty"`
}

type StandaloneBattleMember struct {
	MemberType   int            `json:"member_type"`
	ArthurType   int            `json:"arthur_type"`
	JobType      *int           `json:"job_type,omitempty"`
	HP           int            `json:"hp"`
	MaxHP        *int           `json:"max_hp,omitempty"`
	Attack       int            `json:"attack"`
	Magic        int            `json:"magic"`
	Mind         int            `json:"mind"`
	InitialBurst *int           `json:"initial_burst,omitempty"`
	DeckCards    []BattleCard   `json:"cards"`
	SupportCards []BattleCard   `json:"support_cards,omitempty"`
	DeckSpheres  []BattleSphere `json:"spheres,omitempty"`
	DeckBuddies  []BattleBuddy  `json:"buddies,omitempty"`
}

type StandaloneCardPlay struct {
	MemberType      int    `json:"member_type"`
	CardTypes       [5]int `json:"card_types"`
	Targets         [5]int `json:"targets"`
	SphereSlot      int    `json:"sphere_slot"`
	SphereTarget    int    `json:"sphere_target"`
	SummonsCardMask int    `json:"summons_card_idx_bit"`
}

// StandaloneBattle delegates to the same engine used by rooms. The caller owns
// its lifetime and serializes actions; this object has no listener or Hub.
type StandaloneBattle struct{ engine *BattleEngine }

func NewStandaloneBattle(catalog *CombatCatalog, config StandaloneBattleConfig, inputs []StandaloneBattleMember) (*StandaloneBattle, error) {
	return newStandaloneBattle(catalog, config, inputs, false)
}

// NewStandalonePVPBattle uses the same input mapping and instance as ordinary
// standalone battles, with both complete player parties and no monster encounter.
func NewStandalonePVPBattle(catalog *CombatCatalog, config StandaloneBattleConfig, inputs []StandaloneBattleMember) (*StandaloneBattle, error) {
	return newStandaloneBattle(catalog, config, inputs, true)
}

func newStandaloneBattle(catalog *CombatCatalog, config StandaloneBattleConfig, inputs []StandaloneBattleMember, pvp bool) (*StandaloneBattle, error) {
	if !pvp && ((config.EnemyPartyID <= 0 && config.Encounter == nil) || (config.EnemyPartyID != 0 && config.Encounter != nil)) || config.CostInitial < 0 || config.CostInitial > 10 || config.HoldMax < 0 || config.HoldMax > maxHoldEntries || config.BurstGaugeInitial < 0 {
		return nil, errors.New("standalone battle configuration is invalid")
	}
	memberCount := maxRoomMembers
	if pvp {
		memberCount *= 2
	}
	if (pvp || !config.Alchemist) && len(inputs) != memberCount {
		return nil, errors.New("standalone battle party size is invalid")
	}
	if catalog == nil {
		return nil, errors.New("standalone combat catalog is unavailable")
	}
	members := make([]Participant, len(inputs))
	for index, input := range inputs {
		if input.ArthurType < 0 || input.ArthurType > 5 || input.HP < 0 || len(input.DeckCards) != 10 || len(input.SupportCards) > 10 || len(input.DeckSpheres) > 3 || len(input.DeckBuddies) > 5 {
			return nil, errors.New("standalone battle member input is invalid")
		}
		for _, value := range []int{input.HP, input.Attack, input.Magic, input.Mind} {
			if value < math.MinInt32 || value > math.MaxInt32 {
				return nil, errors.New("standalone member parameter exceeds native int32")
			}
		}
		if input.MaxHP != nil && (*input.MaxHP < 0 || *input.MaxHP > math.MaxInt32) {
			return nil, errors.New("standalone maximum HP is invalid")
		}
		for slot, card := range input.DeckCards {
			definition, exists := catalog.Cards[card.CardID]
			if !exists || card.CardType != slot+1 || card.Level < 1 || card.Level > definition.MaxLevel || card.Love < 0 || card.Love > definition.LoveMax {
				return nil, errors.New("standalone battle card identity/level is invalid")
			}
		}
		members[index] = Participant{
			MemberType: input.MemberType, ArthurType: input.ArthurType, JobType: input.JobType,
			HP: input.HP, MaxHP: input.MaxHP, Attack: input.Attack, Magic: input.Magic, Mind: input.Mind, InitialBurst: input.InitialBurst,
			DeckCards: append([]BattleCard(nil), input.DeckCards...), SupportCards: append([]BattleCard(nil), input.SupportCards...),
			DeckSpheres: append([]BattleSphere(nil), input.DeckSpheres...), DeckBuddies: append([]BattleBuddy(nil), input.DeckBuddies...),
		}
	}
	create := NewBattleEngine
	if pvp {
		create = NewPVPBattleEngine
	}
	engine, err := create(catalog, Config{
		Explore: config.Explore, Story: config.Story, StoryEnd: config.StoryEnd, StoryHelp: config.StoryHelp, DeckShuffleOff: config.DeckShuffleOff, Alchemist: config.Alchemist, Summons: config.Summons, EnemyType: config.EnemyType, AwakeTakeover: config.AwakeTakeover,
		EnemyPartyID: config.EnemyPartyID, CostInitial: config.CostInitial, HoldMax: config.HoldMax,
		Encounter:         config.Encounter,
		BurstGaugeInitial: config.BurstGaugeInitial, Seed: config.Seed, ContinueAllowed: config.ContinueAllowed, GameOverByCaller: config.GameOverByCaller, EndTurn: config.EndTurn, MemberDeadEnd: config.MemberDeadEnd,
		Drops: append([]BattleDrop(nil), config.Drops...),
	}, members)
	if err != nil {
		return nil, err
	}
	return &StandaloneBattle{engine: engine}, nil
}

// NextEncounter preserves the party and random stream across a registered wave.
func (battle *StandaloneBattle) NextEncounter(catalog *CombatCatalog, encounter Encounter, policy WaveTakeover) (*StandaloneBattle, error) {
	next, err := battle.engine.NextEncounter(catalog, encounter, policy)
	if err != nil {
		return nil, err
	}
	return &StandaloneBattle{engine: next}, nil
}

func (battle *StandaloneBattle) Start() ([]BattleResult, error)     { return battle.engine.Start() }
func (battle *StandaloneBattle) TurnPhase() ([]BattleResult, error) { return battle.engine.TurnPhase() }
func (battle *StandaloneBattle) UserPhase() ([]BattleResult, error) { return battle.engine.UserPhase() }
func (battle *StandaloneBattle) UserAttack() ([]BattleResult, error) {
	return battle.engine.UserAttack()
}

func (battle *StandaloneBattle) SummonExecutionCounts() [10]int16 {
	return battle.engine.SummonExecutionCounts()
}

func (battle *StandaloneBattle) ScoreDamage() int64 { return battle.engine.ScoreDamage() }
func (battle *StandaloneBattle) EnemyPhase() ([]BattleResult, error) {
	return battle.engine.EnemyPhase()
}
func (battle *StandaloneBattle) ChaliceUserPhase() ([]BattleResult, error) {
	return battle.engine.ExecuteChaliceUserPhase()
}
func (battle *StandaloneBattle) ChaliceEnemyPhase() ([]BattleResult, error) {
	return battle.engine.ExecuteChaliceEnemyPhase()
}
func (battle *StandaloneBattle) ReserveChaliceSphere(member, slot int) ([]BattleResult, error) {
	return battle.engine.ReserveChaliceSphere(member, slot)
}
func (battle *StandaloneBattle) ExecuteBurst(member int, selection BurstSelection) ([]BattleResult, error) {
	return battle.engine.ExecuteBurst(member, selection)
}
func (battle *StandaloneBattle) ResumeResults() ([]BattleResult, error) {
	return battle.engine.ResumeResults()
}
func (battle *StandaloneBattle) GameEndResults(member int) ([]BattleResult, error) {
	return battle.engine.GameEndResults(member)
}
func (battle *StandaloneBattle) EndType() int         { return battle.engine.EndType() }
func (battle *StandaloneBattle) StateView() StateView { return battle.engine.StateView() }
func (battle *StandaloneBattle) Selection(member int) (CardSelection, bool) {
	return battle.engine.Selection(member)
}
func (battle *StandaloneBattle) SetDeckOrderObserver(observer func(DeckOrderChange)) {
	battle.engine.SetDeckOrderObserver(observer)
}
func (battle *StandaloneBattle) GameOver(member int) ([]BattleResult, error) {
	return battle.engine.GameOver(member)
}

// Continue applies a caller-authorized revival. The original managed callback
// invokes this only after its HTTP payment succeeds (or an advertised free
// continue); account authorization never belongs to the local combat library.
func (battle *StandaloneBattle) Continue(member int) ([]BattleResult, error) {
	if !battle.engine.continueAllowed {
		return nil, errors.New("continuation is disabled for this battle")
	}
	plan, err := battle.engine.PrepareContinue(member)
	if err != nil {
		return nil, err
	}
	rows := plan.Results()
	if err := plan.Commit(); err != nil {
		return nil, err
	}
	return rows, nil
}

// ContinueDataGet is requested after the revival direction completes. Original
// CN and JP APIs emit the same snapshot as resume_data_get with no prior drops;
// it restores display state without replaying attacks or advancing the turn.
func (battle *StandaloneBattle) ContinueDataGet() ([]BattleResult, error) {
	return battle.engine.ResumeResults()
}

// SubmitNPC consumes the same typed rule program and planner as room selection.
func (battle *StandaloneBattle) SubmitNPC(member, enemyType int, program NPCProgram) ([]BattleResult, error) {
	return battle.engine.SubmitNPC(member, enemyType, program)
}

// Submit models mutable native user_card_play input; the room's once-per-phase
// Submit contract remains at the existing core/room submission boundary.
func (battle *StandaloneBattle) Submit(input StandaloneCardPlay) ([]BattleResult, error) {
	var rows []BattleResult
	err := battle.engine.WithCardSelections(func(batch *CardSelectionBatch) error {
		if err := batch.view.resetCardSubmission(input.MemberType); err != nil {
			return err
		}
		var err error
		rows, err = batch.Submit(input.MemberType, cardPlaySubmission{
			CardTypes: input.CardTypes, Targets: input.Targets, SphereSlot: input.SphereSlot, SphereTarget: input.SphereTarget,
			SummonsCardMask: input.SummonsCardMask,
		})
		return err
	})
	return rows, err
}
