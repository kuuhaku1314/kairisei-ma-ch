package battlecontent

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"kairisei.local/server/battlemodel"
)

const EnemyGaugeTable = "enemy_trance_gauge_reaction.csv"

func enemyGaugeState(value string) (int, bool) {
	switch value {
	case "NONE":
		return 0, true
	case "NORMAL":
		return 1, true
	case "TRANCE":
		return 2, true
	case "OVER_HEAT":
		return 3, true
	default:
		return 0, false
	}
}

var gaugeReactionKinds = []string{"SKILL_KIND", "COST_FIXED", "COST_RANGE", "ATTR", "ARTHUR_TYPE", "PLAY_TAG", "BURST_SKILL", "NONE"}
var gaugeReactionTargets = []string{"NONE", "ALL", "CARD", "HOLD_CARD", "CURSE", "BLESS", "SPHR", "TOKEN"}

func gaugeEnum(text string, names []string, fallback int) (int, string, error) {
	if text == "-1" && fallback == -1 {
		return -1, "-1", nil
	}
	if text == "" {
		if fallback < 0 {
			return fallback, strconv.Itoa(fallback), nil
		}
		return fallback, names[fallback], nil
	}
	for value, name := range names {
		if text == name || text == strconv.Itoa(value) {
			return value, name, nil
		}
	}
	return 0, "", fmt.Errorf("unknown gauge enum %q", text)
}

// ParseEnemyGaugeReactions reads the original group ID, five targets, kind,
// eight parameter slots and two signed rates. Duplicate IDs append in source
// order. The original valid count selects a prefix; holes are not compacted.
func ParseEnemyGaugeReactions(text string) (map[int][]battlemodel.EnemyGaugeReaction, error) {
	groups := make(map[int][]battlemodel.EnemyGaugeReaction)
	err := readCombatData(strings.NewReader(text), true, func(row []string) error {
		if len(row) != 17 && (len(row) != 18 || Field(row, 17) != "") {
			return fmt.Errorf("gauge reaction row must have 17 fields and an optional empty tail")
		}
		id, err := RequiredInt(row, 0, "gauge group ID")
		if err != nil {
			return err
		}
		kind, name, err := gaugeEnum(strings.TrimSpace(row[6]), gaugeReactionKinds, 0)
		if err != nil {
			return err
		}
		value := battlemodel.EnemyGaugeReaction{Kind: name, Targets: make([]string, 5)}
		for i := range value.Targets {
			_, value.Targets[i], err = gaugeEnum(strings.TrimSpace(row[1+i]), gaugeReactionTargets, 0)
			if err != nil {
				return err
			}
		}
		parameters := make([]string, 8)
		valid := 0
		for i := range parameters {
			token := strings.TrimSpace(row[7+i])
			var number int
			switch kind {
			case 0:
				number, parameters[i], err = gaugeEnum(token, []string{"NULL", "ATTACK", "SORCERY", "RECOVERY", "SUPPORT", "DEFENSE", "JAMMING", "SPECIAL"}, 0)
			case 3:
				number, parameters[i], err = gaugeEnum(token, []string{"NULL", "FIRE", "ICE", "WIND", "LIGHT", "DARK"}, 0)
			case 4:
				number, parameters[i], err = gaugeEnum(token, []string{"NULL", "MERCENARY", "MILLIONAIRE", "THIEF", "SINGER", "ALCHEMIST"}, -1)
			case 7:
				// The original reader consumes but does not retain these slots.
				parameters[i] = "0"
			default:
				number = -1
				if token != "" {
					var parsed int64
					parsed, err = strconv.ParseInt(token, 10, 32)
					number = int(parsed)
				}
				parameters[i] = strconv.Itoa(number)
			}
			if err != nil {
				return fmt.Errorf("gauge group %d parameter %d: %w", id, i, err)
			}
			if kind != 7 && ((kind == 0 || kind == 3) && number > 0 || kind != 0 && kind != 3 && number >= 0) {
				valid++
			}
		}
		value.Parameters = parameters[:valid:valid]
		for _, field := range []struct {
			column int
			target *int
		}{{15, &value.NormalRate}, {16, &value.TranceRate}} {
			if token := Field(row, field.column); token != "" {
				parsed, err := strconv.ParseInt(token, 10, 32)
				if err != nil {
					return fmt.Errorf("gauge group %d rate: %w", id, err)
				}
				*field.target = int(parsed)
			}
		}
		groups[id] = append(groups[id], value)
		if len(groups[id]) > 50 {
			return fmt.Errorf("gauge group %d exceeds supported reaction capacity", id)
		}
		return nil
	})
	return groups, err
}

func readEnemyGaugeFile(root string) (string, error) {
	path, err := resolveCombatFileWithin(root, EnemyGaugeTable)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw) > maxCombatMasterBytes {
		return "", fmt.Errorf("invalid size for %s", EnemyGaugeTable)
	}
	return string(raw), nil
}

func applyEnemyGaugeTables(catalog *battlemodel.Definitions, text string) error {
	groups, err := ParseEnemyGaugeReactions(text)
	if err != nil {
		return err
	}
	for id, enemy := range catalog.Enemies {
		if enemy.GaugeReactionID == 0 {
			// The original JP state exists even without reactions. A NONE
			// gauge still publishes its unchanged value at skill start.
			enemy.ReactiveGauge = &battlemodel.EnemyGaugeDefinition{InitialState: enemy.InitialGaugeState, InitialRate: enemy.InitialGaugeRate}
			catalog.Enemies[id] = enemy
			continue
		}
		reactions, found := groups[enemy.GaugeReactionID]
		if !found {
			return fmt.Errorf("enemy %d requires gauge reaction group %d in %s", id, enemy.GaugeReactionID, EnemyGaugeTable)
		}
		if _, found := catalog.EnemyLevels[id]; !found {
			return fmt.Errorf("enemy %d has no level definition for its gauge", id)
		}
		// Definitions are immutable: shared groups keep one parsed reaction
		// slice; each enemy retains its own initial state and rate.
		enemy.ReactiveGauge = &battlemodel.EnemyGaugeDefinition{InitialState: enemy.InitialGaugeState, InitialRate: enemy.InitialGaugeRate, Reactions: reactions}
		catalog.Enemies[id] = enemy
	}
	return nil
}
