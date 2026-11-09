package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/operations"
	"kairisei.local/server/internal/testfixture"
)

type homeBannerSink struct {
	value operations.HomeBannerConfiguration
}

func (s *homeBannerSink) ApplyHomeBannerConfiguration(value operations.HomeBannerConfiguration) {
	s.value = value
}

func TestHomeBannersUploadPolicyConflictRestartAndPublication(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	days := []gamestate.LoginBonusDay{{Day: 1, Comment: "签到", Reward: gamestate.Reward{Type: 10, Num: 1, CardSkillLevels: []int16{}}}}
	o.playerDefaults = &PlayerPolicy{Notice: noticePolicy{Title: "公告", Body: "欢迎"}, Login: loginRewards{days, days, days}, StoryCrystals: 50, Navigators: []game.NaviSetting{}}
	o.playerLoginBase = gamestate.LoginBonusPolicy{Cycle: days, Beginner: days, TotalMilestones: days}
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: o, gachaCoverDir: t.TempDir(), homeBannerDir: t.TempDir(), catalogByKey: map[string]AdminCatalogEntry{}}
	router := chi.NewRouter()
	router.Post("/upload", a.uploadHomeBanner)
	router.Post("/cover", a.uploadGachaCover)
	router.Put("/policy", a.savePlayerPolicy)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 128, 64))); err != nil {
		t.Fatal(err)
	}
	upload := map[string]any{"data_base64": base64.StdEncoding.EncodeToString(encoded.Bytes())}
	var result struct {
		CoverPath string `json:"cover_path"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/upload", upload, 200), &result); err != nil {
		t.Fatal(err)
	}
	path := result.CoverPath
	if !homeBannerPathPattern.MatchString(path) {
		t.Fatal("banner upload left the home-banners folder", path)
	}
	var again struct {
		CoverPath string `json:"cover_path"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/upload", upload, 200), &again); err != nil || again.CoverPath != path {
		t.Fatal("identical banner image lost its shared content address", err)
	}
	config := *o.playerDefaults
	config.HomeBanners = operations.HomeBannerPolicy{Custom: true, Items: []operations.HomeBannerEntry{
		{Title: "第二张先显示", Enabled: true, CoverPath: path, Target: "none"},
		{Title: "公告", Enabled: true, CoverPath: path, Target: "notice", StartUnix: 100, EndUnix: 200},
	}}
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": config}, 200)
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": config}, 409)
	bad := config
	bad.HomeBanners.Items = append([]operations.HomeBannerEntry(nil), config.HomeBanners.Items...)
	bad.HomeBanners.Items[0].CoverPath = "home-banners/ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff.png"
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	// Pool covers and home banners share validation rules, but keep separate asset references.
	var cover struct {
		CoverPath string `json:"cover_path"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/cover", upload, 200), &cover); err != nil {
		t.Fatal(err)
	}
	bad.HomeBanners.Items[0].CoverPath = cover.CoverPath
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	bad.HomeBanners.Items[0] = config.HomeBanners.Items[0]
	bad.HomeBanners.Items[0].Target = "https://unconfigured.example/"
	testfixture.CallContentAdmin(t, router, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.playerDefaults, restarted.playerLoginBase = o.playerDefaults, o.playerLoginBase
	if err := restarted.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	if got := restarted.playerPolicy.Load(); got.Revision != 1 || !reflect.DeepEqual(got.Value.HomeBanners, config.HomeBanners) {
		t.Fatal("banner policy changed on restart", got)
	}
	var sink homeBannerSink
	restarted.Runtime.PrepareBusiness(&sink)
	if !reflect.DeepEqual(sink.value.Policy, config.HomeBanners) {
		t.Fatal("saved policy not published to the business handler")
	}
	request := httptest.NewRequest("GET", "/home-banners/"+path[len("home-banners/"):], nil)
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("file", path[len("home-banners/"):])
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, ctx))
	response := httptest.NewRecorder()
	ServeHomeBanner(a.homeBannerDir)(response, request)
	if response.Code != 200 || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || !bytes.Equal(response.Body.Bytes(), encoded.Bytes()) {
		t.Fatal("immutable banner route differs", response.Code)
	}
	bossID := 37010101
	o.content = &contentStore{base: gamestate.State{TeamBattleRewards: []gamestate.TeamBattleRewardProfile{{
		BossID: bossID, ScorePolicy: &gamestate.TeamBattleScorePolicy{Rate: 1},
	}}}, bosses: map[int]DropBoss{bossID: {BossID: bossID, Name: "海景房", Difficulty: "圣剑杯"}}}
	bossBanner := operations.HomeBannerPolicy{Custom: true, Items: []operations.HomeBannerEntry{{
		Title: "海景房排行榜", Enabled: true, CoverPath: path, Target: "boss_score_rank", TargetID: bossID,
	}}}
	if err := a.validateHomeBannerAssets(bossBanner); err != nil || a.homeBannerScoreBosses()[bossID] == "" {
		t.Fatal("published score boss is unavailable to banner editor", err)
	}
	bossBanner.Items[0].TargetID++
	if err := a.validateHomeBannerAssets(bossBanner); err == nil {
		t.Fatal("banner accepted a non-score boss")
	}
	o.content.configuration = game.ContentConfiguration{Revision: 1, State: gamestate.State{
		TeamBattleRewards:         []gamestate.TeamBattleRewardProfile{{BossID: bossID, ScorePolicy: &gamestate.TeamBattleScorePolicy{Rate: 1}}},
		DisabledTeamBattleBossIDs: map[int]bool{bossID: true},
	}}
	bossBanner.Items[0].TargetID = bossID
	if err := a.validateHomeBannerAssets(bossBanner); err != nil || a.homeBannerScoreBosses()[bossID] == "" {
		t.Fatal("closed score boss lost its ranking banner", err)
	}
	o.gachaBases = map[int]gamestate.GachaProfile{
		101: {GachaID: 101, GroupID: 30, Name: "同组单抽"},
		102: {GachaID: 102, GroupID: 30, Name: "同组连抽"},
	}
	groupBanner := operations.HomeBannerPolicy{Items: []operations.HomeBannerEntry{{
		Title: "卡池组", Enabled: true, CoverPath: path, Target: "gacha_group", TargetID: 30,
	}}}
	if groups := a.homeBannerGachas(); len(groups) != 1 || groups[30] == "" {
		t.Fatal("banner picker must expose one destination per group", groups)
	}
	if err := a.validateHomeBannerAssets(groupBanner); err != nil {
		t.Fatal("banner rejected the existing group", err)
	}
	groupBanner.Items[0].TargetID = 102
	if err := a.validateHomeBannerAssets(groupBanner); err == nil {
		t.Fatal("banner accepted an individual draw ID as a group")
	}
}
