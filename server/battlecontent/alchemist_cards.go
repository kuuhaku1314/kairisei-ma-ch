package battlecontent

import (
	"fmt"
	"strings"

	"kairisei.local/server/battlemodel"
)

// Both modern adapters share the ordinary card namespace and parser with the
// original alchemist table. Legacy inputs omit this table. Definition loading
// is independent from inventory admission and engine capability.
func applyAlchemistCardTable(catalog *battlemodel.Definitions, tables map[string]string) error {
	text, exists := tables["card_alchemist.csv"]
	if !exists {
		return nil
	}
	if len(text) == 0 || len(text) > maxCombatMasterBytes {
		return fmt.Errorf("alchemist card table is outside the size boundary")
	}
	count := 0
	err := readCombatData(strings.NewReader(text), true, func(row []string) error {
		card, err := parseModernCard(row)
		if err != nil {
			return err
		}
		if _, exists := catalog.Cards[card.ID]; exists {
			return fmt.Errorf("alchemist card ID %d collides with an existing definition", card.ID)
		}
		catalog.Cards[card.ID] = card
		count++
		return nil
	})
	if err != nil {
		return fmt.Errorf("load card_alchemist.csv: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("alchemist card table has no definitions")
	}
	return nil
}
