package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"strconv"

	"kairisei.local/server/internal/clientbridge"
)

// Partner decks have one declared named-card shape across live partners and
// stored clear/ranking snapshots. Enrich only the outgoing view, preserving
// the snapshot's IDs, levels, ordinary parameters and all unrelated fields.
type partnerDeckParameterView struct {
	value      any
	parameters *clientbridge.CardParameterCatalog
}

func (a *API) partnerDeckParameterView(value any) partnerDeckParameterView {
	return partnerDeckParameterView{value: value, parameters: a.cardParameters}
}

func (a *API) partnerDeckParameterViews(values []any) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = a.partnerDeckParameterView(value)
	}
	return result
}

func (view partnerDeckParameterView) MarshalJSON() ([]byte, error) {
	if view.parameters == nil {
		return json.Marshal(view.value)
	}
	var source map[string]any
	switch value := view.value.(type) {
	case map[string]any:
		source = value
	case json.RawMessage:
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&source); err != nil {
			return nil, fmt.Errorf("decode stored partner deck: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("stored partner deck contains trailing data")
		}
	default:
		return nil, fmt.Errorf("unsupported partner deck view %T", view.value)
	}
	result := maps.Clone(source)
	for _, field := range []string{"deck", "support_deck"} {
		if source[field] == nil {
			continue
		}
		cards, ok := source[field].([]any)
		if !ok {
			return nil, fmt.Errorf("partner %s is not a card array", field)
		}
		projected := make([]any, len(cards))
		for i, value := range cards {
			card, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("partner %s card %d is not an object", field, i)
			}
			var input [4]int
			for j, name := range []string{"cardid", "lv", "love", "fame"} {
				number, err := partnerParameterNumber(card[name])
				if err != nil {
					return nil, fmt.Errorf("partner %s card %d %s: %w", field, i, name, err)
				}
				input[j] = number
			}
			families, err := view.parameters.Resolve(input[0], input[1], input[2], input[3])
			if err != nil {
				return nil, err
			}
			copy := maps.Clone(card)
			if families.Alchemist != nil {
				copy["alchemist_param"] = families.Alchemist
			}
			if families.Summons != nil {
				copy["summons_param"] = families.Summons
			}
			projected[i] = copy
		}
		result[field] = projected
	}
	return json.Marshal(result)
}

func partnerParameterNumber(value any) (int, error) {
	var number int64
	switch value := value.(type) {
	case nil:
		return 0, nil
	case int:
		number = int64(value)
	case int32:
		number = int64(value)
	case int64:
		number = value
	case json.Number:
		var err error
		number, err = strconv.ParseInt(string(value), 10, 32)
		if err != nil {
			return 0, err
		}
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < math.MinInt32 || value > math.MaxInt32 {
			return 0, fmt.Errorf("parameter is not a signed 32-bit integer")
		}
		number = int64(value)
	default:
		return 0, fmt.Errorf("unsupported parameter type %T", value)
	}
	if number < math.MinInt32 || number > math.MaxInt32 {
		return 0, fmt.Errorf("parameter exceeds signed 32-bit range")
	}
	return int(number), nil
}
