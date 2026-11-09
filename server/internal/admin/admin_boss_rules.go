package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sort"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const bossRulesKey = "boss-rules"

func (c *contentStore) rewardBossID(id int) int {
	if sourceID := c.base.TeamBattleOwnDeckSources[id]; sourceID != 0 {
		return sourceID
	}
	return id
}

type BossRules struct {
	BossID   int  `json:"boss_id"`
	Standard bool `json:"standard"`
	OwnDeck  bool `json:"own_deck"`
	Continue bool `json:"continue"`
}

type bossRuleEntry struct {
	BossRules
	GroupID       int       `json:"group_id"`
	BPUse         int       `json:"bp_use"`
	BPUseHalf     int       `json:"bp_use_half"`
	Default       BossRules `json:"default"`
	OwnDeckBossID int       `json:"own_deck_boss_id,omitempty"`
}

func (c *contentStore) defaultBossRules(id int) (bossRuleEntry, bool) {
	row, ok := c.ruleBases[id]
	return row, ok
}

func (c *contentStore) validateBossRules(rule BossRules) error {
	if rule.Continue {
		for _, p := range c.base.TeamBattleRewards {
			if p.BossID == rule.BossID && p.ScorePolicy != nil {
				return errors.New("圣剑杯计分战斗不允许复活")
			}
		}
	}
	base, ok := c.defaultBossRules(rule.BossID)
	if !ok {
		return errors.New("该难度不支持运营规则配置")
	}
	if rule.OwnDeck && base.OwnDeckBossID == 0 {
		return errors.New("当前难度缺少可用的自卡组战斗配置")
	}
	if rule.Standard && (base.BPUseHalf <= 0 || base.BPUseHalf > base.BPUse) {
		return errors.New("此难度缺少有效组队体力消耗，不能开放组队")
	}
	return nil
}

// Update both native catalog shapes, leaving identity, rewards and progress intact.
func projectBossRuleGroup(raw json.RawMessage, bossKey string, rules map[int]bool) (json.RawMessage, error) {
	var group map[string]json.RawMessage
	if err := json.Unmarshal(raw, &group); err != nil {
		return nil, err
	}
	var bosses []map[string]json.RawMessage
	if err := json.Unmarshal(group[bossKey], &bosses); err != nil {
		return nil, err
	}
	for _, boss := range bosses {
		var id int
		if err := json.Unmarshal(boss["0"], &id); err != nil {
			return nil, err
		}
		if allowContinue, ok := rules[id]; ok {
			revive := 0
			if allowContinue {
				revive = 1
			}
			boss["7"], _ = json.Marshal(revive)
		}
	}
	group[bossKey], _ = json.Marshal(bosses)
	return json.Marshal(group)
}

func (c *contentStore) projectBossRules(state *gamestate.State, overrides map[int]BossRules) error {
	rules := map[int]bool{}
	state.DisabledTeamBattleBossIDs = maps.Clone(c.base.DisabledTeamBattleBossIDs)
	if state.DisabledTeamBattleBossIDs == nil {
		state.DisabledTeamBattleBossIDs = map[int]bool{}
	}
	for id, rule := range overrides {
		rules[id] = rule.Continue
		if !rule.Standard {
			state.DisabledTeamBattleBossIDs[id] = true
		} else {
			delete(state.DisabledTeamBattleBossIDs, id)
		}
		if ownID := c.ruleBases[id].OwnDeckBossID; ownID != 0 {
			rules[ownID] = rule.Continue
			if !rule.OwnDeck {
				state.DisabledTeamBattleBossIDs[ownID] = true
			} else {
				delete(state.DisabledTeamBattleBossIDs, ownID)
			}
		}
	}
	if len(rules) == 0 {
		return nil
	}
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
			updated, err := projectBossRuleGroup(raw, "10", rules)
			if err != nil {
				return err
			}
			groups[i] = updated
		}
		top[key], _ = json.Marshal(groups)
	}
	state.TeamBattleSolo, _ = json.Marshal(top)
	past := make([]json.RawMessage, len(state.TeamBattlePastBossGroups))
	for i, raw := range state.TeamBattlePastBossGroups {
		updated, err := projectBossRuleGroup(raw, "13", rules)
		if err != nil {
			return err
		}
		past[i] = updated
	}
	state.TeamBattlePastBossGroups = past
	return nil
}

func (a *API) bossRules(w http.ResponseWriter, r *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入BOSS目录")
		return
	}
	rows := make([]bossRuleEntry, 0, len(c.bosses))
	for id := range c.bosses {
		row, ok := c.defaultBossRules(id)
		if !ok {
			continue
		}
		if rule, ok := c.rules[id]; ok {
			row.BossRules = rule
		}
		if base, ok := c.economyBases[id]; ok {
			if rule := c.economy[base.Difficulty]; rule.BPUse != nil {
				row.BPUse = *rule.BPUse
				row.BPUseHalf = (row.BPUse + 1) / 2
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].BossID < rows[j].BossID })
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": c.ruleRevision, "rules": rows})
}

func (a *API) saveBossRules(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int `json:"expected_revision"`
		Rules    []struct {
			BossID   int   `json:"boss_id"`
			Standard *bool `json:"standard"`
			OwnDeck  *bool `json:"own_deck"`
			Continue *bool `json:"continue"`
		} `json:"rules"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Expected == nil || len(body.Rules) == 0 || len(body.Rules) > 100 {
		WriteAdminError(w, 400, "请提交版本及1至100个难度规则")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入BOSS目录")
		return
	}
	next := make(map[int]BossRules, len(c.rules)+len(body.Rules))
	for id, rule := range c.rules {
		next[id] = rule
	}
	seen := map[int]bool{}
	for _, input := range body.Rules {
		if input.Standard == nil || input.OwnDeck == nil || input.Continue == nil {
			WriteAdminError(w, 400, "每个难度须明确设置两种入口及复活选项")
			return
		}
		rule := BossRules{BossID: input.BossID, Standard: *input.Standard, OwnDeck: *input.OwnDeck, Continue: *input.Continue}
		if seen[rule.BossID] {
			WriteAdminError(w, 400, "难度重复")
			return
		}
		seen[rule.BossID] = true
		if err := c.validateBossRules(rule); err != nil {
			WriteAdminError(w, 400, fmt.Sprintf("%d：%s", rule.BossID, err))
			return
		}
		next[rule.BossID] = rule
	}
	state, err := c.project(c.drops, c.shops, next)
	if err != nil {
		writeContentError(w, err)
		return
	}
	configuration, err := game.NewContentConfiguration(c.configuration.Revision+1, state)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := o.writeDocument(bossRulesKey, *body.Expected, next)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.rules, c.ruleRevision = next, doc.Revision
	c.configuration = configuration
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision})
}
