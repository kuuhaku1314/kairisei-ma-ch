package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sort"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

const bossEconomyKey = "boss-economy"

type bossEconomyOverride struct {
	BPUse      *int `json:"bp_use,omitempty"`
	Experience *int `json:"experience,omitempty"`
}

type bossEconomyBase struct {
	Difficulty string
	BPUse      int
	Experience int
}

func (c *contentStore) loadBossEconomy(o *Operations, battleMasterPath string) error {
	c.economyBases = map[int]bossEconomyBase{}
	// Solo-show also contains ordinary main quests whose reward profiles have
	// no StageQuestAreaID. Only the battle master's activity/past directories
	// define this editor's scope, independently of runtime publication state.
	var allowed map[int]bool
	if battleMasterPath != "" {
		master, err := masterdata.LoadBattleRuntimeMaster(battleMasterPath)
		if err != nil {
			return err
		}
		allowed = map[int]bool{}
		collect := func(raw json.RawMessage, key string) error {
			var group map[string]json.RawMessage
			if err := json.Unmarshal(raw, &group); err != nil {
				return err
			}
			var bosses []struct {
				ID int `json:"0"`
			}
			if err := json.Unmarshal(group[key], &bosses); err != nil {
				return err
			}
			for _, b := range bosses {
				allowed[b.ID] = true
			}
			return nil
		}
		for _, raw := range master.Groups {
			if err := collect(raw, "10"); err != nil {
				return err
			}
		}
		for _, raw := range master.PastBossGroups {
			if err := collect(raw, "13"); err != nil {
				return err
			}
		}
	}
	experience := map[int]int{}
	for _, p := range c.base.TeamBattleRewards {
		if p.StageQuestAreaID == 0 && p.TowerID == 0 {
			for _, r := range p.ResultRewards {
				if r.Type == 0 {
					experience[p.BossID] += r.Num
				}
			}
		}
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(c.base.TeamBattleSolo, &top); err != nil && len(c.base.TeamBattleSolo) != 0 {
		return err
	}
	for _, key := range []string{"9", "10", "11", "12"} {
		if len(top[key]) == 0 {
			continue
		}
		var groups []struct {
			Bosses []struct {
				ID    int `json:"0"`
				BPUse int `json:"5"`
			} `json:"10"`
		}
		if err := json.Unmarshal(top[key], &groups); err != nil {
			return err
		}
		for _, g := range groups {
			for _, b := range g.Bosses {
				if allowed != nil && !allowed[c.rewardBossID(b.ID)] {
					continue
				}
				row, ok := c.bosses[b.ID]
				if !ok || row.Difficulty == "" || (row.Category != "boss" && row.Category != "past") {
					continue
				}
				if source, ok := c.bosses[c.rewardBossID(b.ID)]; ok && source.Category == "material" {
					continue
				}
				c.economyBases[b.ID] = bossEconomyBase{row.Difficulty, b.BPUse, experience[b.ID]}
			}
		}
	}
	doc, err := o.storage.ReadDocument(bossEconomyKey)
	if err != nil {
		return err
	}
	c.economy, c.economyRevision = map[string]bossEconomyOverride{}, doc.Revision
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &c.economy); err != nil {
			return err
		}
		if c.economy == nil {
			return errors.New("副本体力与经验配置不能为null")
		}
	}
	for name, rule := range c.economy {
		if err := c.validateBossEconomy(name, rule); err != nil {
			return err
		}
	}
	return nil
}

