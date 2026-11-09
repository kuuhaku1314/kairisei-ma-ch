package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const activityRewardsKey = "activity-rewards"

type exploreRewardSlot struct {
	Event   int                `json:"event"`
	Kind    string             `json:"kind"`
	Index   int                `json:"index"`
	Rewards []gamestate.Reward `json:"rewards"`
	Chances []int              `json:"chance_per_million,omitempty"`
}

type activityRewards struct {
	Cups    map[int]*gamestate.TeamBattleScorePolicy `json:"cups"`
	Explore []exploreRewardSlot                      `json:"explore"`
}

func exploreRewardSlots(events []json.RawMessage) ([]exploreRewardSlot, error) {
	result := []exploreRewardSlot{}
	for i, raw := range events {
		var event map[string]json.RawMessage
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, err
		}
		for _, kind := range []string{"symbols", "treasureboxes"} {
			var rows []struct {
				Rewards []gamestate.Reward `json:"reward"`
			}
			if len(event[kind]) == 0 {
				continue
			}
			if err := json.Unmarshal(event[kind], &rows); err != nil {
				return nil, err
			}
			for index, row := range rows {
				result = append(result, exploreRewardSlot{Event: i, Kind: kind, Index: index, Rewards: row.Rewards})
			}
		}
	}
	return result, nil
}

func (c *contentStore) activityDefaults() (activityRewards, error) {
	result := activityRewards{Cups: map[int]*gamestate.TeamBattleScorePolicy{}}
	for _, p := range c.base.TeamBattleRewards {
		if p.ScorePolicy != nil && p.ScorePolicy.IsDamageScore() && c.rewardBossID(p.BossID) == p.BossID {
			result.Cups[p.BossID] = gamestate.CloneTeamBattleScorePolicy(p.ScorePolicy)
		}
	}
	var err error
	result.Explore, err = exploreRewardSlots(c.base.Explore.Events)
	return result, err
}

func (c *contentStore) projectActivityRewards(state *gamestate.State) error {
	defaults, err := c.activityDefaults()
	if err != nil {
		return err
	}
	policies := defaults.Cups
	for id, p := range c.activities.Cups {
		if policies[id] == nil || p == nil || !p.IsDamageScore() {
			return errors.New("计分副本身份或计分方式无效")
		}
		if err := gamestate.ValidateTeamBattleScorePolicy(p); err != nil {
			return err
		}
		policies[id] = gamestate.CloneTeamBattleScorePolicy(p)
	}
	state.TeamBattleReplays = slices.Clone(c.base.TeamBattleReplays)
	for i := range state.TeamBattleRewards {
		p := &state.TeamBattleRewards[i]
		if policy := policies[c.rewardBossID(p.BossID)]; policy != nil {
			p.ScorePolicy = gamestate.CloneTeamBattleScorePolicy(policy)
		}
	}
	for i := range state.TeamBattleReplays {
		r := &state.TeamBattleReplays[i]
		if policy := policies[c.rewardBossID(r.BossID)]; policy != nil {
			r.EndTurn = policy.EndTurn
		}
	}
	// Preserve native event positions, dialogue and animation identities.
	state.Explore = c.base.Explore
	state.Explore.Events = slices.Clone(c.base.Explore.Events)
	if c.activities.Explore == nil {
		return nil
	}
	if len(c.activities.Explore) != len(defaults.Explore) {
		return errors.New("探索奖励槽数量与当前场景不一致")
	}
	for i, slot := range c.activities.Explore {
		base := defaults.Explore[i]
		if slot.Event != base.Event || slot.Kind != base.Kind || slot.Index != base.Index || slot.Rewards == nil || len(slot.Rewards) > 10 {
			return errors.New("探索奖励槽身份或奖励数量无效")
		}
		if slot.Chances != nil && len(slot.Chances) != len(slot.Rewards) {
			return errors.New("探索掉落概率与奖励数量不一致")
		}
		for _, chance := range slot.Chances {
			if chance < 0 || chance > 1000000 {
				return errors.New("探索掉落概率必须在0%至100%之间")
			}
		}
		var event map[string]json.RawMessage
		if err := json.Unmarshal(state.Explore.Events[slot.Event], &event); err != nil {
			return err
		}
		var rows []map[string]json.RawMessage
		if err := json.Unmarshal(event[slot.Kind], &rows); err != nil {
			return err
		}
		rows[slot.Index]["reward"], _ = json.Marshal(slot.Rewards)
		if slot.Chances != nil {
			// Consumed and removed by ExploreStart before the native DTO is sent.
			rows[slot.Index]["local_reward_chances"], _ = json.Marshal(slot.Chances)
		}
		event[slot.Kind], _ = json.Marshal(rows)
		state.Explore.Events[slot.Event], _ = json.Marshal(event)
	}
	return nil
}

func (a *API) activityRewardEditor(w http.ResponseWriter, r *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入活动目录")
		return
	}
	config, err := c.activityDefaults()
	if err != nil {
		writeContentError(w, err)
		return
	}
	for id, p := range c.activities.Cups {
		config.Cups[id] = p
	}
	if c.activities.Explore != nil {
		config.Explore = c.activities.Explore
	}
	names := map[int]string{}
	targets := map[int][]dropTarget{}
	for id, p := range config.Cups {
		policy := gamestate.CloneTeamBattleScorePolicy(p)
		if policy.TeamCostInitial == 0 {
			policy.TeamCostInitial = 3
		}
		config.Cups[id] = policy
		b := c.bosses[id]
		names[id] = b.Name + " · " + b.Difficulty
		targets[id] = b.Targets
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": c.activityRevision, "config": config, "names": names, "boss_targets": targets})
}

func (a *API) validateActivityReward(r gamestate.Reward) error {
	if r.Type == 4 || r.Type == 10 || r.Type == 12 {
		if r.RewardTypeID != 0 || r.Num < 1 || r.Num > 10000000 || r.CardLevel != 0 || r.CardFame != 0 || r.CardLove != 0 || r.CardSkillLevels == nil || len(r.CardSkillLevels) != 0 {
			return errors.New("货币奖励格式不正确")
		}
		return nil
	}
	return a.validateContentReward(r)
}

func (a *API) saveActivityRewards(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int            `json:"expected_revision"`
		Config   activityRewards `json:"config"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Expected == nil || body.Config.Cups == nil || body.Config.Explore == nil {
		WriteAdminError(w, 400, "请提交版本、计分副本与探索奖励配置")
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
	for id, p := range body.Config.Cups {
		if p == nil {
			WriteAdminError(w, 400, "计分副本规则不能为空")
			return
		}
		for _, g := range p.Grades {
			for _, reward := range g.Rewards {
				if err := a.validateActivityReward(reward); err != nil {
					WriteAdminError(w, 400, fmt.Sprintf("计分副本%d：%v", id, err))
					return
				}
			}
		}
	}
	for _, slot := range body.Config.Explore {
		for _, reward := range slot.Rewards {
			if err := a.validateActivityReward(reward); err != nil {
				WriteAdminError(w, 400, fmt.Sprintf("探索：%v", err))
				return
			}
		}
	}
	candidate := *c
	candidate.activities = body.Config
	state, err := candidate.project(c.drops, c.shops, c.rules)
	if err != nil {
		WriteAdminError(w, http.StatusBadRequest, err.Error())
		return
	}
	configuration, err := game.NewContentConfiguration(c.configuration.Revision+1, state)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := o.writeDocument(activityRewardsKey, *body.Expected, body.Config)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.activities, c.activityRevision = body.Config, doc.Revision
	c.configuration = configuration
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision})
}
