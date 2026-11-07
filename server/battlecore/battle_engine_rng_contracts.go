package battlecore

import "fmt"

func validateXorShift128Contracts() error {
	zero := newXorShift128(0)
	if zero.x != 123456789 || zero.y != 362436069 || zero.z != 521288629 || zero.w != 88675123 {
		return fmt.Errorf("native xor128 zero-seed state changed: %+v", zero)
	}
	one := newXorShift128(1)
	if one.x != 123456788 || one.y != 362435813 || one.z != 521354165 || one.w != 71897907 {
		return fmt.Errorf("native xor128 seed-one state changed: %+v", one)
	}
	want := []uint32{3718467011, 442046446, 2618378627, 3498901945, 394756926, 3316842567, 3556250659, 1807763244}
	for index, expected := range want {
		if value := one.next(); value != expected {
			return fmt.Errorf("native xor128 seed-one draw %d is %d, want %d", index+1, value, expected)
		}
	}
	return nil
}
