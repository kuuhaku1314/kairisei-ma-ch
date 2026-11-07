// Command kairi-boss-guide-export is a read-only offline helper for the player
// BOSS guide generator (tools/python/export_cn_boss_guides.py).
//
// It loads the battle masters through the shared content parser and catalog
// validation, exports the typed enemy definitions reachable from the requested
// enemy parties, and drives the production battle phases for a fixed baseline
// in which every player passes. The baseline records which enemy skills the
// engine actually executes per wave-local turn; it is a diagnostic scenario,
// not a recommended play or a full combat simulation. Nothing is written except
// the requested new output file.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"kairisei.local/server/battlecontent"
	"kairisei.local/server/battlecore"
	"kairisei.local/server/battlemodel"
)

const (
	maxParties = 20000
	maxSeeds   = 64
	maxTurns   = 40
)

type request struct {
	Parties []int    `json:"parties"`
	Seeds   []uint32 `json:"seeds"`
	Turns   int      `json:"turns"`
}

type definitionsExport struct {
	Parties    map[int]battlemodel.CombatEnemyParty           `json:"parties"`
	Enemies    map[int]battlemodel.CombatEnemyDefinition      `json:"enemies"`
	Levels     map[int]battlemodel.CombatEnemyLevel           `json:"levels"`
	AIOrders   map[int]battlemodel.CombatEnemyAIOrder         `json:"ai_orders"`
	Skills     map[int][]battlemodel.CombatSkillDefinition    `json:"skills"`
	Roles      map[int][]battlemodel.CombatSkillRole          `json:"roles"`
	Rules      map[string]battlemodel.CombatRoleParameterRule `json:"role_parameter_rules"`
	SkillLevel map[string]int                                 `json:"enemy_skill_level"`
	// Global CN heat reactions [kind][state][cost-1] in percent of the current
	// limit, and the player Burst gauge contract read by enemy conditions.
	TranceRates [4][2][5]int                       `json:"trance_rates"`
	BurstGauge  battlemodel.CombatBurstGaugeConfig `json:"burst_gauge"`
	Missing     []string                           `json:"missing"`
}

// skillUse is one executed enemy skill header (ResultCmd 50) or charge
// announcement (ResultCmd 74), keyed by the selected branch's function ID.
type skillUse struct {
	Actor    int    `json:"actor"`
	Skill    int    `json:"skill"`
	Function int    `json:"function"`
	Passive  bool   `json:"passive,omitempty"`
	Phase    string `json:"phase"`
}

// turnRecord is one seed's observation of a wave-local turn. Turn zero holds
// the battle start (enemy passives) and the first attack.
type turnRecord struct {
	turn      int
	uses      []skillUse
	charges   []skillUse
	breaks    []int
	flags     []uint32
	variables [][5]int32
	enemyHP   []int
	knockouts int
	revives   int
	wiped     bool // every member was down at a phase boundary; later queued actions had no target
	ended     bool
	endType   int
}

type countedUse struct {
	skillUse
	Runs  int `json:"runs"`  // seeds in which it occurred at least once
	Count int `json:"count"` // total occurrences over all seeds
}

type variableRange struct {
	Min int32 `json:"min"`
	Max int32 `json:"max"`
}

type turnSummary struct {
	Turn int `json:"turn"`
	// Runs is the number of seeds that reached this turn without an engine error.
	Runs      int                `json:"runs"`
	Uses      []countedUse       `json:"uses"`
	Charges   []countedUse       `json:"charges,omitempty"`
	FlagRuns  []map[int]int      `json:"flag_runs"` // per enemy slot: bit -> seeds with it set after the turn
	Variables [][5]variableRange `json:"variables"`
	EnemyHP   [][2]int           `json:"enemy_hp"` // per slot: min/max HP after the turn
	Breaks    map[int]int        `json:"breaks,omitempty"`
	Knockouts int                `json:"knockouts,omitempty"`
	Wiped     int                `json:"wiped,omitempty"` // seeds excluded from Runs because the party was wiped
	Revives   int                `json:"revives,omitempty"`
	Ended     map[int]int        `json:"ended,omitempty"` // end type -> seeds
	Sample    []skillUse         `json:"sample"`          // ordered uses of the first seed
	SampleCh  []skillUse         `json:"sample_charges,omitempty"`
	index     map[skillUse]int
}

