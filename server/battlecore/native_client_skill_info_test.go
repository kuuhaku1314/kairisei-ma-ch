package battlecore

import (
	"encoding/binary"
	"strconv"
	"testing"
)

func TestNativeClientSkillInfoParameterAndEXRoles(t *testing.T) {
	// Expected rows were observed from the original tooltip API with these
	// controlled growth values (R106); no combat state is involved.
	cases := []struct {
		code int32
		want [10]int64
	}{
		{17, [10]int64{3, 52, 400, 0, 0, 0, 5}},
		{23, [10]int64{3, 52, 400, 0, 0, 0, 5}},
		{28, [10]int64{3, 52, 400, 0, 0, 5}},
		{30, [10]int64{3, 52, 400, 0, 0, 5}},
		{123, [10]int64{3, 261, 203}},
		{124, [10]int64{3, 523, 101}},
		{125, [10]int64{3, 523, 101}},
		{126, [10]int64{3, 523, 101}},
		{127, [10]int64{3, 523, 101}},
		{128, [10]int64{3, 523, 101}},
		{129, [10]int64{3, 261}},
		{130, [10]int64{3, 52, 101}},
		{131, [10]int64{3, 261, 203}},
		{132, [10]int64{3, 52, 101}},
		{133, [10]int64{}},
		{134, [10]int64{0, 0, 0, 0, 0, 0, 0, 523, 405}},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(int(tc.code)), func(t *testing.T) {
			wire := make([]byte, 2436)
			put := func(offset int, n int32) { binary.LittleEndian.PutUint32(wire[offset:], uint32(n)) }
			// Exercise a noninitial extend/role so the complete projection must
			// retain the original five-by-five layout.
			offset := 132 + 2*460 + 100 + 4*72
			put(offset, tc.code)
			for i, n := range []int32{3, 101, 2, 203, 4, 5, 5, 0, 0, 0} {
				put(offset+16+i*4, n)
			}
			put(2432, 80)
			got, err := NativeClientSkillInfo(wire)
			if err != nil {
				t.Fatal(err)
			}
			var want [250]int64
			copy(want[140:], tc.want[:])
			if got != want {
				t.Fatalf("tooltip row differs: got %v, want %v", got[140:150], tc.want)
			}
		})
	}
}
