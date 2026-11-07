package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const missionRegistryKey = "mission_series"

func (a *API) accountMissions(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "userID"))
	if err != nil || id < accountstore.PrimaryUserID {
		WriteAdminError(w, 400, "账号编号无效")
		return
	}
	state, err := a.accounts.LoadState(id)
	if err != nil {
		WriteAdminError(w, 404, "账号不存在或存档不可读")
		return
	}
	WriteAdminJSON(w, 200, map[string]any{"user_id": id, "name": state.User.Name, "missions": a.operations.MissionCatalog().SavedProgress(state.Engagement.Missions, time.Now())})
}

type missionSeriesRecord struct {
	Draft     gamestate.MissionSeries  `json:"draft"`
	Published *gamestate.MissionSeries `json:"published,omitempty"`
}
type missionRegistry struct {
	NextID int                   `json:"next_id"`
	Series []missionSeriesRecord `json:"series"`
}

func (o *Operations) loadMissions(base map[int]gamestate.MissionDefinition) error {
	o.missionBase = maps.Clone(base)
	doc, err := o.storage.ReadDocument(missionRegistryKey)
	if err != nil {
		return err
	}
	registry := missionRegistry{NextID: 10000000, Series: []missionSeriesRecord{}}
	for id := range base {
		registry.NextID = max(registry.NextID, id+1)
	}
	if doc.Revision > 0 {
		if err = json.Unmarshal(doc.Payload, &registry); err != nil {
			return err
		}
	}
	catalog, err := o.compileMissions(registry)
	if err != nil {
		return err
	}
	o.missionConfig = game.MissionConfiguration{Revision: uint64(doc.Revision + 1), Catalog: catalog}
	return nil
}

func (o *Operations) compileMissions(registry missionRegistry) (*game.MissionCatalog, error) {
	definitions := maps.Clone(o.missionBase)
	if definitions == nil {
		definitions = map[int]gamestate.MissionDefinition{}
	}
	for _, record := range registry.Series {
		if record.Published == nil {
			continue
		}
		series := record.Published
		for _, task := range series.Tasks {
			if _, exists := definitions[task.MissionID]; exists {
				return nil, errors.New("任务编号重复")
			}
			task.Published = series.Enabled && !series.Archived
			definitions[task.MissionID] = task
		}
	}
	return game.NewMissionCatalog(definitions)
}

func (a *API) missionSeriesList(w http.ResponseWriter, r *http.Request) {
	doc, err := a.operations.storage.ReadDocument(missionRegistryKey)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	registry := missionRegistry{Series: []missionSeriesRecord{}}
	if doc.Revision > 0 {
		if err = json.Unmarshal(doc.Payload, &registry); err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
	}
	WriteAdminJSON(w, 200, map[string]any{"revision": doc.Revision, "series": registry.Series})
}

// A published series has one permanent economic meaning. Text and visibility
// remain editable; a different rule/reward/window requires new task IDs.
func missionMeaning(series gamestate.MissionSeries) gamestate.MissionSeries {
	series.Name = ""
	series.Enabled = false
	series.Archived = false
	series.Tasks = append([]gamestate.MissionDefinition(nil), series.Tasks...)
	for i := range series.Tasks {
		d := &series.Tasks[i]
		d.Title = ""
		d.Description = ""
		d.OpenURL = ""
		d.ViewPriority = 0
		d.Published = false
		d.ProgressShow = 0
	}
	return series
}

