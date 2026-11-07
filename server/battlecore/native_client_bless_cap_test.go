package battlecore

import (
	"encoding/binary"
	"testing"
)

func TestNativeClientSkillInfoBlessCapGrowthCandidate(t *testing.T) {
	// Direct JP8.0.7 get_skill_info_param observations in valentine-native-tooltip.json.
	for _, tc := range []struct{ level, per, cap int32 }{{1, 66372, 331860}, {40, 189807, 949035}, {80, 316407, 1582035}} {
		b := make([]byte, 2436)
		offset := 132 + 100 + 3*72
		binary.LittleEndian.PutUint32(b[offset:], 134)
		for i, n := range []int32{4, 0, 1, 9639, 134, 63207, 3165, 316035, 15825, 0} {
			binary.LittleEndian.PutUint32(b[offset+16+i*4:], uint32(n))
		}
		binary.LittleEndian.PutUint32(b[2432:], uint32(tc.level))
		out, e := NativeClientSkillInfo(b)
		if e != nil || out[38] != int64(tc.per) || out[39] != int64(tc.cap) {
			t.Fatalf("level%d: per/cap=%d/%d error=%v", tc.level, out[38], out[39], e)
		}
	}
}
