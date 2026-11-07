package battlecore

// CN 9ff6a emits option 6, consumed by 81db0 in role order. Only operators
// before this attack apply, and the last one replaces its physics selector.
// The target's reflection buff survives and can affect subsequent skills.
func attackIgnoresReflection(roles []CombatSkillRole, attack CombatSkillRole) bool {
	active, physics := false, ""
	for _, role := range roles {
		if role.Function == "ATTACK_AA" && role.RoleIndex == attack.RoleIndex {
			break
		}
		if role.Function == "ATK_OP_REFLECTION_INVALID" {
			active, physics = true, role.Parameters[0]
		}
	}
	return active && damagePhysicsMatches(physics, attack.Parameters[8])
}