func (c *contentStore) validateBossEconomy(name string, rule bossEconomyOverride) error {
	found := false
	for _, base := range c.economyBases {
		if base.Difficulty == name {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("不支持的活动／往期难度：%s", name)
	}
	if rule.BPUse == nil && rule.Experience == nil {
		return errors.New("至少设置体力或经验，恢复默认请使用reset")
	}
	if rule.BPUse != nil && (*rule.BPUse < 1 || *rule.BPUse > 1000) {
		return errors.New("单人体力必须为1至1000")
	}
	if rule.Experience != nil && (*rule.Experience < 0 || *rule.Experience > 10000000) {
		return errors.New("通关经验必须为0至10000000")
	}
	return nil
}

func economyGroup(raw json.RawMessage, bossKey string, costs map[int]int) (json.RawMessage, error) {
	var group map[string]json.RawMessage
	if err := json.Unmarshal(raw, &group); err != nil {
		return nil, err
	}
	var bosses []map[string]json.RawMessage
	if err := json.Unmarshal(group[bossKey], &bosses); err != nil {
		return nil, err
	}
	for _, b := range bosses {
		var id int
		if err := json.Unmarshal(b["0"], &id); err != nil {
			return nil, err
		}
		if cost, ok := costs[id]; ok {
			b["5"], _ = json.Marshal(cost)
			b["6"], _ = json.Marshal((cost + 1) / 2)
		}
	}
	group[bossKey], _ = json.Marshal(bosses)
	return json.Marshal(group)
}

func (c *contentStore) projectBossEconomy(state *gamestate.State) error {
	if len(c.economy) == 0 {
		return nil
	}
	costs, experiences := map[int]int{}, map[int]int{}
	for id, base := range c.economyBases {
		rule := c.economy[base.Difficulty]
		if rule.BPUse != nil {
			costs[id] = *rule.BPUse
		}
		if rule.Experience != nil {
			experiences[id] = *rule.Experience
		}
	}
	if len(costs) > 0 {
		var top map[string]json.RawMessage
		if err := json.Unmarshal(state.TeamBattleSolo, &top); err != nil {
			return err
		}
		for _, key := range []string{"9", "10", "11", "12"} {
			if len(top[key]) == 0 {
				continue
			}
			var groups []json.RawMessage
			if err := json.Unmarshal(top[key], &groups); err != nil {
				return err
			}
			for i, raw := range groups {
				var err error
				groups[i], err = economyGroup(raw, "10", costs)
				if err != nil {
					return err
				}
			}
			top[key], _ = json.Marshal(groups)
		}
		state.TeamBattleSolo, _ = json.Marshal(top)
		past := make([]json.RawMessage, len(state.TeamBattlePastBossGroups))
		for i, raw := range state.TeamBattlePastBossGroups {
			var err error
			past[i], err = economyGroup(raw, "13", costs)
			if err != nil {
				return err
			}
		}
		state.TeamBattlePastBossGroups = past
	}
	for i, p := range state.TeamBattleRewards {
		experience, ok := experiences[p.BossID]
		if !ok || p.StageQuestAreaID != 0 || p.TowerID != 0 {
			continue
		}
		rewards := make([]gamestate.Reward, 0, len(p.ResultRewards)+1)
		inserted := false
		for _, reward := range p.ResultRewards {
			if reward.Type != 0 {
				rewards = append(rewards, reward)
				continue
			}
			if experience > 0 && !inserted {
				reward.Num = experience
				rewards = append(rewards, reward)
				inserted = true
			}
		}
		if experience > 0 && !inserted {
			rewards = append(rewards, gamestate.Reward{Type: 0, Num: experience, CardSkillLevels: []int16{}})
		}
		p.ResultRewards = rewards
		state.TeamBattleRewards[i] = p
	}
	return nil
}

type bossEconomyValues struct {
	BPUse      []int `json:"bp_use"`
	BPUseHalf  []int `json:"bp_use_half"`
	Experience []int `json:"experience"`
}

func (a *API) bossEconomy(w http.ResponseWriter, r *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入活动目录")
		return
	}
	type row struct {
		Difficulty string               `json:"difficulty"`
		BossCount  int                  `json:"boss_count"`
		Defaults   bossEconomyValues    `json:"defaults"`
		Current    bossEconomyValues    `json:"current"`
		Override   *bossEconomyOverride `json:"override"`
	}
	byName := map[string]*row{}
	seen := map[int]bool{}
	for id, base := range c.economyBases {
		source := c.rewardBossID(id)
		if seen[source] {
			continue
		}
		seen[source] = true
		value := byName[base.Difficulty]
		if value == nil {
			value = &row{Difficulty: base.Difficulty}
			byName[base.Difficulty] = value
		}
		value.BossCount++
		value.Defaults.BPUse = append(value.Defaults.BPUse, base.BPUse)
		value.Defaults.Experience = append(value.Defaults.Experience, base.Experience)
	}
	unique := func(values []int) []int { slices.Sort(values); return slices.Compact(values) }
	rows := make([]row, 0, len(byName))
	for name, value := range byName {
		value.Defaults.BPUse = unique(value.Defaults.BPUse)
		value.Defaults.Experience = unique(value.Defaults.Experience)
		value.Current = value.Defaults
		if rule, ok := c.economy[name]; ok {
			copy := rule
			value.Override = &copy
			if rule.BPUse != nil {
				value.Current.BPUse = []int{*rule.BPUse}
			}
			if rule.Experience != nil {
				value.Current.Experience = []int{*rule.Experience}
			}
		}
		for _, bp := range value.Defaults.BPUse {
			value.Defaults.BPUseHalf = append(value.Defaults.BPUseHalf, (bp+1)/2)
		}
		for _, bp := range value.Current.BPUse {
			value.Current.BPUseHalf = append(value.Current.BPUseHalf, (bp+1)/2)
		}
		rows = append(rows, *value)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Difficulty < rows[j].Difficulty })
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": c.economyRevision, "rules": rows,
		"limits":      map[string]int{"bp_use_min": 1, "bp_use_max": 1000, "experience_min": 0, "experience_max": 10000000},
		"directories": []string{"activity_3d", "activity_2d", "past"}, "half_rounding": "ceil"})
}

