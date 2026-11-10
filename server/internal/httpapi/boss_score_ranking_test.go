package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/operations"
	"kairisei.local/server/internal/testfixture"
)

type scoreRankingFixture struct {
	bossID, userID, offset, limit int
	mode                          gamestate.BattleScoreMode
}

func (f *scoreRankingFixture) BossScoreRanking(bossID int, mode gamestate.BattleScoreMode, userID, offset, limit int) (game.BossScoreRankingPage, error) {
	f.bossID, f.userID, f.offset, f.limit = bossID, userID, offset, limit
	f.mode = mode
	return game.BossScoreRankingPage{Total: 1, Entries: []game.BossScoreRank{{Rank: 1, UserID: userID, Name: "亚瑟", Grade: 7, Score: 8800, AchievedAt: 1000}}}, nil
}

func TestBossScoreRankingRouteAndBannerTarget(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.Onboarding = gamestate.OnboardingState{Step: 9}
	state.TeamBattleRewards = append(state.TeamBattleRewards, gamestate.TeamBattleRewardProfile{
		BossID: 37010101, ScorePolicy: &gamestate.TeamBattleScorePolicy{Rate: 1},
	})
	fixture := &scoreRankingFixture{}
	a := &API{initialState: state, account: testAccount(t, func(s *gamestate.State) {
		s.Onboarding = state.Onboarding
	}), bossScoreRankings: fixture, baseURL: "http://127.0.0.1:26040"}
	path := "home-banners/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"
	a.homeBanners = operations.HomeBannerConfiguration{Policy: operations.HomeBannerPolicy{Custom: true, Items: []operations.HomeBannerEntry{
		{Title: "Boss榜", Enabled: true, CoverPath: path, Target: "boss_score_rank", TargetID: 37010101},
	}}}
	bannerRequest := httptest.NewRequest("POST", "/HomeShow", nil)
	rows := a.configuredHomeBanners(bannerRequest, time.Unix(100, 0))
	if len(rows) != 1 || rows[0].(map[string]any)["open_url"] != "boss_score_rank:37010101" {
		t.Fatal("banner did not target its selected score boss", rows)
	}
	request := httptest.NewRequest("POST", "/BossScoreRankingShow", strings.NewReader(`{"bossid":37010101,"mode":0}`))
	response := httptest.NewRecorder()
	a.bossScoreRankingShow(response, request)
	if response.Code != 200 || fixture.bossID != 37010101 || fixture.userID != state.User.UserID || fixture.offset != 0 || fixture.limit != 50 {
		t.Fatalf("ranking request contract: %d %+v", response.Code, fixture)
	}
	segments := strings.Split(response.Body.String(), "\n")
	if len(segments) != 3 {
		t.Fatal("protocol envelope changed", response.Body.String())
	}
	var method struct {
		BossID  int                  `json:"bossid"`
		Total   int                  `json:"total"`
		Entries []game.BossScoreRank `json:"entries"`
	}
	if err := json.Unmarshal([]byte(segments[1]), &method); err != nil || method.BossID != 37010101 || method.Total != 1 || len(method.Entries) != 1 || method.Entries[0].Grade != 7 {
		t.Fatal("ranking response contract", method, err)
	}
	state.DisabledTeamBattleBossIDs = map[int]bool{37010101: true}
	a.initialState = state
	if rows := a.configuredHomeBanners(bannerRequest, time.Unix(100, 0)); len(rows) != 1 {
		t.Fatal("closed score boss no longer advertises its final ranking", rows)
	}
	response = httptest.NewRecorder()
	a.bossScoreRankingShow(response, httptest.NewRequest("POST", "/BossScoreRankingShow", strings.NewReader(`{"bossid":37010101,"mode":1}`)))
	if response.Code != 200 || fixture.mode != 1 {
		t.Fatal("closed score boss refused its ranking", response.Code)
	}
}
