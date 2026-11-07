package battlecore

import "errors"

// ErrSummonSelectionUnsupported identifies a mode or adapter without summon
// input. Configured alchemist core instances implement the selection directly.
var ErrSummonSelectionUnsupported = errors.New("summon selection is unavailable for this mode")

// CheckSummonSelectionInput shares the configured mode/mask boundary with the
// local ABI. The core separately validates live slots and source registration.
func CheckSummonSelectionInput(mask int, alchemist bool) error {
	if err := checkSummonMask(mask); err != nil {
		return err
	}
	if mask != 0 && !alchemist {
		return ErrSummonSelectionUnsupported
	}
	return nil
}

func checkSummonMask(mask int) error {
	if mask < 0 || mask&^31 != 0 {
		return errors.New("summon selection mask exceeds five card positions")
	}
	return nil
}