func (a *API) normalizeMissionSeries(series *gamestate.MissionSeries, requireReady bool) error {
	series.Name = strings.TrimSpace(series.Name)
	if series.Name == "" || len([]rune(series.Name)) > 80 || len(series.Tasks) < 1 || len(series.Tasks) > 100 {
		return errors.New("系列名称须为1–80字，任务数须为1–100项")
	}
	if series.Kind != "mission" && series.Kind != "achievement" || series.Layout != "independent" && series.Layout != "chain" {
		return errors.New("请选择任务/成就和独立/任务链")
	}
	if series.TabType < 0 || series.TabType > 3 || series.TabType == 2 {
		return errors.New("任务页签无效")
	}
	if series.Kind == "achievement" && series.Daily {
		return errors.New("成就使用累计状态，不能按日重置")
	}
	var targets missionTargetIndex
	for i := range series.Tasks {
		d := &series.Tasks[i]
		d.SeriesID = series.SeriesID
		d.TabType = series.TabType
		d.Daily = series.Daily
		d.StartsAt = series.StartsAt
		d.EndsAt = series.EndsAt
		d.Published = series.Enabled && !series.Archived
		d.PrerequisiteID = 0
		if i > 0 && series.Layout == "chain" {
			d.PrerequisiteID = series.Tasks[i-1].MissionID
		}
		d.Title = strings.TrimSpace(d.Title)
		if len([]rune(d.Title)) > 80 || len([]rune(d.Description)) > 300 || d.Rule == nil || requireReady && len(d.Rewards) < 1 || len(d.Rewards) > 10 {
			return fmt.Errorf("第%d项须有规则、1–10份奖励及有效标题/描述", i+1)
		}
		if series.Kind == "achievement" && d.Rule.Metric == "" {
			return errors.New("成就须使用当前状态指标")
		}
		d.ProgressShow = 1
		d.ProgressMax = d.Rule.Target
		if !requireReady {
			continue
		}
		if err := a.validateMissionTargets(*d.Rule, &targets); err != nil {
			return fmt.Errorf("第%d项：%w", i+1, err)
		}
		if d.OpenURL != "" && !game.ValidMissionCommand(d.OpenURL) {
			return errors.New("任务跳转不属于客户端本地命令")
		}
		for j, r := range d.Rewards {
			normalized, _, err := a.mailReward(AdminMailRequest{RewardType: r.Type, RewardTypeID: r.RewardTypeID, Quantity: r.Num, CardLevel: int(r.CardLevel), CardFame: int(r.CardFame), CardLove: r.CardLove})
			if err != nil {
				return fmt.Errorf("第%d项奖励：%w", i+1, err)
			}
			d.Rewards[j] = normalized
		}
	}
	if !requireReady {
		return nil
	}
	defs := map[int]gamestate.MissionDefinition{}
	for _, d := range series.Tasks {
		if _, ok := defs[d.MissionID]; ok {
			return errors.New("任务编号重复")
		}
		defs[d.MissionID] = d
	}
	_, err := game.NewMissionCatalog(defs)
	return err
}

// missionTabRuleLocked matches the client's three tabs: 普通 holds long-lived achievements over 主线 stages,
// 期间限定 needs a daily reset or an end, 活动 needs an end, and both timed tabs count actions on non-main BOSSes
// when a BOSS filter is specified. Story conditions are 普通 achievements over one story. Drafts may be saved
// before these hold. The caller holds
// operations.configMu.
func (a *API) missionTabRuleLocked(series gamestate.MissionSeries) error {
	for i, d := range series.Tasks {
		if d.Rule == nil {
			continue
		}
		// 主线 stages open one after another, so a set of them says no more than its last stage.
		if series.TabType == 0 && d.Rule.Metric == gamestate.MissionQuestCleared && d.Rule.Kind == gamestate.MissionTargetSet {
			return fmt.Errorf("第%d项：普通页签只能指定一个主线 BOSS", i+1)
		}
		if (series.TabType == 1 || series.TabType == 3) && d.Rule.Metric != "" {
			return fmt.Errorf("第%d项：期间限定和活动页签只能使用动作计数", i+1)
		}
		// Most stories open one after another, so one story says what a set of them would.
		if d.Rule.Metric == gamestate.MissionStoryCleared && d.Rule.Kind == gamestate.MissionTargetSet {
			return fmt.Errorf("第%d项：剧情条件只能指定一段剧情", i+1)
		}
		ids := append([]int(nil), d.Rule.BossIDs...)
		if d.Rule.Metric == gamestate.MissionQuestCleared {
			ids = append(ids, d.Rule.Targets...)
			if d.Rule.SubjectID != 0 {
				ids = append(ids, d.Rule.SubjectID)
			}
		}
		for _, id := range ids {
			if main := a.missionMainQuestBossLocked(id); series.TabType == 0 && !main {
				return fmt.Errorf("第%d项：普通页签只能选择主线 BOSS（%d）", i+1, id)
			} else if series.TabType != 0 && main {
				return fmt.Errorf("第%d项：主线 BOSS（%d）属于普通页签的成就", i+1, id)
			}
		}
	}
	switch series.TabType {
	case 0:
		if series.Kind != "achievement" {
			return errors.New("普通页签只能使用成就")
		}
		if series.StartsAt != 0 || series.EndsAt != 0 {
			return errors.New("普通成就长期有效，不设开放时间")
		}
	case 1:
		if series.Kind != "mission" {
			return errors.New("期间限定页签使用任务，不能使用成就")
		}
		if !series.Daily && series.EndsAt == 0 {
			return errors.New("期间限定须开启每日重置或填写结束时间")
		}
	case 3:
		if series.Kind != "mission" {
			return errors.New("活动页签使用任务，不能使用成就")
		}
		if series.EndsAt == 0 {
			return errors.New("活动须填写结束时间")
		}
	}
	return nil
}

