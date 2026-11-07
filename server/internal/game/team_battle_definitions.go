package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
)

// TeamBattleDefinitions is immutable and shared by one content publication.
// Player clears and timed unlocks never enter this catalog or its entry index.
type TeamBattleDefinitions struct {
	base   *TeamBattleCatalog
	rules  map[int]TeamBattleEntryRules
	areas  map[battleBossKey]bool
	buffs  map[int][]battleBossKey
	bosses map[int]teamBattleCatalogBoss
}

type battleBossKey struct{ AreaID, BossID int }

type battleBossProgress struct {
	State int   `json:"10"`
	Until int64 `json:"16"`
}

// The JSON is only a derived persistence view. Mutations own the small progress
// map and invalidate this existing view; request DTOs are always separate.
type battleCatalogState struct {
	definitions *TeamBattleDefinitions
	progress    map[battleBossKey]battleBossProgress
	encoded     json.RawMessage
}

func NewTeamBattleDefinitions(raw json.RawMessage) (*TeamBattleDefinitions, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	base, err := decodeTeamBattleCatalog(raw)
	if err != nil {
		return nil, err
	}
	definitions := &TeamBattleDefinitions{base: base, rules: map[int]TeamBattleEntryRules{}, areas: map[battleBossKey]bool{}, buffs: map[int][]battleBossKey{}, bosses: map[int]teamBattleCatalogBoss{}}
	for _, key := range teamBattleCategoryKeys {
		for _, group := range base.groups[key] {
			for _, boss := range group.bosses {
				definitions.areas[battleBossKey{group.areaID, boss.id}] = true
				if _, found := definitions.bosses[boss.id]; !found {
					definitions.bosses[boss.id] = boss
					if rules, ok := entryRulesForCatalogBoss(group, boss); ok {
						definitions.rules[boss.id] = rules
					}
				}
				if _, present := boss.fields["10"]; present {
					boss.fields["10"] = json.Number("0")
				}
				if _, present := boss.fields["16"]; present {
					boss.fields["16"] = json.Number("0")
				}
				if values, ok := boss.fields["15"].([]any); ok {
					for _, value := range values {
						id, err := teamBattleCatalogInteger(value)
						if err != nil {
							return nil, fmt.Errorf("decode user-buff reference: %w", err)
						}
						if !slices.Contains(definitions.buffs[int(id)], battleBossKey{group.areaID, boss.id}) {
							definitions.buffs[int(id)] = append(definitions.buffs[int(id)], battleBossKey{group.areaID, boss.id})
						}
					}
				}
			}
		}
	}
	return definitions, nil
}

func (definitions *TeamBattleDefinitions) entryRules(bossID int) (TeamBattleEntryRules, bool) {
	if definitions == nil {
		return TeamBattleEntryRules{}, false
	}
	rules, found := definitions.rules[bossID]
	rules.EntryLimit = slices.Clone(rules.EntryLimit)
	return rules, found
}

func readBattleProgress(raw json.RawMessage) (map[battleBossKey]battleBossProgress, error) {
	progress := map[battleBossKey]battleBossProgress{}
	if len(raw) == 0 {
		return progress, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	for _, key := range teamBattleCategoryKeys {
		var groups []struct {
			AreaID int `json:"9"`
			Bosses []struct {
				ID int `json:"0"`
				battleBossProgress
			} `json:"10"`
		}
		if len(top[key]) == 0 {
			continue
		}
		if err := json.Unmarshal(top[key], &groups); err != nil {
			return nil, err
		}
		for _, group := range groups {
			for _, boss := range group.Bosses {
				if boss.State != 0 || boss.Until != 0 {
					key := battleBossKey{group.AreaID, boss.ID}
					next := boss.battleBossProgress
					if previous, exists := progress[key]; exists {
						next.State, next.Until = max(previous.State, next.State), max(previous.Until, next.Until)
					}
					progress[key] = next
				}
			}
		}
	}
	return progress, nil
}

func newBattleCatalogState(raw json.RawMessage, definitions *TeamBattleDefinitions) (*battleCatalogState, error) {
	progress, err := readBattleProgress(raw)
	if err != nil {
		return nil, err
	}
	if definitions == nil {
		definitions, err = NewTeamBattleDefinitions(raw)
		if err != nil {
			return nil, err
		}
	}
	return &battleCatalogState{definitions: definitions, progress: progress}, nil
}

func cloneCatalogValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			result[key] = cloneCatalogValue(item)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = cloneCatalogValue(item)
		}
		return result
	default:
		return value
	}
}

