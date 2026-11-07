package battlecore

import (
	"encoding/binary"
	"fmt"
	"strconv"
)

// NativeClientSkillInfo is the explicit PvE query used without preparation.
func NativeClientSkillInfo(wire []byte) ([250]int64, error) {
	return NativeClientSkillInfoForMode(wire, 1)
}

// NativeClientSkillInfoForMode preserves the original five extends, five roles
// and ten signed slots per role. The query has an empty actor view; only the
// prepared mode and each role's own PVPRate affect its parameter projection.
func NativeClientSkillInfoForMode(wire []byte, mode int) ([250]int64, error) {
	var out [250]int64
	if len(wire) != 2436 {
		return out, fmt.Errorf("original SkillData requires 2436 bytes")
	}
	if mode < 0 || mode > 7 {
		return out, fmt.Errorf("invalid skill query mode %d", mode)
	}
	level := int(int32(binary.LittleEndian.Uint32(wire[2432:])))
	if level < 0 {
		return out, fmt.Errorf("negative skill level")
	}
	for extend := 0; extend < 5; extend++ {
		for index := 0; index < 5; index++ {
			offset := 132 + extend*460 + 100 + index*72
			code := int(int32(binary.LittleEndian.Uint32(wire[offset:])))
			var p [10]int32
			for n := range p {
				p[n] = int32(binary.LittleEndian.Uint32(wire[offset+16+n*4:]))
			}
			rate := int32(binary.LittleEndian.Uint32(wire[offset+64:]))
			values, err := nativeClientRoleInfo(code, p, level, mode == 3 || mode == 4, rate)
			if err != nil {
				return [250]int64{}, fmt.Errorf("extend %d role %d: %w", extend, index, err)
			}
			copy(out[(extend*5+index)*10:], values[:])
		}
	}
	return out, nil
}

