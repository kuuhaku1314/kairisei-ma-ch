package httpapi

import (
	"strings"

	"kairisei.local/server/battlecodec"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) scoreBossID(bossID int) int {
	if source := a.initialState.TeamBattleOwnDeckSources[bossID]; source != 0 {
		return source
	}
	return bossID
}

func soloScoreStartPolicy(policy *gamestate.TeamBattleScorePolicy) any {
	if policy == nil {
		return nil
	}
	return map[string]any{"rate": policy.Rate, "end_turn": policy.EndTurn, "member_dead_end": policy.MemberDeadEnd}
}

// Called only after the native report envelope was validated. Preserve turn
// metadata for score policies; damage is supplied by the local battle bridge.
func nativeScoreTurns(commands []string) int {
	turns := 0
	for _, wave := range commands {
		for _, line := range strings.Split(wave, "\n") {
			fields := strings.Split(strings.TrimSpace(line), ",")
			if len(fields) == battlecodec.InputCommandFieldCount && strings.TrimSpace(fields[1]) == "10" {
				turns++
			}
		}
	}
	return max(1, turns)
}

func scoreInfoWire(info []any) []any {
	if info == nil {
		return []any{}
	}
	return info
}
