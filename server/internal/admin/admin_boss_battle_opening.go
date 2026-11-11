package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const bossBattleOpeningKey = "boss-battle-opening"

// Nil fields inherit; an explicit zero gauge starts empty.
type bossBattleOpeningOverride struct {
	CostInitial       *int `json:"cost_initial,omitempty"`
	BurstGaugeInitial *int `json:"burst_gauge_initial,omitempty"`
}

type bossBattleOpeningConfig struct {
	Global bossBattleOpeningOverride         `json:"global"`
	Bosses map[int]bossBattleOpeningOverride `json:"bosses"`
}

func (rule bossBattleOpeningOverride) validate() error {
	if rule.CostInitial != nil && (*rule.CostInitial < gamestate.BattleOpeningCostMin || *rule.CostInitial > gamestate.BattleOpeningCostMax) {
		return fmt.Errorf("起始COST须为3至10")
	}
	if rule.BurstGaugeInitial != nil && (*rule.BurstGaugeInitial < 0 || *rule.BurstGaugeInitial > gamestate.BattleOpeningBurstGaugeMax) {
		return fmt.Errorf("开场圣剑槽须为0至300")
	}
	return nil
}

func (rule bossBattleOpeningOverride) apply(value gamestate.TeamBattleOpening) gamestate.TeamBattleOpening {
	if rule.CostInitial != nil {
		value.CostInitial = *rule.CostInitial
	}
	if rule.BurstGaugeInitial != nil {
		value.BurstGaugeInitial = *rule.BurstGaugeInitial
	}
	return value
}

func (c *contentStore) validateBossBattleOpening(config bossBattleOpeningConfig) error {
	if err := config.Global.validate(); err != nil {
		return err
	}
	for id, rule := range config.Bosses {
		if _, ok := c.battleOpeningBases[id]; !ok {
			return fmt.Errorf("BOSS %d不属于可配置的3D活动、2D活动或往期副本", id)
		}
		if err := rule.validate(); err != nil {
			return fmt.Errorf("BOSS %d：%w", id, err)
		}
	}
	return nil
}

func (c *contentStore) loadBossBattleOpening(o *Operations) error {
	c.battleOpeningBases = map[int]gamestate.TeamBattleOpening{}
	excluded := map[int]bool{}
	for _, reward := range c.base.TeamBattleRewards {
		if reward.ScorePolicy != nil || reward.StageQuestAreaID != 0 || reward.TowerID != 0 {
			excluded[c.rewardBossID(reward.BossID)] = true
		}
	}
	// Reuse the activity/past directory membership already resolved for economy.
	// Tower, main quests and key materials are outside that set; cups are separate.
	for _, replay := range c.base.TeamBattleReplays {
		id := c.rewardBossID(replay.BossID)
		if _, ok := c.economyBases[id]; ok && replay.BossID == id && !excluded[id] {
			c.battleOpeningBases[id] = gamestate.TeamBattleOpening{
				CostInitial: replay.CostInitial, BurstGaugeInitial: replay.BurstGaugeInitial,
			}
		}
	}
	doc, err := o.storage.ReadDocument(bossBattleOpeningKey)
	if err != nil {
		return err
	}
	c.battleOpening = bossBattleOpeningConfig{Bosses: map[int]bossBattleOpeningOverride{}}
	c.battleOpeningRevision = doc.Revision
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &c.battleOpening); err != nil {
			return err
		}
	}
	return c.validateBossBattleOpening(c.battleOpening)
}

func (c *contentStore) projectBossBattleOpening(state *gamestate.State) {
	for i := range state.TeamBattleReplays {
		replay := &state.TeamBattleReplays[i]
		id := c.rewardBossID(replay.BossID)
		if _, ok := c.battleOpeningBases[id]; !ok {
			continue
		}
		// Each variant retains its resource defaults for fields left unconfigured.
		value := gamestate.TeamBattleOpening{CostInitial: replay.CostInitial, BurstGaugeInitial: replay.BurstGaugeInitial}
		value = c.battleOpening.Bosses[id].apply(c.battleOpening.Global.apply(value))
		replay.CostInitial, replay.BurstGaugeInitial = value.CostInitial, value.BurstGaugeInitial
	}
}

func (a *API) bossBattleOpening(w http.ResponseWriter, r *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入活动目录")
		return
	}
	type row struct {
		DropBoss
		Defaults  gamestate.TeamBattleOpening `json:"defaults"`
		Inherited gamestate.TeamBattleOpening `json:"inherited"`
		Current   gamestate.TeamBattleOpening `json:"current"`
	}
	rows := make([]row, 0, len(c.battleOpeningBases))
	for id, base := range c.battleOpeningBases {
		inherited := c.battleOpening.Global.apply(base)
		rows = append(rows, row{c.bosses[id], base, inherited, c.battleOpening.Bosses[id].apply(inherited)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].BossID < rows[j].BossID })
	WriteAdminJSON(w, 200, map[string]any{
		"state": "PASS", "revision": c.battleOpeningRevision, "config": c.battleOpening, "bosses": rows,
		"limits":      map[string]int{"cost_initial_min": gamestate.BattleOpeningCostMin, "cost_initial_max": gamestate.BattleOpeningCostMax, "burst_gauge_initial_min": 0, "burst_gauge_initial_max": gamestate.BattleOpeningBurstGaugeMax},
		"burst_gauge": map[string]int{"normal_threshold": gamestate.BattleOpeningBurstGaugeReady, "maximum": gamestate.BattleOpeningBurstGaugeMax},
		"directories": []string{"activity_3d", "activity_2d", "past"},
	})
}

func (a *API) saveBossBattleOpening(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int                    `json:"expected_revision"`
		Config   bossBattleOpeningConfig `json:"config"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Expected == nil || *body.Expected < 0 || body.Config.Bosses == nil {
		WriteAdminError(w, 400, "请提交版本、全局设置和BOSS覆盖设置")
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
	if err := c.validateBossBattleOpening(body.Config); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	candidate := *c
	candidate.battleOpening = body.Config
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
	doc, err := o.writeDocument(bossBattleOpeningKey, *body.Expected, body.Config)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.battleOpening, c.battleOpeningRevision, c.configuration = body.Config, doc.Revision, configuration
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision})
}
