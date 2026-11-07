package game

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// MissionCatalog is immutable publication data. Account progress and response
// objects never own or modify its definitions, indexes or prerequisite graph.
type MissionCatalog struct {
	definitions map[int]gamestate.MissionDefinition
	byEvent     map[gamestate.MissionEventKind][]int
	byMetric    map[gamestate.MissionMetric][]int
	successors  map[int][]int
	order       []int
}

func NewMissionCatalog(definitions map[int]gamestate.MissionDefinition) (*MissionCatalog, error) {
	c := &MissionCatalog{definitions: make(map[int]gamestate.MissionDefinition, len(definitions)), byEvent: map[gamestate.MissionEventKind][]int{}, byMetric: map[gamestate.MissionMetric][]int{}, successors: map[int][]int{}}
	for id, d := range definitions {
		if id != d.MissionID || id <= 0 || id > math.MaxInt32 || d.Title == "" || d.ProgressMax < 0 || d.PrerequisiteID < 0 || d.TabType < 0 || d.TabType > 3 || d.StartsAt < 0 || d.EndsAt < 0 || d.EndsAt > math.MaxInt32 || d.EndsAt != 0 && d.EndsAt <= d.StartsAt {
			return nil, fmt.Errorf("任务 %d 的身份、显示或排期无效", id)
		}
		d.Rewards = cloneRewards(d.Rewards)
		if d.Rule != nil {
			r := *d.Rule
			r.Targets = slices.Clone(r.Targets)
			r.BossIDs = slices.Clone(r.BossIDs)
			if err := validateMissionRule(r); err != nil {
				return nil, fmt.Errorf("任务 %d：%w", id, err)
			}
			d.Rule = &r
			if r.Event != "" {
				c.byEvent[r.Event] = append(c.byEvent[r.Event], id)
			} else {
				c.byMetric[r.Metric] = append(c.byMetric[r.Metric], id)
			}
		}
		c.definitions[id] = d
		c.order = append(c.order, id)
		if d.PrerequisiteID != 0 {
			c.successors[d.PrerequisiteID] = append(c.successors[d.PrerequisiteID], id)
		}
	}
	for id, d := range c.definitions {
		seen := map[int]bool{id: true}
		for parent := d.PrerequisiteID; parent != 0; {
			next, ok := c.definitions[parent]
			if !ok || seen[parent] {
				return nil, errors.New("任务前置不存在或形成循环")
			}
			if d.Daily != next.Daily || d.SeriesID != next.SeriesID || d.StartsAt != next.StartsAt || d.EndsAt != next.EndsAt {
				return nil, errors.New("前置任务必须属于同一系列和周期")
			}
			seen[parent] = true
			parent = next.PrerequisiteID
		}
	}
	sort.Slice(c.order, func(i, j int) bool {
		a, b := c.definitions[c.order[i]], c.definitions[c.order[j]]
		if a.ViewPriority != b.ViewPriority {
			return a.ViewPriority < b.ViewPriority
		}
		return a.MissionID < b.MissionID
	})
	// A command evaluates candidates independently of map iteration order.
	for _, ids := range c.byEvent {
		slices.Sort(ids)
	}
	return c, nil
}

