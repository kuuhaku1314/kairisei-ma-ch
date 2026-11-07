package game

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestScoreSettlementKeepsBestAndDoesNotRepeatAwards(t *testing.T) {
	p := &gamestate.TeamBattleScorePolicy{SourceState: "PLACEHOLDER_LOCAL_CLEAR_TURN_SCORE", BaseScore: 1000000, BestTurn: 4}
	for i := 0; i < 9; i++ {
		p.Grades = append(p.Grades, gamestate.TeamBattleScoreGrade{Score: 1000000 * int64(i+1), Crystal: 50})
	}
	s := onboardingTestStore()
	s.teamBattleScores = map[int]gamestate.TeamBattleScoreProgress{}
	s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(`{"9":[{"0":1,"9":0,"10":[{"0":123,"10":0}]}],"10":[],"11":[],"12":[]}`))
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: 123, ScorePolicy: p}}
	play := func(turns int) teamBattleSettlement {
		t.Helper()
		s.activeBattle = &TeamBattleContext{BossID: 123}
		r, err := s.CompleteTeamBattle(123, true, profiles, TeamBattleDropReport{Turns: turns})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := play(12)
	if s.coinFree != 50 || first.ScoreInfo[0].(map[string]any)["high_score"] != int64(0) {
		t.Fatal("first award or previous-record display is wrong")
	}
	if _, err := s.CompleteTeamBattle(123, true, profiles); err == nil {
		t.Fatal("duplicate result allowed without a start")
	}
	// Persist/reload the same field carried by the account save, then improve the score.
	raw, err := json.Marshal(gamestate.State{TeamBattleScores: cloneTeamBattleScores(s.teamBattleScores)})
	if err != nil {
		t.Fatal(err)
	}
	var restored gamestate.State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	s.teamBattleScores = cloneTeamBattleScores(restored.TeamBattleScores)
	second := play(4)
	if s.coinFree != 450 || len(second.Score.Rewards) != 8 || s.teamBattleScores[123].HighScore != 9000000 {
		t.Fatal("improved score did not grant only newly reached grades")
	}
	third := play(20)
	if s.coinFree != 450 || len(third.Score.Rewards) != 0 || s.teamBattleScores[123].HighScore != 9000000 {
		t.Fatal("lower score repeated rewards or erased best")
	}
	high, rows := s.ScoreLineup(123, profiles, gamestate.BattleScoreMultiplayer)
	if high != 9000000 || len(rows) != 9 {
		t.Fatal("lineup lost saved best")
	}
}

func TestDamageScoreTimeoutFreezesPolicyAndAwardsOnlyNewGrades(t *testing.T) {
	p := &gamestate.TeamBattleScorePolicy{SourceState: "LOCAL_POLICY_DAMAGE_SCORE", EndTurn: 5, Rate: 100}
	for i := range 9 {
		p.Grades = append(p.Grades, gamestate.TeamBattleScoreGrade{Score: int64(i+1) * 100, Rewards: []gamestate.Reward{{Type: 10, Num: 10, CardSkillLevels: []int16{}}}})
	}
	s := onboardingTestStore()
	s.teamBattleScores = map[int]gamestate.TeamBattleScoreProgress{}
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: 123, ScorePolicy: gamestate.CloneTeamBattleScorePolicy(p), ResultRewards: []gamestate.Reward{{Type: 4, Num: 100}}}}
	s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: gamestate.CloneTeamBattleScorePolicy(p)}
	// A policy change during the fight must not multiply the accepted score.
	profiles[0].ScorePolicy.Rate = 1000
	result, err := s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: 250})
	if err != nil || s.coinFree != 20 || s.gold != 0 || len(result.ScoreInfo) != 1 || s.teamBattleScores[123].HighScore != 250 {
		t.Fatalf("timeout settlement: coin=%d gold=%d result=%+v err=%v", s.coinFree, s.gold, result, err)
	}
	if _, err := s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: 900}); err == nil {
		t.Fatal("timeout retry awarded without a new entry")
	}
	encoded, err := json.Marshal(s.teamBattleScores)
	if err != nil {
		t.Fatal(err)
	}
	s.teamBattleScores = nil
	if err := json.Unmarshal(encoded, &s.teamBattleScores); err != nil {
		t.Fatal(err)
	}
	s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: p}
	result, err = s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: 450})
	if err != nil || s.coinFree != 40 || len(result.Score.Rewards) != 2 || s.teamBattleScores[123].Claimed != 15 {
		t.Fatal("reloaded progress repeated or lost grade rewards", result, err)
	}
	s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: p}
	_, err = s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: 150})
	if err != nil || s.coinFree != 40 || s.teamBattleScores[123].HighScore != 450 {
		t.Fatal("lower result erased progress", err)
	}
	// A late JP body alone has one trillion HP; its parts add valid score.
	s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: p}
	_, err = s.CompleteTeamBattle(123, false, profiles, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: 1000000000250})
	if err != nil || s.teamBattleScores[123].HighScore != 1000000000250 {
		t.Fatal("valid body and part damage above one trillion was rejected", err)
	}
}

