package battlecore

import "fmt"

// NativeClientUserParameters exposes the existing engine's base projection to
// a client preparing a deck. It does not allocate a battle or publish rewards.
// Input follows the original UserData's eighteen signed int32 fields.
func NativeClientUserParameters(member int, user [18]int32, alchemist bool) (BattleResult, error) {
	if member < 1 || member > 8 {
		return BattleResult{}, fmt.Errorf("invalid native client member %d", member)
	}
	limit := initialParameterLimit(alchemist)
	player := battlePlayer{Alchemist: alchemist,
		MemberType: member, HP: int(user[1]), MaxHP: int(user[2]), BaseMaxHP: int(user[2]),
		Attack: int(user[3]), BaseAttack: int(user[3]), Magic: int(user[4]), BaseMagic: int(user[4]),
		Recovery: int(user[5]), BaseRecovery: int(user[5]),
		LimitAttack: limit, LimitMagic: limit, LimitRecovery: limit,
	}
	return playerBaseParameterResult(&player), nil
}
