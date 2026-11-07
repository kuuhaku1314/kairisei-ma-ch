package admin

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestPlayerPolicySaveRestartAndNewAccounts(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	days := []gamestate.LoginBonusDay{{Day: 1, Comment: "签到", Reward: gamestate.Reward{Type: 10, Num: 5, CardSkillLevels: []int16{}}}}
	o.playerLoginBase = gamestate.LoginBonusPolicy{ConfigVersion: 1, Cycle: days, Beginner: days, TotalMilestones: days}
	o.playerDefaults = &PlayerPolicy{Notice: noticePolicy{Title: "公告", Body: "欢迎", Enabled: true}, Login: loginRewards{days, days, days}, StoryCrystals: 50, Navigators: []game.NaviSetting{{NaviID: 1, Enabled: true, Price: 500}}}
	o.playerNaviNames = map[int8]string{1: "妮妙"}
	if err := o.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: o, catalogByKey: map[string]AdminCatalogEntry{
		adminCatalogKey(10, 0):       {RewardType: 10},
		adminCatalogKey(6, 10000010): {RewardType: 6, RewardTypeID: 10000010, LevelMax: 60, FameMax: 90, LoveMax: 100},
	}}
	r := chi.NewRouter()
	r.Put("/policy", a.savePlayerPolicy)
	var config PlayerPolicy
	data, _ := json.Marshal(o.playerDefaults)
	_ = json.Unmarshal(data, &config)
	config.TutorialMail = game.TutorialCompletionMail{Enabled: true, Title: "毕业礼物", Message: "全部训练完成奖励", Rewards: []gamestate.Reward{
		{Type: 10, Num: 456, CardSkillLevels: []int16{}},
		{Type: 6, RewardTypeID: 10000010, Num: 2, CardLevel: 60, CardFame: 90, CardLove: 100, CardSkillLevels: []int16{1}},
	}}
	config.Notice.Body = "<script>alert(1)</script>\n活动公告"
	config.Login.Cycle[0].Reward.Num = 31
	config.StoryCrystals = 75
	config.Navigators[0].Price = 222
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": config}, 200)
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 0, "config": config}, 409)
	bad := config
	bad.StoryCrystals = -1
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	bad = config
	bad.TutorialMail.Rewards = []gamestate.Reward{{Type: 6, RewardTypeID: 99999999, Num: 1}}
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	bad.TutorialMail.Rewards = append(config.TutorialMail.Rewards, config.TutorialMail.Rewards[0])
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	bad = config
	bad.Notice.StartUnix, bad.Notice.EndUnix = 2000, 1000
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": bad}, 400)
	if now := time.Now().Unix(); (noticePolicy{Enabled: true, StartUnix: now + 60}).visible(now) || !(noticePolicy{Enabled: true, EndUnix: now + 60}).visible(now) || (noticePolicy{Enabled: true, EndUnix: now}).visible(now) {
		t.Fatal("notice display window not applied")
	}
	w := httptest.NewRecorder()
	o.LocalNotice(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), "<script>") || !strings.Contains(w.Body.String(), "活动公告") {
		t.Fatal("notice text not safely rendered")
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.playerDefaults = o.playerDefaults
	restarted.playerNaviNames = o.playerNaviNames
	restarted.playerLoginBase = o.playerLoginBase
	if err := restarted.loadPlayerPolicy(); err != nil {
		t.Fatal(err)
	}
	p := restarted.playerPolicy.Load()
	if p.Revision != 1 || !reflect.DeepEqual(p.Runtime.TutorialMail, config.TutorialMail) || p.Runtime.LoginBonus.Cycle[0].Reward.Num != 31 || p.Runtime.Navigators[0].Price != 222 {
		t.Fatal("public policy was not restored")
	}
	for i, uuid := range []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"} {
		identity, err := accounts.ResolveInternalLogin(uuid)
		if err != nil {
			t.Fatal(err)
		}
		if identity.UserID != accountstore.PrimaryUserID+i {
			t.Fatalf("unexpected identity %d", identity.UserID)
		}
		state, err := accounts.LoadState(identity.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if state.User.Gold != 0 || state.User.CoinFree != 0 || state.User.FriendPoint != 0 || state.Onboarding.Step != 0 || len(state.Engagement.Presents) != 0 {
			t.Fatalf("new user balance/flow: %+v", state.User)
		}
		state.User.Gold = 9
		if err := accounts.PersistState(identity.UserID, state); err != nil {
			t.Fatal(err)
		}
		if _, err := accounts.ResolveInternalLogin(uuid); err != nil {
			t.Fatal(err)
		}
		state, err = accounts.LoadState(identity.UserID)
		if err != nil || state.User.Gold != 9 {
			t.Fatal("existing login was reset")
		}
	}
	config.Notice.Enabled = false
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 1, "config": config}, 200)
	w = httptest.NewRecorder()
	o.LocalNotice(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), "活动公告") || !strings.Contains(w.Body.String(), "暂无公告") {
		t.Fatal("disabled notice remains visible")
	}
	// A resource upgrade expands the catalog without replacing the operator's
	// database. The old policy stays authoritative for every existing ID.
	config.Navigators[0].Enabled = false
	testfixture.CallContentAdmin(t, r, "PUT", "/policy", map[string]any{"expected_revision": 2, "config": config}, 200)
	before, err := o.storage.ReadDocument(playerPolicyKey)
	if err != nil {
		t.Fatal(err)
	}
	upgradedDefaults := *o.playerDefaults
	newNavigator := game.NaviSetting{NaviID: 49, Enabled: true, Price: 500}
	upgradedDefaults.Navigators = append(append([]game.NaviSetting(nil), o.playerDefaults.Navigators...), newNavigator)
	restarted.playerDefaults = &upgradedDefaults
	restarted.playerNaviNames = map[int8]string{1: "妮妙", 49: "新看板"}
	if err := restarted.loadPlayerPolicy(); err != nil {
		t.Fatal("existing database blocked catalog expansion", err)
	}
	want := config
	want.Navigators = append(append([]game.NaviSetting(nil), config.Navigators...), newNavigator)
	if got := restarted.playerPolicy.Load(); got.Revision != 3 || !reflect.DeepEqual(got.Value, want) || !reflect.DeepEqual(got.Runtime.Navigators, want.Navigators) {
		t.Fatalf("catalog expansion reset saved settings or omitted new navigator: %+v", got)
	}
	after, err := o.storage.ReadDocument(playerPolicyKey)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("loading catalog expansion rewrote the stored policy")
	}
	// Admin receives the complete current list and persists it through the
	// ordinary revision-checked save, not a special database migration.
	upgradedAdmin := &API{operations: restarted, catalogByKey: a.catalogByKey}
	upgradedRouter := chi.NewRouter()
	upgradedRouter.Put("/policy", upgradedAdmin.savePlayerPolicy)
	testfixture.CallContentAdmin(t, upgradedRouter, "PUT", "/policy", map[string]any{"expected_revision": 3, "config": config}, 400)
	testfixture.CallContentAdmin(t, upgradedRouter, "PUT", "/policy", map[string]any{"expected_revision": 3, "config": want}, 200)
	if err := restarted.loadPlayerPolicy(); err != nil || !reflect.DeepEqual(restarted.playerPolicy.Load().Value, want) || restarted.playerPolicy.Load().Revision != 4 {
		t.Fatal("completed navigator policy failed to survive another load", err)
	}
}