func (a *API) saveBossEconomy(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int `json:"expected_revision"`
		Rules    []struct {
			Difficulty string `json:"difficulty"`
			BPUse      *int   `json:"bp_use"`
			Experience *int   `json:"experience"`
			Reset      bool   `json:"reset"`
		} `json:"rules"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Expected == nil || *body.Expected < 0 || len(body.Rules) == 0 || len(body.Rules) > 100 {
		WriteAdminError(w, 400, "请提交版本及1至100个难度设置")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入活动目录")
		return
	}
	next := maps.Clone(c.economy)
	seen := map[string]bool{}
	for _, input := range body.Rules {
		if seen[input.Difficulty] {
			WriteAdminError(w, 400, "难度重复")
			return
		}
		seen[input.Difficulty] = true
		rule := bossEconomyOverride{input.BPUse, input.Experience}
		if input.Reset {
			if input.BPUse != nil || input.Experience != nil {
				WriteAdminError(w, 400, "恢复默认时不应提交覆盖值")
				return
			}
			minimum := 1
			if err := c.validateBossEconomy(input.Difficulty, bossEconomyOverride{BPUse: &minimum}); err != nil {
				WriteAdminError(w, 400, err.Error())
				return
			}
			delete(next, input.Difficulty)
		} else {
			if err := c.validateBossEconomy(input.Difficulty, rule); err != nil {
				WriteAdminError(w, 400, err.Error())
				return
			}
			next[input.Difficulty] = rule
		}
	}
	candidate := *c
	candidate.economy = next
	state, err := candidate.project(c.drops, c.shops, c.rules)
	if err != nil {
		writeContentError(w, err)
		return
	}
	configuration, err := game.NewContentConfiguration(c.configuration.Revision+1, state)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := o.writeDocument(bossEconomyKey, *body.Expected, next)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.economy, c.economyRevision, c.configuration = next, doc.Revision, configuration
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision})
}