type partyBaseline struct {
	Party  int           `json:"party"`
	Seeds  int           `json:"seeds"`
	Errors []string      `json:"errors,omitempty"`
	Turns  []turnSummary `json:"turns"`
}

type output struct {
	Tool        string                `json:"tool"`
	Schema      int                   `json:"schema_version"`
	Scenario    string                `json:"baseline_scenario"`
	Turns       int                   `json:"turns"`
	Seeds       []uint32              `json:"seeds"`
	Definitions definitionsExport     `json:"definitions"`
	Baselines   map[int]partyBaseline `json:"baselines"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("kairi-boss-guide-export", flag.ContinueOnError)
	cardMaster := flags.String("card-master", "", "configured card.csv")
	battleMaster := flags.String("battle-master", "", "configured battle master directory")
	requestPath := flags.String("request", "", "JSON request written by the guide generator")
	outputPath := flags.String("output", "", "new JSON output file")
	workers := flags.Int("workers", runtime.NumCPU(), "parallel baseline workers")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *cardMaster == "" || *battleMaster == "" || *requestPath == "" || *outputPath == "" {
		return errors.New("-card-master, -battle-master, -request and -output are required")
	}
	if _, err := os.Stat(*outputPath); err == nil || !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("output already exists or is not checkable: %s", *outputPath)
	}
	raw, err := os.ReadFile(*requestPath)
	if err != nil {
		return err
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if len(req.Parties) == 0 || len(req.Parties) > maxParties || len(req.Seeds) == 0 || len(req.Seeds) > maxSeeds || req.Turns < 1 || req.Turns > maxTurns {
		return errors.New("request needs 1..20000 parties, 1..64 seeds and 1..40 turns")
	}
	definitions, err := battlecontent.LoadDefinitions(*cardMaster, *battleMaster)
	if err != nil {
		return err
	}
	catalog, err := battlecore.NewCatalog(definitions)
	if err != nil {
		return err
	}
	export := collectDefinitions(catalog, req.Parties)
	squad, err := idleSquad(catalog)
	if err != nil {
		return err
	}
	// The baseline runs on a separate in-memory copy whose enemy damage values
	// are reduced to the engine minimum, so the idle party is not wiped and the
	// whole schedule stays observable. Exported definitions are unmodified.
	simulation, err := battlecontent.LoadDefinitions(*cardMaster, *battleMaster)
	if err != nil {
		return err
	}
	neutralizeEnemyDamage(simulation)
	simulationCatalog, err := battlecore.NewCatalog(simulation)
	if err != nil {
		return err
	}
	baselines := simulateAll(simulationCatalog, squad, req, max(1, *workers))
	result := output{
		Tool: "kairi-boss-guide-export", Schema: 1,
		Scenario: "four members pass every turn and deal no damage; enemy damage values are reduced to the engine minimum in an in-memory copy so the party is not wiped; a knocked-out member is still revived through the engine continue flow",
		Turns:    req.Turns, Seeds: req.Seeds, Definitions: export, Baselines: baselines,
	}
	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(*outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(file).Encode(result); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// collectDefinitions exports exactly what the requested parties reach: their
// enemies, level rows, AI orders, action/gauge/passive/CALL skills and the
// role sets selected by each skill variant's function ID.
func collectDefinitions(catalog *battlecore.CombatCatalog, parties []int) definitionsExport {
	export := definitionsExport{
		Parties: map[int]battlemodel.CombatEnemyParty{}, Enemies: map[int]battlemodel.CombatEnemyDefinition{},
		Levels: map[int]battlemodel.CombatEnemyLevel{}, AIOrders: map[int]battlemodel.CombatEnemyAIOrder{},
		Skills: map[int][]battlemodel.CombatSkillDefinition{}, Roles: map[int][]battlemodel.CombatSkillRole{},
		Rules: map[string]battlemodel.CombatRoleParameterRule{}, SkillLevel: map[string]int{},
		TranceRates: catalog.TranceRates, BurstGauge: catalog.BurstGauge,
	}
	missing := map[string]struct{}{}
	note := func(format string, values ...any) { missing[fmt.Sprintf(format, values...)] = struct{}{} }
	addSkill := func(id int) {
		if id <= 0 {
			return
		}
		if _, done := export.Skills[id]; done {
			return
		}
		variants, ok := catalog.Skills[id]
		if !ok {
			note("skill %d", id)
			return
		}
		export.Skills[id] = variants
		for _, variant := range variants {
			roles, ok := catalog.SkillRoles[variant.FunctionID]
			if !ok {
				note("roles %d (skill %d)", variant.FunctionID, id)
				continue
			}
			export.Roles[variant.FunctionID] = roles
			for _, role := range roles {
				if rule, ok := catalog.RoleParamRules[role.Function]; ok {
					export.Rules[role.Function] = rule
				}
				export.SkillLevel[role.Function] = battlecore.EnemySkillLevel(role.Function)
			}
		}
	}
	for _, partyID := range parties {
		party, ok := catalog.EnemyParties[partyID]
		if !ok {
			note("party %d", partyID)
			continue
		}
		export.Parties[partyID] = party
		for _, slot := range party.Slots {
			if slot.EnemyID == 0 {
				continue
			}
			enemy, ok := catalog.Enemies[slot.EnemyID]
			level, levelOK := catalog.EnemyLevels[slot.EnemyID]
			if !ok || !levelOK {
				note("enemy %d", slot.EnemyID)
				continue
			}
			export.Enemies[slot.EnemyID] = enemy
			export.Levels[slot.EnemyID] = level
			addSkill(level.PassiveSkillID)
			for _, id := range level.CallSkillIDs {
				addSkill(id)
			}
			for _, action := range level.GaugeActions {
				addSkill(action.SkillID)
			}
			for _, action := range level.Actions {
				addSkill(action.SkillID)
				if action.AIConditionID == 0 {
					continue
				}
				if order, ok := catalog.EnemyAIOrders[action.AIConditionID]; ok {
					export.AIOrders[action.AIConditionID] = order
				} else {
					note("ai order %d", action.AIConditionID)
				}
			}
		}
	}
	for key := range missing {
		export.Missing = append(export.Missing, key)
	}
	sort.Strings(export.Missing)
	return export
}

// neutralizeEnemyDamage zeroes the power inputs of enemy damage roles in a
// simulation-only copy, plus the player debuffs that would amplify a minimum
// hit (negative defense, fixed attribute-defense additions, HP cuts). Each hit
// then deals the engine's minimum of one; the scheduler, conditions, branches,
// status registration and flags are untouched.
func neutralizeEnemyDamage(definitions *battlemodel.Definitions) {
	functions := map[int]struct{}{}
	addSkill := func(id int) {
		for _, variant := range definitions.Skills[id] {
			functions[variant.FunctionID] = struct{}{}
		}
	}
	for _, level := range definitions.EnemyLevels {
		addSkill(level.PassiveSkillID)
		for _, id := range level.CallSkillIDs {
			addSkill(id)
		}
		for _, action := range level.GaugeActions {
			addSkill(action.SkillID)
		}
		for _, action := range level.Actions {
			addSkill(action.SkillID)
		}
	}
	zero := func(role *battlemodel.CombatSkillRole, indexes ...int) {
		for _, index := range indexes {
			role.Parameters[index] = "0"
		}
	}
	for function := range functions {
		roles := definitions.SkillRoles[function]
		for i := range roles {
			switch roles[i].Function {
			case "ATTACK_AA", "ATK_OP_DAMAGE_INCREASE":
				zero(&roles[i], 0, 1, 2, 3)
			case "ATK_OP_REVENGE", "ATK_OP_NOW_TURN_REVENGE":
				zero(&roles[i], 0, 1)
			case "POISON", "BURN", "FREEZE", "BLEED", "ELECTRIC", "CARD_TRAP_DAMAGE":
				zero(&roles[i], 3, 4, 5, 6)
			case "ENCHANT":
				zero(&roles[i], 1)
			case "HP_CUT":
				zero(&roles[i], 0)
			case "DOT_VALUE_UP":
				// Keeps the turn extension; the rate and fixed additions stack per use.
				zero(&roles[i], 1, 2, 3, 4)
			case "ATTR_DEF_DOWN":
				// Its fixed segment is added to every hit after defense.
				zero(&roles[i], 1, 2, 3, 4)
			case "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED", "ATK_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM",
				"ATK_BREAK_BY_TARGET_PARAM", "GUARD_BREAK_BY_TARGET_PARAM":
				// Negative player defense turns a minimum hit into a large one.
				zero(&roles[i], 3, 4, 5)
			case "ATK_BREAK_BY_NOW_TURN_DAMAGE", "GUARD_BREAK_BY_NOW_TURN_DAMAGE":
				zero(&roles[i], 2, 3)
			case "ATK_BREAK_FIXED_ENEMY", "GUARD_BREAK_FIXED_ENEMY":
				roles[i].Parameters[2] = "1"
			}
		}
	}
}

// idleSquad picks ten ordinary cards per profession only to satisfy the deck
// input contract. Members never play them, so card choice does not affect the
// baseline; cards with passive/CALL/summon effects are excluded for that reason.
// Player maximum HP is capped by the engine, so knockouts can still happen.
func idleSquad(catalog *battlecore.CombatCatalog) ([]battlecore.StandaloneBattleMember, error) {
	ids := make([]int, 0, len(catalog.Cards))
	for id := range catalog.Cards {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	squad := make([]battlecore.StandaloneBattleMember, 4)
	for role := 1; role <= 4; role++ {
		member := battlecore.StandaloneBattleMember{MemberType: role, ArthurType: role, HP: 999999, Attack: 1000, Magic: 1000, Mind: 1000}
		used := map[int]bool{}
		for _, id := range ids {
			card := catalog.Cards[id]
			if card.MaxLevel < 1 || card.PassiveSkillID != 0 || card.CallSkillID != 0 || card.SummonID != 0 {
				continue
			}
			if _, _, err := catalog.CardSkill(id, role); err != nil {
				continue
			}
			if catalog.ValidateMainCardPassives(card) != nil {
				continue
			}
			base := card.BaseCardID
			if base == 0 {
				base = id
			}
			if used[base] {
				continue
			}
			used[base] = true
			member.DeckCards = append(member.DeckCards, battlecore.BattleCard{CardType: len(member.DeckCards) + 1, CardID: id, Level: 1})
			if len(member.DeckCards) == 10 {
				break
			}
		}
		if len(member.DeckCards) != 10 {
			return nil, fmt.Errorf("profession %d has fewer than ten neutral cards for the idle deck", role)
		}
		squad[role-1] = member
	}
	return squad, nil
}

func simulateAll(catalog *battlecore.CombatCatalog, squad []battlecore.StandaloneBattleMember, req request, workers int) map[int]partyBaseline {
	parties := append([]int(nil), req.Parties...)
	sort.Ints(parties)
	results := make(map[int]partyBaseline, len(parties))
	var mutex sync.Mutex
	jobs := make(chan int)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for party := range jobs {
				if _, ok := catalog.EnemyParties[party]; !ok {
					continue
				}
				baseline := partyBaseline{Party: party, Seeds: len(req.Seeds)}
				for index, seed := range req.Seeds {
					records, err := simulate(catalog, squad, party, seed, req.Turns)
					if err != "" {
						baseline.Errors = append(baseline.Errors, fmt.Sprintf("seed %d: %s", seed, err))
					}
					aggregate(&baseline, records, index == 0)
				}
				for i := range baseline.Turns {
					baseline.Turns[i].Uses = sortedUses(baseline.Turns[i].Uses)
					baseline.Turns[i].Charges = sortedUses(baseline.Turns[i].Charges)
				}
				mutex.Lock()
				results[party] = baseline
				mutex.Unlock()
			}
		}()
	}
	for _, party := range parties {
		jobs <- party
	}
	close(jobs)
	group.Wait()
	return results
}

func simulate(catalog *battlecore.CombatCatalog, squad []battlecore.StandaloneBattleMember, party int, seed uint32, turns int) (records []turnRecord, failure string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = fmt.Sprintf("panic: %v", recovered)
		}
	}()
	battle, err := battlecore.NewStandaloneBattle(catalog, battlecore.StandaloneBattleConfig{
		EnemyPartyID: party, CostInitial: 3, HoldMax: 6, Seed: seed, ContinueAllowed: true,
	}, squad)
	if err != nil {
		return nil, err.Error()
	}
	opening := turnRecord{turn: 0}
	rows, err := battle.Start()
	if err != nil {
		return nil, "start: " + err.Error()
	}
	collect(&opening, "start", rows)
	for turn := 1; turn <= turns; turn++ {
		record := turnRecord{turn: turn}
		rows, err := battle.TurnPhase()
		if err != nil {
			return records, fmt.Sprintf("turn %d: %v", turn, err)
		}
		// The first TurnPhase carries the wave-turn-zero first attack before
		// its ResultCmd 1 (TURN). Split it into the opening record.
		if turn == 1 {
			split := len(rows)
			for index, row := range rows {
				if row.Command == battlecore.CommandTurn {
					split = index
					break
				}
			}
			collect(&opening, "first_attack", rows[:split])
			revive(battle, &opening)
			finish(&opening, battle)
			records = append(records, opening)
			rows = rows[split:]
		}
		collect(&record, "turn", rows)
		revive(battle, &record)
		for _, phase := range []struct {
			name string
			call func() ([]battlecore.BattleResult, error)
		}{
			{"user", battle.UserPhase},
			{"selection", func() ([]battlecore.BattleResult, error) { return submitPasses(battle) }},
			{"user_attack", battle.UserAttack},
			{"chalice_user", battle.ChaliceUserPhase},
			{"enemy", battle.EnemyPhase},
			{"chalice_enemy", battle.ChaliceEnemyPhase},
		} {
			if battle.StateView().Ended {
				break
			}
			rows, err := phase.call()
			if err != nil {
				finish(&record, battle)
				return append(records, record), fmt.Sprintf("turn %d %s: %v", turn, phase.name, err)
			}
			collect(&record, phase.name, rows)
			revive(battle, &record)
		}
		finish(&record, battle)
		records = append(records, record)
		if record.ended {
			break
		}
	}
	return records, ""
}

// revive resolves a pending continuation through the engine's own continue
// flow so that a knocked-out idle member does not end the observation. A turn
// in which the whole party was down is marked: actions queued after the wipe
// found no player target, so that turn does not describe the boss schedule.
func revive(battle *battlecore.StandaloneBattle, record *turnRecord) {
	players, down := 0, 0
	for _, member := range battle.StateView().Members {
		if member.EnemyID == 0 {
			players++
			if member.HP <= 0 {
				down++
			}
		}
	}
	if players > 0 && down == players {
		record.wiped = true
	}
	for _, member := range battle.StateView().Members {
		if member.EnemyID != 0 || member.HP > 0 {
			continue
		}
		// One continuation restores every knocked-out member at full HP.
		if _, err := battle.Continue(member.MemberType); err == nil {
			record.revives++
			battle.ContinueDataGet()
			return
		}
	}
}

func submitPasses(battle *battlecore.StandaloneBattle) ([]battlecore.BattleResult, error) {
	var rows []battlecore.BattleResult
	for _, member := range battle.StateView().Members {
		if member.EnemyID != 0 || member.HP <= 0 {
			continue
		}
		submitted, err := battle.Submit(battlecore.StandaloneCardPlay{MemberType: member.MemberType})
		if err != nil {
			return rows, err
		}
		rows = append(rows, submitted...)
	}
	return rows, nil
}

func collect(record *turnRecord, phase string, rows []battlecore.BattleResult) {
	for _, row := range rows {
		switch row.Command {
		case battlecore.CommandSkill:
			if len(row.Args) >= 7 && row.Args[0] >= 5 {
				record.uses = append(record.uses, skillUse{Actor: int(row.Args[0]), Skill: int(row.Args[1]), Function: int(row.Args[5]), Passive: row.Args[6] != 0, Phase: phase})
			}
		case battlecore.CommandChargeStart:
			if len(row.Args) >= 3 {
				record.charges = append(record.charges, skillUse{Actor: int(row.Args[0]), Skill: int(row.Args[1]), Function: int(row.Args[2]), Phase: phase})
			}
		case battlecore.CommandPartsBreak, battlecore.CommandEnemyBreak:
			if len(row.Args) >= 1 {
				record.breaks = append(record.breaks, int(row.Args[0]))
			}
		case battlecore.CommandGameOver:
			record.knockouts++
		case battlecore.CommandContinueWait:
			record.knockouts++
		}
	}
}

func finish(record *turnRecord, battle *battlecore.StandaloneBattle) {
	view := battle.StateView()
	record.ended, record.endType = view.Ended, view.EndType
	for _, member := range view.Members {
		if member.EnemyID == 0 {
			continue
		}
		record.flags = append(record.flags, member.Flags)
		record.variables = append(record.variables, member.Variables)
		record.enemyHP = append(record.enemyHP, member.HP)
	}
}

func aggregate(baseline *partyBaseline, records []turnRecord, sample bool) {
	for _, record := range records {
		for len(baseline.Turns) <= record.turn {
			baseline.Turns = append(baseline.Turns, turnSummary{Turn: len(baseline.Turns), Uses: []countedUse{}, Sample: []skillUse{}})
		}
		summary := &baseline.Turns[record.turn]
		if summary.index == nil {
			summary.index = map[skillUse]int{}
		}
		summary.Knockouts += record.knockouts
		summary.Revives += record.revives
		if record.wiped {
			summary.Wiped++
			continue
		}
		summary.Runs++
		count := func(list *[]countedUse, uses []skillUse, prefix string) {
			seen := map[skillUse]bool{}
			for _, use := range uses {
				key := use
				key.Phase = prefix + use.Phase
				position, ok := summary.index[key]
				if !ok {
					position = len(*list)
					*list = append(*list, countedUse{skillUse: use})
					summary.index[key] = position
				}
				entry := &(*list)[position]
				entry.Count++
				if !seen[key] {
					entry.Runs++
					seen[key] = true
				}
			}
		}
		count(&summary.Uses, record.uses, "use:")
		count(&summary.Charges, record.charges, "charge:")
		for len(summary.FlagRuns) < len(record.flags) {
			summary.FlagRuns = append(summary.FlagRuns, map[int]int{})
			summary.Variables = append(summary.Variables, [5]variableRange{})
			summary.EnemyHP = append(summary.EnemyHP, [2]int{-1, -1})
		}
		for slot, flags := range record.flags {
			for bit := 0; bit < 32; bit++ {
				if flags&(1<<bit) != 0 {
					summary.FlagRuns[slot][bit]++
				}
			}
			for index, value := range record.variables[slot] {
				current := &summary.Variables[slot][index]
				if summary.Runs == 1 || value < current.Min {
					current.Min = value
				}
				if summary.Runs == 1 || value > current.Max {
					current.Max = value
				}
			}
			hp := &summary.EnemyHP[slot]
			if hp[0] < 0 || record.enemyHP[slot] < hp[0] {
				hp[0] = record.enemyHP[slot]
			}
			if record.enemyHP[slot] > hp[1] {
				hp[1] = record.enemyHP[slot]
			}
		}
		for _, member := range record.breaks {
			if summary.Breaks == nil {
				summary.Breaks = map[int]int{}
			}
			summary.Breaks[member]++
		}
		if record.ended {
			if summary.Ended == nil {
				summary.Ended = map[int]int{}
			}
			summary.Ended[record.endType]++
		}
		if sample {
			summary.Sample = append([]skillUse{}, record.uses...)
			summary.SampleCh = append([]skillUse(nil), record.charges...)
		}
	}
}

func sortedUses(uses []countedUse) []countedUse {
	sort.SliceStable(uses, func(i, j int) bool {
		if uses[i].Actor != uses[j].Actor {
			return uses[i].Actor < uses[j].Actor
		}
		return uses[i].Runs > uses[j].Runs
	})
	return uses
}
