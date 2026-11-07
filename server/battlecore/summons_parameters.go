package battlecore

// SummonsParameterRange is one initial/maximum pair from the summon master.
type SummonsParameterRange struct {
	Initial int32
	Maximum int32
}

// SummonsParameters applies the original JP 8.0.7 parameter interpolation to
// HP, ATK, INT, MND, DEF and MDEF. It is a read-only query, independent of a
// battle instance. Keep the original signed widths, two divisions and upper
// clamp; a descending range is not equivalent to clamping the level first.
func SummonsParameters(level, maximumLevel int32, ranges [6]SummonsParameterRange) [6]int32 {
	var result [6]int32
	for index, pair := range ranges {
		switch {
		case level <= 1:
			result[index] = pair.Initial
		case maximumLevel <= 1:
			result[index] = pair.Maximum
		default:
			delta := pair.Maximum - pair.Initial
			growth := int64(level-1) * int64(delta) * 1000
			value := pair.Initial + int32((growth/int64(maximumLevel-1))/1000)
			if value > pair.Maximum {
				value = pair.Maximum
			}
			result[index] = value
		}
	}
	return result
}