func validateMissionRule(r gamestate.MissionRule) error {
	if r.Target <= 0 || r.Target > math.MaxInt32 || r.Minimum < 0 || r.SubjectID < 0 || r.ArthurType < 0 || r.ArthurType > 4 || r.MinPlayers < 0 || r.MinPlayers > 4 || r.ScoreGrade < 0 || r.ScoreGrade > 8 {
		return errors.New("任务目标或筛选范围无效")
	}
	if r.Kind != gamestate.MissionCount && r.Kind != gamestate.MissionStateValue && r.Kind != gamestate.MissionTargetSet {
		return errors.New("不支持的任务计算方式")
	}
	if (r.Event == "") == (r.Metric == "") {
		return errors.New("任务必须明确选择一种业务事件或状态指标")
	}
	if r.Event != "" {
		switch r.Event {
		case gamestate.MissionCardFusion, gamestate.MissionExploreClear, gamestate.MissionQuestClear, gamestate.MissionCardEvolution, gamestate.MissionPVPPlayed:
		default:
			return errors.New("事件尚未接入")
		}
		if r.SubjectID != 0 && r.Event != gamestate.MissionCardEvolution {
			return errors.New("只有指定卡牌进化事件接受目标ID")
		}
		if r.Kind == gamestate.MissionStateValue {
			return errors.New("状态任务不能使用事件")
		}
	} else {
		switch r.Metric {
		case gamestate.MissionPlayerLevel, gamestate.MissionCollectionCount, gamestate.MissionLoginDays, gamestate.MissionLoginStreak, gamestate.MissionPVPRank:
			if r.SubjectID != 0 {
				return errors.New("该状态指标不接受目标ID")
			}
		case gamestate.MissionCardFame, gamestate.MissionCardOwned, gamestate.MissionQuestCleared, gamestate.MissionStoryCleared:
			if r.Kind != gamestate.MissionTargetSet && r.SubjectID <= 0 {
				return errors.New("请选择明确的目标ID")
			}
		default:
			return errors.New("状态指标尚未接入")
		}
		if r.Kind == gamestate.MissionCount {
			return errors.New("状态条件请选择状态计算方式")
		}
	}
	if r.Metric == gamestate.MissionCardOwned || r.Metric == gamestate.MissionQuestCleared || r.Metric == gamestate.MissionStoryCleared {
		if r.Kind != gamestate.MissionTargetSet && r.Target != 1 || r.Minimum > 1 {
			return errors.New("持有／通关状态只能要求完成一次")
		}
	}
	if r.Event == gamestate.MissionCardEvolution && r.Kind == gamestate.MissionTargetSet && r.Minimum > 1 {
		return errors.New("进化集合按每个结果卡牌至少完成一次计算")
	}
	if r.Kind == gamestate.MissionTargetSet {
		if len(r.Targets) == 0 || len(r.Targets) > 100 || r.Target != len(r.Targets) || r.SubjectID != 0 {
			return errors.New("目标集合须有1–100个不同目标，达成数等于目标数量")
		}
		if r.Event != "" && r.Event != gamestate.MissionCardEvolution {
			return errors.New("集合事件目前只支持指定卡牌进化")
		}
		if r.Metric != "" && r.Metric != gamestate.MissionCardOwned && r.Metric != gamestate.MissionCardFame && r.Metric != gamestate.MissionQuestCleared && r.Metric != gamestate.MissionStoryCleared {
			return errors.New("该指标不支持目标集合")
		}
	} else if len(r.Targets) != 0 || r.Minimum != 0 {
		return errors.New("普通规则不能携带集合目标或逐项目标值")
	}
	for _, ids := range [][]int{r.Targets, r.BossIDs} {
		seen := map[int]bool{}
		for _, id := range ids {
			if id <= 0 || seen[id] {
				return errors.New("目标ID无效或重复")
			}
			seen[id] = true
		}
	}
	if len(r.BossIDs) > 100 {
		return errors.New("单个规则最多100个关卡")
	}
	if r.QuestScope != "" || len(r.BossIDs) != 0 || r.ArthurType != 0 || r.OwnDecksOnly || r.MinPlayers != 0 || r.ScoreGrade != 0 || r.IncludeSkip {
		if r.Event != gamestate.MissionQuestClear {
			return errors.New("战斗筛选只能用于通关事件")
		}
	}
	if r.QuestScope == gamestate.MissionQuestSolo && r.MinPlayers > 1 || r.OwnDecksOnly && (r.MinPlayers > 1 || r.QuestScope == gamestate.MissionQuestMultiplayer || r.QuestScope == gamestate.MissionQuestHost) {
		return errors.New("单人／自卡组不能要求多人参与")
	}
	switch r.QuestScope {
	case gamestate.MissionQuestAny, gamestate.MissionQuestSolo, gamestate.MissionQuestMultiplayer, gamestate.MissionQuestHost, gamestate.MissionQuestAlchemist:
	default:
		return errors.New("不支持的关卡范围")
	}
	return nil
}

type MissionConfiguration struct {
	Revision uint64
	Catalog  *MissionCatalog
}

// SavedMissionInfo keeps historical progress visible to operators while
// distinguishing tasks the player can currently see from locked or closed ones.
type SavedMissionInfo struct {
	gamestate.MissionInfo
	Availability string `json:"availability"` // active, locked, inactive, scheduled or expired
}

// SavedProgress is an operator view of committed progress, projected into the
// current daily period. It neither grants progress nor loads a live account.
func (c *MissionCatalog) SavedProgress(saved []gamestate.Mission, now time.Time) []SavedMissionInfo {
	progress := make(map[int]gamestate.MissionProgress, len(saved))
	for _, row := range saved {
		progress[row.Info.MissionID] = row.Info
	}
	result := []SavedMissionInfo{}
	current := now.Unix()
	for _, id := range c.order {
		definition := c.definitions[id]
		if definition.SeriesID == 0 && !definition.Published {
			continue
		}
		period := missionProgressAt(definition, progress[id], current)
		availability := "active"
		switch {
		case !definition.Published:
			availability = "inactive"
		case definition.StartsAt > current:
			availability = "scheduled"
		case definition.EndsAt != 0 && current >= definition.EndsAt || period.ClearLimitTime != 0 && int64(period.ClearLimitTime) < current:
			availability = "expired"
		case definition.PrerequisiteID != 0 && missionProgressAt(c.definitions[definition.PrerequisiteID], progress[definition.PrerequisiteID], current).State != 2:
			availability = "locked"
		}
		result = append(result, SavedMissionInfo{MissionInfo: projectMission(definition, period), Availability: availability})
	}
	return result
}

type MissionConfigurator interface{ ApplyMissionConfiguration(MissionConfiguration) }

func WithMissionCatalog(c *MissionCatalog) AccountOption {
	return func(o *accountOptions) { o.missions = c }
}

func (s *Account) ApplyMissionConfiguration(config MissionConfiguration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if config.Catalog == nil || config.Revision == s.missionRevision {
		return
	}
	s.missionCatalog = config.Catalog
	s.missionRevision = config.Revision
	s.markAllMissionMetricsLocked()
}
