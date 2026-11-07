// Package battlecodec encodes the shared ResultCmd consumer boundary.
// Core result rows remain independent of CSV framing and presentation partitioning.
package battlecodec

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"kairisei.local/server/battlemodel"
)

// Original ResultCmd82 retires a member; ResultCmd502 separates presentation.
const commandGameOver = 82
const commandAttackPartition = 502

// resultCommandMinimumArgs starts with the CN 6.0.2 ResultCmd consumers in
// BattleResultCmdFunctionBase and BattleDirectionBase, extended by confirmed JP
// shapes (including five-argument207 and ten-argument PVP104; ordinary104
// retains its list-type column). Keeping the boundary
// here makes a malformed engine row fail before it can become a managed-client
// array overrun.
var resultCommandMinimumArgs = map[int]int{
	1: 2, 2: 1, 3: 4, 5: 10, 6: 11, 7: 0, 9: 1, 10: 0, 11: 1, 12: 2, 13: 2, 14: 2,
	20: 3, 21: 3, 22: 5, 23: 4, 24: 6, 25: 10, 27: 4, 28: 1, 29: 4, 31: 1,
	32: 2, 50: 7, 51: 12, 53: 2, 56: 11, 58: 11, 59: 0, 60: 10, 61: 4, 62: 11,
	64: 4, 65: 3, 66: 8, 67: 2, 68: 4, 69: 11, 70: 0, 71: 3, 72: 5, 74: 3, 80: 5, 81: 5, 82: 1, 83: 5, 84: 5, 86: 2, 94: 3,
	87: 4, 88: 2, 89: 2, 205: 3, 500: 5, 501: 1,
	85: 0, 100: 2, 102: 11, 103: 2, 104: 10, 106: 7, 107: 1, 108: 5, 109: 3, 110: 8,
	200: 4, 201: 7, 202: 2, 203: 2, 204: 2, 206: 5, 207: 5, 209: 8, 300: 3, 301: 4, 302: 3, 303: 7,
	304: 3, 305: 3, 306: 3, 307: 9, 308: 4, 309: 5, 314: 5, 315: 3, 318: 2, 319: 2,
	310: 9, 311: 5, 312: 2, 313: 1, 316: 4, 317: 4, 320: 5, 321: 3, 322: 9, 323: 6,
	324: 4, 325: 3, 326: 1, 328: 1, 502: 0, 504: 4, 505: 5, 506: 2, 602: 3, 700: 11, 701: 3,
}

func EncodeRow(result battlemodel.Result) (string, error) {
	if result.Command <= 0 || result.Command >= 1000 {
		return "", errors.New("battle result command is invalid")
	}
	if minimum, exists := resultCommandMinimumArgs[result.Command]; exists && len(result.Args) < minimum {
		return "", fmt.Errorf("battle result command %d requires at least %d arguments, got %d", result.Command, minimum, len(result.Args))
	}
	fields := make([]string, 1, len(result.Args)+1)
	fields[0] = strconv.Itoa(result.Command)
	for _, argument := range result.Args {
		fields = append(fields, strconv.FormatInt(argument, 10))
	}
	return strings.Join(fields, ","), nil
}

func Encode(results []battlemodel.Result) (string, error) {
	if len(results) == 0 {
		return "", errors.New("battle result group is empty")
	}
	rows := make([]string, 0, len(results))
	for index, result := range results {
		row, err := EncodeRow(result)
		if err != nil {
			return "", err
		}
		// The CN parser applies every row in a presentation group before
		// playing its animation. 82 calls notifyGameOver immediately and is
		// not a native partition. Keep local no-continue retirement outside
		// the preceding damage group, like the separate native GameOver API.
		// 502 is the native no-effect presentation separator; engine results,
		// HP, RNG and resume (107) are deliberately unchanged.
		if result.Command == commandGameOver && index > 0 &&
			results[index-1].Command != commandGameOver && results[index-1].Command != commandAttackPartition {
			rows = append(rows, "502")
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n"), nil
}

func EncodeOptional(results []battlemodel.Result) (string, error) {
	if len(results) == 0 {
		return "", nil
	}
	return Encode(results)
}
