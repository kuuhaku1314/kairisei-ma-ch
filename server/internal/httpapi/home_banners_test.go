package httpapi

import (
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/operations"
)

func TestConfiguredHomeBannersOrderVisibilityAndContentAddress(t *testing.T) {
	a := &API{baseURL: "http://127.0.0.1:26040"}
	a.account = testAccount(t, func(state *gamestate.State) {
		state.Onboarding = gamestate.OnboardingState{ConfigVersion: 2, Step: 9}
	})
	path := "home-banners/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"
	a.homeBanners = operations.HomeBannerConfiguration{Policy: operations.HomeBannerPolicy{Custom: true, Items: []operations.HomeBannerEntry{
		{Title: "第一张", Enabled: true, CoverPath: path, Target: "notice"},
		{Title: "停用", Enabled: false, CoverPath: path, Target: "none"},
		{Title: "将来", Enabled: true, CoverPath: path, Target: "none", StartUnix: 200},
		{Title: "到期", Enabled: true, CoverPath: path, Target: "none", EndUnix: 100},
		{Title: "不存在的卡池", Enabled: true, CoverPath: path, Target: "gacha", TargetID: 2147483000},
		{Title: "第二张", Enabled: true, CoverPath: path, Target: "none"},
	}}}
	request := httptest.NewRequest("POST", "/HomeShow", nil)
	rows := a.configuredHomeBanners(request, time.Unix(100, 0))
	if len(rows) != 2 {
		t.Fatal("disabled, scheduled or unavailable target leaked", rows)
	}
	for i, command := range []string{a.baseURL + "/disabled/web", "none"} {
		row := rows[i].(map[string]any)
		if row["open_url"] != command || row["image_url"] != a.baseURL+"/local/"+path {
			t.Fatal("image/action/order mismatch", row)
		}
	}
	a.homeBanners.ImageBaseURL = "https://media.example.test/assets/"
	row := a.configuredHomeBanners(request, time.Unix(100, 0))[0].(map[string]any)
	if row["image_url"] != "https://media.example.test/assets/"+path {
		t.Fatal("shared storage base not used", row)
	}
	a.homeBanners.Policy.Items = nil
	if got := a.configuredHomeBanners(request, time.Unix(100, 0)); !reflect.DeepEqual(got, []any{}) {
		t.Fatal("custom empty policy did not hide image banners", got)
	}
	// The training entrance retains ownership of Home while it is active.
	a.account = testAccount(t, func(state *gamestate.State) { state.Onboarding = gamestate.OnboardingState{ConfigVersion: 2, Step: 1} })
	a.homeBanners.Policy.Items = []operations.HomeBannerEntry{{Title: "训练中隐藏", Enabled: true, CoverPath: path, Target: "none"}}
	if rows := a.configuredHomeBanners(request, time.Unix(100, 0)); len(rows) != 0 {
		t.Fatal("custom banner interrupted onboarding", rows)
	}
}

func TestHomeGachaGroupBannerResolvesAVisiblePublishedDraw(t *testing.T) {
	now := time.Now()
	a := &API{account: testAccount(t, func(state *gamestate.State) {
		state.Onboarding = gamestate.OnboardingState{ConfigVersion: 2, Step: 9}
		state.Gachas = []gamestate.GachaProfile{
			{GroupID: 30, GachaID: 101, PayType: 4, PayTypeID: 2000, Price: 10000, CardNum: 1, CardNumMax: 1, EndTime: int(now.Add(time.Hour).Unix()), CardIDs: []int{11000045}, CardWeights: []int{1}},
			{GroupID: 30, GachaID: 102, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1, EndTime: int(now.Add(time.Hour).Unix()), CardIDs: []int{11000045}, CardWeights: []int{1}},
			{GroupID: 31, GachaID: 103, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1, EndTime: int(now.Add(time.Hour).Unix()), CardIDs: []int{11000045}, CardWeights: []int{1}},
		}
		state.GachaSelections, state.GachaDailyClaims = nil, nil
	})}
	a.homeBanners.Policy.Items = []operations.HomeBannerEntry{{Title: "目标组", Enabled: true, Target: "gacha_group", TargetID: 30}}
	request := httptest.NewRequest("POST", "/HomeShow", nil)
	rows := a.configuredHomeBanners(WithGachaPublication(request, func(int) bool { return true }), now)
	if len(rows) != 1 || rows[0].(map[string]any)["open_url"] != "gacha:102" {
		t.Fatalf("ticket stock blocked its visible crystal alternative: %+v", rows)
	}
	rows = a.configuredHomeBanners(WithGachaPublication(request, func(id int) bool { return id == 103 }), now)
	if len(rows) != 0 {
		t.Fatalf("unpublished group was advertised or redirected to a different group: %+v", rows)
	}
}