func TestSoloDamageScoreSeparatesGradesAndExcludesRentalRank(t *testing.T) {
	p := &gamestate.TeamBattleScorePolicy{SourceState: "LOCAL_POLICY_DAMAGE_SCORE", EndTurn: 5, Rate: 150}
	for i := range 9 {
		p.Grades = append(p.Grades, gamestate.TeamBattleScoreGrade{Score: int64(i+1) * 100, Rewards: []gamestate.Reward{{Type: 10, Num: 10, CardSkillLevels: []int16{}}}})
	}
	s := onboardingTestStore()
	s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(`{"9":[{"0":1,"9":0,"10":[{"0":123,"10":0}]}],"10":[],"11":[],"12":[]}`))
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: 123, ScorePolicy: gamestate.CloneTeamBattleScorePolicy(p), ResultRewards: []gamestate.Reward{{Type: 4, Num: 100}}}}
	profiles[0].ScorePolicy.Rate = 1000
	play := func(solo, clear bool, damage int64, partners []TeamBattleResultPartner) teamBattleSettlement {
		t.Helper()
		s.activeBattle = &TeamBattleContext{BossID: 123, ScorePolicy: p, SelectedPartners: partners}
		r, err := s.CompleteTeamBattle(123, clear, profiles, TeamBattleDropReport{SoloChallenge: solo, ScoreVerified: true, ScoreDamage: damage, Turns: 4})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	failed := play(true, false, 250, nil)
	if s.coinFree != 30 || s.gold != 0 || s.teamBattleSoloScores[123].HighScore != 375 || s.teamBattleScores[123].Claimed != 0 || failed.RankingScore == nil || failed.RankingScore.Score != 375 {
		t.Fatal("solo defeat lost actual score or mixed modes", failed)
	}
	lower := play(true, false, 100, nil)
	if s.coinFree != 30 || len(lower.Score.Rewards) != 0 {
		t.Fatal("lower solo score repeated awards")
	}
	friend := play(true, false, 500, []TeamBattleResultPartner{{IsSelf: false}})
	if s.coinFree != 70 || friend.RankingScore != nil || s.teamBattleSoloScores[123].HighScore != 750 {
		t.Fatal("rental challenge lost grades or entered own-deck ranking", friend)
	}
	play(false, false, 500, nil)
	if s.coinFree != 140 || s.teamBattleScores[123].HighScore != 750 || s.teamBattleScores[123].Claimed != 127 {
		t.Fatal("multiplayer did not have independent grade claims")
	}
	won := play(true, true, 600, []TeamBattleResultPartner{{IsSelf: true}})
	if s.coinFree != 160 || s.gold != 100 || won.RankingScore == nil || won.RankingScore.Grade != 8 || won.RankingScore.Score != 900 {
		t.Fatal("solo clear ignored frozen multiplier or repeated grades", won)
	}
	if _, err := s.CompleteTeamBattle(123, true, profiles, TeamBattleDropReport{SoloChallenge: true, ScoreVerified: true, ScoreDamage: 600}); err == nil {
		t.Fatal("solo retry accepted without an entry")
	}
	solo, _ := s.ScoreLineup(123, profiles, gamestate.BattleScoreSolo)
	multi, _ := s.ScoreLineup(123, profiles, gamestate.BattleScoreMultiplayer)
	if solo != 900 || multi != 750 {
		t.Fatal("reward lineup mixed participation modes", solo, multi)
	}
}