func cloneCatalogFields(fields map[string]any, excluded ...string) map[string]any {
	result := make(map[string]any, len(fields))
	for key, value := range fields {
		if !slices.Contains(excluded, key) {
			result[key] = cloneCatalogValue(value)
		}
	}
	return result
}

func (state *battleCatalogState) view() (*TeamBattleCatalog, error) {
	if state == nil || state.definitions == nil {
		return nil, errors.New("team battle catalog is unavailable")
	}
	base := state.definitions.base
	view := &TeamBattleCatalog{fields: cloneCatalogFields(base.fields, teamBattleCategoryKeys[:]...), groups: make(map[string][]teamBattleCatalogGroup, 4)}
	for _, key := range teamBattleCategoryKeys {
		groups := make([]teamBattleCatalogGroup, len(base.groups[key]))
		for i, source := range base.groups[key] {
			group := source
			group.fields = cloneCatalogFields(source.fields, "10")
			group.bosses = make([]teamBattleCatalogBoss, len(source.bosses))
			for j, boss := range source.bosses {
				boss.fields = cloneCatalogFields(boss.fields)
				if progress, found := state.progress[battleBossKey{group.areaID, boss.id}]; found {
					applyBossProgress(boss.fields, progress)
				}
				group.bosses[j] = boss
			}
			groups[i] = group
		}
		view.groups[key] = groups
	}
	return view, nil
}

func applyBossProgress(fields map[string]any, progress battleBossProgress) {
	fields["10"] = json.Number(strconv.Itoa(progress.State))
	if progress.Until != 0 || fields["16"] != nil {
		fields["16"] = json.Number(strconv.FormatInt(progress.Until, 10))
	}
}

func (state *battleCatalogState) saved() json.RawMessage {
	if state == nil || state.definitions == nil {
		return nil
	}
	if state.encoded == nil {
		view, _ := state.view()
		// Definitions originate in decoded JSON and progress contains integers;
		// the derived view cannot contain an unsupported encoder value.
		state.encoded, _ = view.MarshalJSON()
	}
	return state.encoded
}

func (state *battleCatalogState) expire(now int64) {
	if state == nil {
		return
	}
	for id, progress := range state.progress {
		if progress.Until > 0 && progress.Until <= now {
			delete(state.progress, id)
			state.encoded = nil
		}
	}
}

func (state *battleCatalogState) firstClear(bossID, areaID int) (bool, error) {
	if state == nil || state.definitions == nil || !state.definitions.areas[battleBossKey{areaID, bossID}] {
		return false, errors.New("local team battle clear boss is unavailable")
	}
	progress := state.progress[battleBossKey{areaID, bossID}]
	if progress.State < 0 || progress.State > 2 {
		return false, errors.New("local team battle clear state is invalid")
	}
	return progress.State != 2, nil
}

func (state *battleCatalogState) markClear(bossID, areaID int) {
	progress := state.progress[battleBossKey{areaID, bossID}]
	progress.State = 2
	state.progress[battleBossKey{areaID, bossID}] = progress
	state.encoded = nil
}

func WithBattleCatalog(definitions *TeamBattleDefinitions) AccountOption {
	return func(options *accountOptions) { options.battleCatalog = definitions }
}

func (state *battleCatalogState) entryRules(bossID int) (TeamBattleEntryRules, bool) {
	if state == nil {
		return TeamBattleEntryRules{}, false
	}
	return state.definitions.entryRules(bossID)
}
