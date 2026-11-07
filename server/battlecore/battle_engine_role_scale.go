package battlecore

import "strings"

// Percentages apply at each native producer boundary, before source products
// and Chain additions. One scale value is shared by display and execution.
type battleRoleScale struct {
	pvp     bool
	percent int32
}

func (engine *BattleEngine) roleScale(role CombatSkillRole) battleRoleScale {
	if engine.pvp {
		return battleRoleScale{pvp: true, percent: int32(role.PVPRate)}
	}
	return battleRoleScale{percent: 100}
}
func (scale battleRoleScale) fixed(value int32) int32 { return value * scale.percent / 100 }
func (scale battleRoleScale) source(value int32, parameter string) int32 {
	switch strings.ToUpper(parameter) {
	case "HP", "MAX_HP", "DEF", "MDEF":
		return scale.fixed(value)
	}
	return value
}