func nativeClientRoleInfo(code int, p [10]int32, level int, pvp bool, pvpRate int32) ([10]int64, error) {
	var out [10]int64
	if code == 0 {
		return out, nil
	}
	role := CombatSkillRole{}
	for i, n := range p {
		role.Parameters[i] = strconv.FormatInt(int64(n), 10)
	}
	coefficient := func(base, growth int) int64 { return int64(p[base] + p[growth]*int32(level)) }
	fixed := func(base, growth int) int64 { return int64(p[base]) + int64(p[growth])*int64(level)/1000 }
	scale := func(value int64) int64 {
		// Native producers finish signed32 percentage division before the
		// tooltip converts that value through float32.
		rate := int32(100)
		if pvp {
			rate = pvpRate
		}
		return int64(float32(int32(value) * rate / 100))
	}
	sourceScale := func(value int64, source int32) int64 {
		if source == 0 || source == 1 || source == 5 || source == 6 {
			return scale(value)
		}
		return value
	}
	duration := func() { out[0] = int64(p[0]) }
	switch code {
	case 1, 2, 3, 4:
		value := int64(p[0] + p[1]*int32(level)/1000)
		if code == 3 || code == 4 {
			value = int64(p[0])
		}
		out[1] = scale(value)
		if code == 1 || code == 3 {
			out[2] = coefficient(2, 3) / 10
		} else {
			out[2] = int64(p[2]) / 10
		}
		out[3], out[4] = int64(p[4]), int64(p[6])/10
	case 5, 6, 7, 85, 145, 214:
		out[1] = scale(int64(attackOperatorLevelValue(role, level, 1)))
		if code == 6 || code == 85 {
			out[2] = scale(int64(p[3]))
		}
	case 8:
		out[1] = scale(int64(p[0] + p[1]*int32(level)/1000))
		out[2] = sourceScale(coefficient(2, 3), p[4]) / 10
		out[3] = int64(p[5])
	case 9:
		out[1] = scale(fixed(0, 1))
		out[2] = coefficient(2, 3) / 10
	case 10, 11, 57:
		out[1], out[2] = scale(coefficient(0, 1))/10, scale(int64(p[2])*int64(level))
		if code != 57 {
			out[4] = int64(p[3])
		}
	case 12:
		out[1], out[2], out[4] = sourceScale(coefficient(1, 2), p[0])/10, scale(int64(p[3])*int64(level)), int64(p[4])
	case 13:
		duration()
		out[1] = scale(int64(fixedRegenerateRoleValue(role, level, 1, 0)))
		out[2] = coefficient(3, 4) / 10
	case 14, 15:
		duration()
		out[1], out[2], out[5] = scale(coefficient(1, 2))/10, scale(int64(p[3])*int64(level)), int64(p[4])
	case 16:
		duration()
		out[1], out[2], out[5] = sourceScale(coefficient(2, 3), p[1])/10, scale(int64(p[4])*int64(level)), int64(p[5])
	case 17, 18, 23, 24, 28, 30, 31, 143, 147, 148, 191, 209, 211, 213:
		duration()
		source := p[2]
		if code == 30 || code == 31 || code == 147 {
			source = p[1]
		}
		out[1] = sourceScale(coefficient(3, 4), source) / 10
		out[2] = scale(int64(p[5] * int32(level)))
		capSlot := 6
		if code == 28 || code == 30 || code == 31 || code == 143 || code == 147 || code == 191 || code == 213 {
			capSlot = 5
		}
		out[capSlot] = int64(p[6])
		if code == 209 || code == 211 || code == 213 {
			out[capSlot+1] = int64(p[7])
		}
	case 19, 25, 29, 32, 66, 67, 103, 104, 105, 106, 142, 208, 210, 212:
		duration()
		first := (p[3] + p[4]*int32(level)) * p[2] / 1000
		second := p[5] * int32(level)
		out[1], out[2] = scale(int64(first)), scale(int64(second))
		if code == 208 || code == 210 {
			out[5] = int64(p[6])
		} else if code == 212 {
			out[4] = int64(p[6])
		}
	case 20, 22, 33, 34, 64, 75, 86, 122:
		duration()
		out[1] = scale(int64(retainedRateRoleValue(role, level))) / 10
	case 35, 36, 37:
		duration()
		if !pvp {
			out[1] = int64(retainedRateRoleValue(role, level))
		}
		if code == 35 {
			out[2] = coefficient(3, 4) / 10
		}
	case 38, 39, 40, 41, 88, 93:
		duration()
		rate, fixed, coefficient, _ := dotRoleValues(role, level, 1, 0)
		out[1], out[2], out[3] = int64(rate), scale(int64(fixed)), sourceScale(int64(coefficient), p[7])/10
	case 42:
		out[0], out[1], out[2] = int64(p[2]), scale(coefficient(0, 1))/10, scale(coefficient(3, 4))/10
	case 43, 44, 58, 59, 61, 87, 140, 141, 175, 176:
		duration()
	case 56:
		out[0] = scale(int64(p[0]))
	case 63, 144:
		duration()
		out[2], out[3] = int64(p[3]), int64(attackBarrierRoleValue(role, level))
	case 78:
		duration()
		out[1] = scale(coefficient(3, 4))
	case 79:
		duration()
		first, _, _ := fixedSourceRoleValues(role, level, 0)
		out[1], out[2] = scale(int64(retainedRateRoleValue(role, level))), scale(int64(first))
	case 81, 82, 83, 84:
		duration()
		out[1], out[3] = scale(coefficient(2, 3)), int64(p[4])
	case 91, 96:
		duration()
		out[1] = scale(int64(retainedRateRoleValue(role, level)))
	case 92:
		duration()
		first, second, _ := fixedSourceRoleValues(role, level, 0)
		out[1], out[2], out[3] = int64(minInt(int(p[1]), int(p[2]))), scale(int64(first)), sourceScale(int64(second), p[7])/10
	case 94:
		duration()
		role.Function = "COST_BLOCK"
		out[1] = int64(persistentRoleValue(role, level, &battlePlayer{}, 1))
	case 113:
		duration()
		out[1] = scale(int64(retainedRateRoleValue(role, level))) / 100
	case 121:
		duration()
		out[1] = int64(retainedRateRoleValue(role, level))
	case 123, 131, 160, 197:
		duration()
		out[1], out[2] = scale(coefficient(1, 2)), scale(fixed(3, 4))
	case 124, 125, 126, 127, 128, 161, 162, 163, 164, 198, 199, 200, 201, 202, 219, 220, 221, 222:
		duration()
		out[1], out[2] = scale(coefficient(3, 4)), scale(fixed(1, 2))
	case 129, 165, 203:
		duration()
		out[1] = scale(coefficient(1, 2))
	case 130, 132, 215, 216, 217, 218:
		duration()
		out[1], out[2] = scale(coefficient(3, 4))/10, scale(fixed(1, 2))
	case 134:
		out[7], out[8], out[9] = coefficient(3, 4), coefficient(5, 6), coefficient(7, 8)
	case 21, 46, 47, 52, 53, 54, 55, 76, 77, 135, 136:
		out[0] = int64(linearRoleValue(role, 0, 1, level))
	case 26, 168, 174:
		out[1] = scale(int64(linearRoleValue(role, 0, 1, level)))
	case 172:
		out[1] = -scale(int64(linearRoleValue(role, 0, 1, level)))
	case 49, 80:
		duration()
		out[1] = scale(int64(retainedRateRoleValue(role, level)))
	case 120:
		duration()
		out[1] = int64(retainedRateRoleValue(role, level))
	case 118, 119, 169, 170:
		out[1] = int64(retainedRateRoleValue(role, level))
		if code != 170 {
			out[1] = scale(out[1])
		} else {
			out[2] = int64(p[0])
		}
	case 50:
		out[1] = int64(p[2])
	case 95:
		out[0], out[1] = int64(p[1]), int64(p[0])
	case 97, 98, 102:
		duration()
		out[1], out[2], out[5], out[6] = scale(int64(retainedRateRoleValue(role, level))), int64(p[3]), int64(p[6]), int64(p[7])
	case 99, 100, 115, 116, 155, 182:
		duration()
		out[1] = int64(p[1])
		if code != 155 && code != 182 {
			out[1] = scale(out[1])
		}
	case 171:
		duration()
		out[1] = int64(minInt(100, int(p[1]))) / 10
	case 101, 156, 177:
		duration()
		out[1], out[5], out[6], out[7] = int64(p[1]), int64(p[5]), int64(p[6]), int64(p[7])
		if code != 101 {
			out[4] = int64(p[4])
		}
	case 107:
		duration()
		role.Function = "ENCHANT"
		out[1] = scale(int64(persistentRoleValue(role, level, &battlePlayer{}, 1)))
	case 110, 111, 112:
		out[1], out[2] = scale(coefficient(1, 2))/10, scale(fixed(3, 4))
	case 137, 138:
		out[0] = -int64(p[0])
	case 146:
		duration()
		out[1], out[2] = int64(p[1]), scale(int64(p[2]))
	case 149, 151, 153:
		duration()
		out[1] = scale(coefficient(2, 3)) / 10
		if code == 153 {
			out[3] = int64(p[1])
		} else {
			out[2] = int64(p[1])
		}
	case 150, 152, 154:
		duration()
		out[2] = scale(coefficient(2, 3))
		if code != 154 {
			out[4] = int64(p[1])
		}
	case 157:
		duration()
		out[2] = int64(p[2])
	case 158:
		duration()
		out[1] = int64(p[1]) / 10
	case 178, 179, 180, 181:
		duration()
		out[1] = int64(p[2])
	case 166, 167, 204:
		if code != 167 {
			duration()
		}
		out[1], out[2] = coefficient(2, 3), int64(p[1])
	case 183, 186, 189, 192, 195:
		duration()
		first := (p[2] + p[3]*int32(level)) * p[1] / 1000
		second := p[4] * int32(level)
		out[1], out[2] = scale(int64(first)), scale(int64(second))
	case 184, 185, 187, 188, 190, 193, 194, 196:
		duration()
		out[1], out[2] = sourceScale(coefficient(2, 3), p[1])/10, scale(int64(p[4])*int64(level))
		capSlot := 6
		if code == 190 || code == 193 || code == 194 || code == 196 {
			capSlot = 5
		}
		out[capSlot] = int64(p[5])
	case 205, 207:
		out[1] = int64(p[1])
	case 27, 45, 48, 51, 60, 62, 65, 68, 69, 70, 71, 72, 73, 74, 89, 90, 108, 109, 114, 117, 133, 139, 159, 173, 206:
		// These original query producers publish no numbered parameters.
	default:
		return out, fmt.Errorf("native skill info role %d is not connected", code)
	}
	return out, nil
}