// A preview or publication validates all tasks against the same per-request ID sets.
// Build each set only when a task uses it.
type missionTargetIndex struct {
	bosses  map[int]bool
	stories map[int]bool
}

func (a *API) validateMissionTargets(rule gamestate.MissionRule, targets *missionTargetIndex) error {
	if rule.Metric == gamestate.MissionPVPRank {
		// The same ranks feed the 段位 select; rank 0 is the unranked start and no goal.
		for _, rank := range a.pvpRanks {
			if rank.RankID > 0 && rank.RankID == rule.Target {
				return nil
			}
		}
		return fmt.Errorf("竞技场段位 %d 不在当前段位目录", rule.Target)
	}
	ids := append([]int(nil), rule.Targets...)
	if rule.SubjectID != 0 {
		ids = append(ids, rule.SubjectID)
	}
	if rule.Metric == gamestate.MissionCardOwned || rule.Metric == gamestate.MissionCardFame || rule.Event == gamestate.MissionCardEvolution {
		for _, id := range ids {
			_, card := a.catalogByKey[adminCatalogKey(6, id)]
			_, material := a.catalogByKey[adminCatalogKey(13, id)]
			if !card && !(rule.Metric == gamestate.MissionCardOwned && material) {
				return fmt.Errorf("卡牌目标 %d 不在当前目录", id)
			}
		}
		return nil
	}
	if rule.Event == gamestate.MissionQuestClear {
		ids = rule.BossIDs
	}
	if len(ids) == 0 {
		return nil
	}
	// The same lists feed the Admin pickers (/api/mission-targets), so every pickable ID validates.
	story := rule.Metric == gamestate.MissionStoryCleared
	known := targets.bosses
	if story {
		known = targets.stories
	}
	if known == nil {
		catalog, err := a.operations.storage.CatalogState()
		if err != nil {
			return err
		}
		known = map[int]bool{}
		if story {
			for _, target := range missionStoryTargets(catalog) {
				known[target.StoryID] = true
			}
			targets.stories = known
		} else {
			for _, target := range a.missionBossTargetsLocked(catalog) {
				known[target.BossID] = true
			}
			targets.bosses = known
		}
	}
	for _, id := range ids {
		if !known[id] {
			return fmt.Errorf("任务目标 %d 不在当前剧情／关卡目录", id)
		}
	}
	return nil
}

