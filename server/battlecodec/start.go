package battlecodec

import (
	"strings"

	"kairisei.local/server/battlemodel"
)

// EncodeStart preserves the original Start ordering: recovery HP/limit/gauge rows,
// card identities, then the ordinary result group. Only the tail is grouped.
func EncodeStart(results, identities []battlemodel.Result) (string, error) {
	var rows []string
	for len(results) > 0 && (results[0].Command == 3 || results[0].Command == 503 || results[0].Command == 32 || results[0].Command == 500) {
		row, err := EncodeRow(results[0])
		if err != nil {
			return "", err
		}
		rows = append(rows, row)
		results = results[1:]
	}
	for _, identity := range identities {
		row, err := EncodeRow(identity)
		if err != nil {
			return "", err
		}
		rows = append(rows, row)
	}
	tail, err := EncodeOptional(results)
	if err != nil {
		return "", err
	}
	if tail != "" {
		rows = append(rows, tail)
	}
	return strings.Join(rows, "\n"), nil
}
