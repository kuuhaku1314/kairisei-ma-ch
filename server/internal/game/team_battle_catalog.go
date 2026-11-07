package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"

	"kairisei.local/server/internal/gamestate"
)

var teamBattleCategoryKeys = [...]string{"9", "10", "11", "12"}

// TeamBattleCatalog is a request-owned protocol view of shared definitions and
// private progress. Its maps never alias another request or the published base.
type TeamBattleCatalog struct {
	fields map[string]any
	groups map[string][]teamBattleCatalogGroup
}

type teamBattleCatalogGroup struct {
	fields map[string]any
	id     int
	areaID int
	bosses []teamBattleCatalogBoss
}

type teamBattleCatalogBoss struct {
	fields map[string]any
	id     int
	model  int
}

func decodeTeamBattleCatalog(raw json.RawMessage) (*TeamBattleCatalog, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	// Preserve integer precision and unmodeled fields when re-encoding the DTO.
	decoder.UseNumber()
	var fields map[string]any
	if err := decoder.Decode(&fields); err != nil {
		return nil, fmt.Errorf("decode team battle catalog: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("team battle catalog has trailing JSON")
	}
	if fields == nil {
		return nil, errors.New("team battle catalog is not an object")
	}
	catalog := &TeamBattleCatalog{fields: fields, groups: make(map[string][]teamBattleCatalogGroup, 4)}
	for _, key := range teamBattleCategoryKeys {
		values, ok := fields[key].([]any)
		if !ok && fields[key] != nil {
			return nil, fmt.Errorf("team battle category %s is not a list", key)
		}
		groups := make([]teamBattleCatalogGroup, 0, len(values))
		for _, value := range values {
			group, err := decodeTeamBattleCatalogGroup(value)
			if err != nil {
				return nil, err
			}
			groups = append(groups, group)
		}
		catalog.groups[key] = groups
	}
	return catalog, nil
}

func decodeTeamBattleCatalogGroup(value any) (teamBattleCatalogGroup, error) {
	fields, ok := value.(map[string]any)
	if !ok {
		return teamBattleCatalogGroup{}, errors.New("team battle group is not an object")
	}
	id, idErr := teamBattleCatalogInteger(fields["0"])
	areaID, areaErr := teamBattleCatalogInteger(fields["9"])
	_, stageErr := teamBattleCatalogInteger(fields["1"])
	if idErr != nil || areaErr != nil || stageErr != nil {
		return teamBattleCatalogGroup{}, errors.New("decode team battle group identity")
	}
	values, ok := fields["10"].([]any)
	if !ok && fields["10"] != nil {
		return teamBattleCatalogGroup{}, errors.New("team battle bosses are not a list")
	}
	group := teamBattleCatalogGroup{fields: fields, id: int(id), areaID: int(areaID)}
	for _, value := range values {
		boss, ok := value.(map[string]any)
		if !ok {
			return teamBattleCatalogGroup{}, errors.New("team battle boss is not an object")
		}
		id, idErr := teamBattleCatalogInteger(boss["0"])
		model, modelErr := teamBattleCatalogInteger(boss["14"])
		if idErr != nil || modelErr != nil {
			return teamBattleCatalogGroup{}, errors.New("decode team battle boss identity")
		}
		for _, key := range []string{"10", "26"} {
			if _, err := teamBattleCatalogInteger(boss[key]); err != nil {
				return teamBattleCatalogGroup{}, fmt.Errorf("decode team battle boss %d field %s: %w", id, key, err)
			}
		}
		if difficulty, exists := boss["4"]; exists && difficulty != nil {
			if _, ok := difficulty.(string); !ok {
				return teamBattleCatalogGroup{}, errors.New("team battle difficulty is not a string")
			}
		}
		group.bosses = append(group.bosses, teamBattleCatalogBoss{fields: boss, id: int(id), model: int(model)})
	}
	return group, nil
}

func teamBattleCatalogInteger(value any) (int64, error) {
	if value == nil {
		return 0, nil
	}
	number, ok := value.(json.Number)
	if !ok {
		return 0, errors.New("team battle numeric field is not an integer")
	}
	return number.Int64()
}

func (group teamBattleCatalogGroup) wire() map[string]any {
	bosses := make([]any, 0, len(group.bosses))
	for _, boss := range group.bosses {
		bosses = append(bosses, boss.fields)
	}
	group.fields["10"] = bosses
	return group.fields
}

// Groups returns the stock numeric-field DTOs for one category. Adapters should
// treat them as read-only. They never alias another request or saved progress.
func (catalog *TeamBattleCatalog) Groups(key string) []any {
	groups := make([]any, 0, len(catalog.groups[key]))
	for _, group := range catalog.groups[key] {
		groups = append(groups, group.wire())
	}
	return groups
}

// MainQuestGroups presents the already projected normal progress in the modern
// directory order. The UI's stage type and zero area slot do not change the
// underlying entry rules or settlement context.
func (catalog *TeamBattleCatalog) MainQuestGroups() []any {
	groups := make([]any, 0, len(catalog.groups["9"]))
	for index := len(catalog.groups["9"]) - 1; index >= 0; index-- {
		group := catalog.groups["9"][index]
		if group.areaID < cnNormalQuestAreaMin || group.areaID > cnNormalQuestAreaMax {
			continue
		}
		bosses := make([]any, 0, len(group.bosses))
		for index := len(group.bosses) - 1; index >= 0; index-- {
			boss := group.bosses[index]
			locked, _ := teamBattleCatalogInteger(boss.fields["26"])
			if locked == 0 {
				bosses = append(bosses, boss.fields)
			}
		}
		fields := maps.Clone(group.fields)
		fields["1"] = json.Number(fmt.Sprint(gamestate.BattleStageMainQuest))
		fields["9"] = json.Number("0")
		fields["10"] = bosses
		groups = append(groups, fields)
	}
	return groups
}

func (catalog *TeamBattleCatalog) HasBoss(bossID int) bool {
	_, _, found := catalog.findBoss(bossID)
	return found
}

func (catalog *TeamBattleCatalog) findBoss(bossID int) (teamBattleCatalogGroup, teamBattleCatalogBoss, bool) {
	if catalog == nil || bossID <= 0 {
		return teamBattleCatalogGroup{}, teamBattleCatalogBoss{}, false
	}
	for _, key := range teamBattleCategoryKeys {
		for _, group := range catalog.groups[key] {
			for _, boss := range group.bosses {
				if boss.id == bossID {
					return group, boss, true
				}
			}
		}
	}
	return teamBattleCatalogGroup{}, teamBattleCatalogBoss{}, false
}

// GroupForBoss reuses the request-owned DTO and its unmodeled protocol fields.
func (catalog *TeamBattleCatalog) GroupForBoss(bossID int) (int, any, bool) {
	group, _, found := catalog.findBoss(bossID)
	if !found {
		return 0, nil, false
	}
	return group.id, group.wire(), true
}

func (catalog *TeamBattleCatalog) EntryRules(bossID int) (TeamBattleEntryRules, bool) {
	group, boss, found := catalog.findBoss(bossID)
	if !found {
		return TeamBattleEntryRules{}, false
	}
	return entryRulesForCatalogBoss(group, boss)
}

func entryRulesForCatalogBoss(group teamBattleCatalogGroup, boss teamBattleCatalogBoss) (TeamBattleEntryRules, bool) {
	rules := TeamBattleEntryRules{BossID: boss.id}
	fields := []struct {
		key string
		out *int
	}{{"1", &rules.OnlyMyDeck}, {"5", &rules.BPUse}, {"6", &rules.BPUseHalf}, {"7", &rules.Continue}, {"24", &rules.StartRule}}
	for _, field := range fields {
		value, err := teamBattleCatalogInteger(boss.fields[field.key])
		if err != nil {
			return TeamBattleEntryRules{}, false
		}
		*field.out = int(value)
	}
	if value := boss.fields["18"]; value != nil {
		awake, ok := value.(map[string]any)
		if !ok {
			return TeamBattleEntryRules{}, false
		}
		flags := []struct {
			key string
			out *bool
		}{
			{"0", &rules.AwakeTakeover.KeepTurn},
			{"1", &rules.AwakeTakeover.KeepCost},
			{"2", &rules.AwakeTakeover.KeepBuff},
			{"3", &rules.AwakeTakeover.KeepSphereCount},
			{"4", &rules.AwakeTakeover.KeepDeckTrash},
			{"5", &rules.AwakeTakeover.KeepDeal},
		}
		for _, flag := range flags {
			value, err := teamBattleCatalogInteger(awake[flag.key])
			if err != nil {
				return TeamBattleEntryRules{}, false
			}
			*flag.out = value != 0
		}
	}
	// Project only the needed fields; do not reparse the whole boss.
	if entries := boss.fields["entry_limit"]; entries != nil {
		raw, err := json.Marshal(entries)
		if err != nil || json.Unmarshal(raw, &rules.EntryLimit) != nil {
			return TeamBattleEntryRules{}, false
		}
	}
	stage, err := teamBattleCatalogInteger(group.fields["1"])
	if err != nil {
		return TeamBattleEntryRules{}, false
	}
	rules.StageType = gamestate.BattleStageType(stage)
	return rules, true
}

func (catalog *TeamBattleCatalog) BossAccessible(bossID int, nowUnix int64) bool {
	_, boss, found := catalog.findBoss(bossID)
	if !found || nowUnix <= 0 {
		return false
	}
	state, hasState := boss.fields["10"]
	expiration, hasExpiration := boss.fields["16"]
	value, stateErr := teamBattleCatalogInteger(state)
	until, expirationErr := teamBattleCatalogInteger(expiration)
	return hasState && hasExpiration && stateErr == nil && expirationErr == nil && value >= 0 && value <= 2 && (until == 0 || until > nowUnix)
}

func (catalog *TeamBattleCatalog) MarshalJSON() ([]byte, error) {
	for _, key := range teamBattleCategoryKeys {
		catalog.fields[key] = catalog.Groups(key)
	}
	return json.Marshal(catalog.fields)
}

func (catalog *TeamBattleCatalog) filterDisabled(disabled map[int]bool) {
	if len(disabled) == 0 {
		return
	}
	for _, key := range teamBattleCategoryKeys {
		for index := range catalog.groups[key] {
			group := &catalog.groups[key][index]
			kept := make([]teamBattleCatalogBoss, 0, len(group.bosses))
			for _, boss := range group.bosses {
				if !disabled[boss.id] {
					kept = append(kept, boss)
				}
			}
			group.bosses = kept
		}
	}
}