func (a *API) missionSeriesAction(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var req struct {
		ExpectedRevision *int                    `json:"expected_revision"`
		Action           string                  `json:"action"`
		Series           gamestate.MissionSeries `json:"series"`
	}
	if err := decodeAdminJSON(r, &req); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if req.ExpectedRevision == nil {
		WriteAdminError(w, 400, "缺少配置版本，请重新载入")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	doc, err := o.storage.ReadDocument(missionRegistryKey)
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	if doc.Revision != *req.ExpectedRevision {
		WriteAdminError(w, 409, accountstore.ErrDocumentConflict.Error())
		return
	}
	registry := missionRegistry{NextID: 10000000, Series: []missionSeriesRecord{}}
	for id := range o.missionBase {
		registry.NextID = max(registry.NextID, id+1)
	}
	if doc.Revision > 0 {
		if err = json.Unmarshal(doc.Payload, &registry); err != nil {
			WriteAdminError(w, 500, err.Error())
			return
		}
	}
	index := -1
	for i, v := range registry.Series {
		if v.Draft.SeriesID == req.Series.SeriesID {
			index = i
			break
		}
	}
	if req.Action == "clone" {
		if index < 0 {
			WriteAdminError(w, 404, "系列不存在")
			return
		}
		req.Series = registry.Series[index].Draft
		req.Series.Tasks = append([]gamestate.MissionDefinition(nil), req.Series.Tasks...)
		name := []rune(req.Series.Name)
		if len(name) > 70 {
			name = name[:70]
		}
		req.Series.Name = string(name) + "（副本）"
		req.Series.SeriesID = 0
		req.Series.Enabled = false
		req.Series.Archived = false
		for i := range req.Series.Tasks {
			req.Series.Tasks[i].MissionID = 0
		}
		index = -1
	}
	if req.Series.SeriesID != 0 && index < 0 {
		WriteAdminError(w, 404, "系列不存在")
		return
	}
	if index < 0 {
		req.Series.SeriesID = registry.NextID
		registry.NextID++
	}
	oldIDs := map[int]bool{}
	if index >= 0 {
		for _, d := range registry.Series[index].Draft.Tasks {
			oldIDs[d.MissionID] = true
		}
	}
	for i := range req.Series.Tasks {
		d := &req.Series.Tasks[i]
		if d.MissionID == 0 {
			d.MissionID = registry.NextID
			registry.NextID++
		} else if !oldIDs[d.MissionID] {
			WriteAdminError(w, 400, "任务编号由后台分配，不得引用其他系列")
			return
		}
	}
	requireReady := req.Action == "publish" || req.Action == "preview"
	if index >= 0 && registry.Series[index].Published != nil && (!req.Series.Enabled || req.Series.Archived) {
		requireReady = false
	}
	if err = a.normalizeMissionSeries(&req.Series, requireReady); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if requireReady {
		if err = a.missionTabRuleLocked(req.Series); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
	}
	record := missionSeriesRecord{Draft: req.Series}
	if index >= 0 {
		record.Published = registry.Series[index].Published
	}
	if record.Published != nil && !reflect.DeepEqual(missionMeaning(*record.Published), missionMeaning(req.Series)) {
		WriteAdminError(w, 400, "已发布系列不能修改目标、奖励、顺序或周期；请复制为新系列")
		return
	}
	switch req.Action {
	case "save", "clone", "preview":
	case "publish":
		published := req.Series
		record.Published = &published
	case "archive":
		record.Draft.Archived = true
		record.Draft.Enabled = false
		if record.Published != nil {
			p := *record.Published
			p.Archived = true
			p.Enabled = false
			record.Published = &p
		}
	default:
		WriteAdminError(w, 400, "未知系列操作")
		return
	}
	if index < 0 {
		registry.Series = append(registry.Series, record)
	} else {
		registry.Series[index] = record
	}
	if req.Action == "preview" {
		WriteAdminJSON(w, 200, map[string]any{"series": req.Series, "revision": doc.Revision})
		return
	}
	var catalog *game.MissionCatalog
	if req.Action == "publish" || req.Action == "archive" && record.Published != nil {
		catalog, err = o.compileMissions(registry)
		if err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
	}
	saved, err := o.storage.WriteDocument(missionRegistryKey, doc.Revision, registry, "mission-series-"+req.Action)
	if err != nil {
		WriteAdminError(w, 409, err.Error())
		return
	}
	if catalog != nil {
		o.missionConfig = game.MissionConfiguration{Revision: uint64(saved.Revision + 1), Catalog: catalog}
		o.publishRuntimeLocked()
	}
	WriteAdminJSON(w, 200, map[string]any{"revision": saved.Revision, "series": registry.Series, "series_id": req.Series.SeriesID})
}
