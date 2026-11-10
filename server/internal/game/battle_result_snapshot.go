package game

import "kairisei.local/server/internal/gamestate"

// TeamBattleResultSnapshot freezes granted rewards and the battle's members.
// It contains no client response, active session or mutable account snapshot.
// Replays render it through the current protocol and read current user balances.
type TeamBattleResultSnapshot struct {
	Return      *TeamBattleReturn        `json:"return,omitempty"`
	ClearRating *TeamBattleClearRating   `json:"clear_rating,omitempty"`
	IsClear     int                      `json:"is_clear"`
	MemberType  int                      `json:"member_type"`
	Partners    []TeamBattleResultMember `json:"partners"`
	Result      PresentReceiveResult     `json:"result"`
	FirstClear  PresentReceiveResult     `json:"first_clear"`
	Score       PresentReceiveResult     `json:"score"`
	Fame        []TeamBattleFameAward    `json:"fame"`
	Host        []TeamBattleFameAward    `json:"host"`
	ScoreInfo   []any                    `json:"score_info"`
}

// TeamBattleReturn is an immutable destination from the accepted run and its
// settled progress. Result retries must not re-resolve publication or progress.
type TeamBattleReturn struct {
	BossID     int                       `json:"boss_id"`
	TowerID    int                       `json:"tower_id"`
	TowerFloor int                       `json:"tower_floor"`
	StageType  gamestate.BattleStageType `json:"stage_type"`
}

type TeamBattleResultMember struct {
	MemberType int              `json:"member_type"`
	IsOnline   int              `json:"is_online"`
	Member     gamestate.Friend `json:"member"`
}

func (settlement teamBattleSettlement) ResultSnapshot(isClear, memberType int, partners []TeamBattleResultMember) TeamBattleResultSnapshot {
	return TeamBattleResultSnapshot{
		Return:  settlement.Return,
		IsClear: isClear, MemberType: memberType, Partners: partners,
		Result: settlement.Result, FirstClear: settlement.FirstClear, Score: settlement.Score,
		ClearRating: settlement.ClearRating,
		Fame:        settlement.Fame, Host: settlement.Host, ScoreInfo: settlement.ScoreInfo,
	}
}
