package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"slices"
	"strings"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/operations"
)

const playerPolicyKey = "player-policy"

type noticePolicy struct {
	Enabled bool   `json:"enabled"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	// Optional display window (Unix seconds); zero means unbounded. Checked when the page is served.
	StartUnix int64 `json:"start_unix,omitempty"`
	EndUnix   int64 `json:"end_unix,omitempty"`
}

func (n noticePolicy) visible(now int64) bool {
	return n.Enabled && (n.StartUnix <= 0 || now >= n.StartUnix) && (n.EndUnix <= 0 || now < n.EndUnix)
}

type loginRewards struct {
	Cycle    []gamestate.LoginBonusDay `json:"cycle"`
	Beginner []gamestate.LoginBonusDay `json:"beginner"`
	Total    []gamestate.LoginBonusDay `json:"total_milestones"`
}

type PlayerPolicy struct {
	HomeBanners   operations.HomeBannerPolicy `json:"home_banners"`
	Notice        noticePolicy                `json:"notice"`
	TutorialMail  game.TutorialCompletionMail `json:"tutorial_completion_mail"`
	Login         loginRewards                `json:"login_rewards"`
	StoryCrystals int                         `json:"story_first_clear_crystals"`
	Navigators    []game.NaviSetting          `json:"navigators"`
}

type playerPolicySnapshot struct {
	Value    PlayerPolicy
	Revision int
	Runtime  game.PlayerConfiguration
}

func (o *Operations) InitializePlayerPolicy(base gamestate.State, naviPath string) error {
	o.playerLoginBase = base.LoginBonusPolicy
	o.playerNaviNames = map[int8]string{}
	master, err := masterdata.LoadNaviRuntimeMaster(naviPath)
	if err != nil {
		return err
	}
	defaults := PlayerPolicy{
		Notice:        noticePolicy{Enabled: true, Title: "本地服务公告", Body: "欢迎来到不列颠！祝各位亚瑟游戏愉快。"},
		TutorialMail:  game.TutorialCompletionMail{Title: "新手毕业礼物", Message: "恭喜完成全部新手训练，祝冒险愉快！", Rewards: []gamestate.Reward{}},
		Login:         loginRewards{Cycle: base.LoginBonusPolicy.Cycle, Beginner: base.LoginBonusPolicy.Beginner, Total: base.LoginBonusPolicy.TotalMilestones},
		StoryCrystals: base.StoryRewardPolicy.MainFirstClear.Num,
		Navigators:    []game.NaviSetting{},
	}
	for _, n := range master.Navigators {
		if n.NaviID == 0 || !n.ClientPublished {
			continue
		}
		id := int8(n.NaviID)
		o.playerNaviNames[id] = n.Name
		defaults.Navigators = append(defaults.Navigators, game.NaviSetting{NaviID: id, Enabled: true, Price: base.User.NaviPurchasePrice})
	}
	o.playerDefaults = &defaults
	return o.loadPlayerPolicy()
}

func (o *Operations) loadPlayerPolicy() error {
	doc, err := o.storage.ReadDocument(playerPolicyKey)
	if err != nil {
		return err
	}
	value := PlayerPolicy{TutorialMail: o.playerDefaults.TutorialMail}
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &value); err != nil {
			return err
		}
		// Retire the old dedicated event-page advertisement even when an
		// operator saved it before the shared carousel gained score targets.
		value.HomeBanners.Items = slices.DeleteFunc(value.HomeBanners.Items, func(row operations.HomeBannerEntry) bool {
			return row.Target == "eventpage"
		})
		// The pre-release per-draw targets are retired. Keep the artwork available
		// for reselection in Admin and leave those rows disabled until then.
		for i := range value.HomeBanners.Items {
			row := &value.HomeBanners.Items[i]
			if row.Target == "gacha" {
				row.Target, row.TargetID, row.Enabled = "none", 0, false
			}
		}
		// Stored policy predates newly published navigators after a resource
		// upgrade. Complete only missing IDs; saved prices and switches win.
		// Keep duplicates/unknown IDs intact so normal validation rejects them.
		seen := make(map[int8]bool, len(value.Navigators))
		for _, n := range value.Navigators {
			seen[n.NaviID] = true
		}
		for _, n := range o.playerDefaults.Navigators {
			if !seen[n.NaviID] {
				value.Navigators = append(value.Navigators, n)
			}
		}
	} else {
		value = *o.playerDefaults
	}
	if err := o.validatePlayerPolicy(value); err != nil {
		return fmt.Errorf("player policy: %w", err)
	}
	o.playerPolicy.Store(o.playerSnapshot(value, doc.Revision))
	o.publishRuntimeLocked()
	return nil
}

func (o *Operations) playerSnapshot(p PlayerPolicy, revision int) *playerPolicySnapshot {
	login := o.playerLoginBase
	login.Cycle, login.Beginner, login.TotalMilestones = p.Login.Cycle, p.Login.Beginner, p.Login.Total
	return &playerPolicySnapshot{Value: p, Revision: revision, Runtime: game.PlayerConfiguration{
		Revision: uint64(revision) + 1, LoginBonus: login, StoryCrystals: p.StoryCrystals, Navigators: p.Navigators,
		TutorialMail: p.TutorialMail,
	}}
}

func validPolicyAmount(amount int, allowZero bool) bool {
	return amount <= 10000000 && (amount > 0 || allowZero && amount == 0)
}

func (o *Operations) validatePlayerPolicy(p PlayerPolicy) error {
	if err := validateHomeBanners(p.HomeBanners); err != nil {
		return err
	}
	if o.playerDefaults == nil {
		return errors.New("运营目录尚未载入")
	}
	if strings.TrimSpace(p.Notice.Title) == "" || len([]rune(p.Notice.Title)) > 80 || len([]rune(p.Notice.Body)) > 8000 {
		return errors.New("公告标题须为1至80字，正文最多8000字")
	}
	if p.Notice.Enabled && strings.TrimSpace(p.Notice.Body) == "" {
		return errors.New("显示公告时正文不能为空")
	}
	if p.Notice.StartUnix < 0 || p.Notice.EndUnix < 0 || p.Notice.StartUnix > 0 && p.Notice.EndUnix > 0 && p.Notice.StartUnix >= p.Notice.EndUnix {
		return errors.New("公告开始时间须早于结束时间")
	}
	mail := p.TutorialMail
	if len([]rune(mail.Title)) > 40 || len([]rune(mail.Message)) > 200 || len(mail.Rewards) > 120 {
		return errors.New("新手毕业邮件标题最多40字、正文最多200字、奖励最多120种")
	}
	if mail.Enabled && (strings.TrimSpace(mail.Title) == "" || strings.TrimSpace(mail.Message) == "" || len(mail.Rewards) == 0) {
		return errors.New("启用新手毕业邮件时，请填写标题、正文并选择奖励")
	}
	if !validPolicyAmount(p.StoryCrystals, false) {
		return errors.New("剧情首通水晶须为1至10000000")
	}
	for _, pair := range []struct{ days, base []gamestate.LoginBonusDay }{
		{p.Login.Cycle, o.playerDefaults.Login.Cycle}, {p.Login.Beginner, o.playerDefaults.Login.Beginner}, {p.Login.Total, o.playerDefaults.Login.Total},
	} {
		if len(pair.days) != len(pair.base) {
			return errors.New("请保留现有签到天数，只修改奖励和说明")
		}
		for i, d := range pair.days {
			r := d.Reward
			if d.Day != pair.base[i].Day || strings.TrimSpace(d.Comment) == "" || len([]rune(d.Comment)) > 100 || (r.Type != 4 && r.Type != 10) || !validPolicyAmount(r.Num, false) || r.RewardTypeID != 0 || r.CardLevel != 0 || r.CardFame != 0 || r.CardLove != 0 || r.CardSkillLevels == nil || len(r.CardSkillLevels) != 0 {
				return errors.New("签到奖励须为金币或水晶，数量1至10000000；天数及奖励格式须正确")
			}
		}
	}
	if len(p.Navigators) != len(o.playerDefaults.Navigators) {
		return errors.New("请保留全部可配置看板")
	}
	seen := map[int8]bool{}
	for _, n := range p.Navigators {
		if _, ok := o.playerNaviNames[n.NaviID]; !ok || seen[n.NaviID] || !validPolicyAmount(n.Price, false) {
			return errors.New("看板ID重复、资源未开放或价格无效（1至10000000）")
		}
		seen[n.NaviID] = true
	}
	return nil
}

func (a *API) validateTutorialMail(mail game.TutorialCompletionMail) error {
	seen := map[string]bool{}
	for _, r := range mail.Rewards {
		canonical, _, err := a.mailReward(AdminMailRequest{
			RewardType: r.Type, RewardTypeID: r.RewardTypeID, Quantity: r.Num,
			CardLevel: int(r.CardLevel), CardFame: int(r.CardFame), CardLove: r.CardLove,
		})
		if err != nil {
			return fmt.Errorf("新手毕业奖励 %d:%d: %w", r.Type, r.RewardTypeID, err)
		}
		key := adminCatalogKey(r.Type, r.RewardTypeID)
		if seen[key] || canonical.CardLevel != r.CardLevel || canonical.CardFame != r.CardFame || !slices.Equal(canonical.CardSkillLevels, r.CardSkillLevels) {
			return errors.New("新手毕业奖励重复或卡牌属性格式不正确")
		}
		seen[key] = true
	}
	return nil
}

func (a *API) playerPolicy(w http.ResponseWriter, _ *http.Request) {
	p := a.operations.playerPolicy.Load()
	if p == nil {
		WriteAdminError(w, 503, "运营目录尚未载入")
		return
	}
	entries := make([]AdminCatalogEntry, 0, len(p.Value.TutorialMail.Rewards))
	for _, r := range p.Value.TutorialMail.Rewards {
		entries = append(entries, a.catalogByKey[adminCatalogKey(r.Type, r.RewardTypeID)])
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": p.Revision, "config": p.Value, "defaults": a.operations.playerDefaults, "navi_names": a.operations.playerNaviNames, "mail_reward_entries": entries, "home_banner_gachas": a.homeBannerGachas(), "home_banner_score_bosses": a.homeBannerScoreBosses()})
}

func (a *API) savePlayerPolicy(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int          `json:"expected_revision"`
		Config   *PlayerPolicy `json:"config"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 256*1024); err != nil || body.Expected == nil || body.Config == nil {
		WriteAdminError(w, 400, "请提交完整配置和页面版本")
		return
	}
	o := a.operations
	if err := o.validatePlayerPolicy(*body.Config); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if err := a.validateTutorialMail(body.Config.TutorialMail); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	// Stable order makes diffs and audit records easy to compare.
	slices.SortFunc(body.Config.Navigators, func(a, b game.NaviSetting) int { return int(a.NaviID) - int(b.NaviID) })
	o.configMu.Lock()
	if err := a.validateHomeBannerAssets(body.Config.HomeBanners); err != nil {
		o.configMu.Unlock()
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := o.writeDocument(playerPolicyKey, *body.Expected, *body.Config)
	if err == nil {
		o.playerPolicy.Store(o.playerSnapshot(*body.Config, doc.Revision))
		o.publishRuntimeLocked()
	}
	o.configMu.Unlock()
	if err != nil {
		writeContentError(w, err)
		return
	}
	a.playerPolicy(w, r)
}

var noticeTemplate = template.Must(template.New("notice").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title><style>body{margin:0;background:#17130d;color:#f4e7bd;font-family:sans-serif}main{max-width:760px;margin:auto;padding:28px}h1{color:#ffd66b;border-bottom:1px solid #8b6a2c;padding-bottom:14px}section{background:#282116;border:1px solid #8b6a2c;border-radius:10px;padding:18px;line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}</style></head><body><main><h1>{{.Title}}</h1><section>{{.Body}}</section></main></body></html>`))

func (o *Operations) LocalNotice(w http.ResponseWriter, r *http.Request) {
	notice := noticePolicy{Title: "公告", Body: "暂无公告。"}
	if p := o.playerPolicy.Load(); p != nil && p.Value.Notice.visible(time.Now().Unix()) {
		notice = p.Value.Notice
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = noticeTemplate.Execute(w, notice)
}
