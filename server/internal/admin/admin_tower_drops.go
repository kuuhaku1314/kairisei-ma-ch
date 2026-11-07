package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

const towerDropsKey = "tower-drops"

// Tower/floor is the editor identity. A boss ID can also occur outside a tower.
type TowerFloorDrops struct {
	BossDrops
	TowerID int `json:"tower_id"`
	Floor   int `json:"floor"`
	// nil inherits the packaged first-clear rewards; [] explicitly disables them.
	FirstClearRewards *[]gamestate.Reward `json:"first_clear_rewards,omitempty"`
}

func towerFloorKey(towerID, floor int) string { return fmt.Sprintf("%d:%d", towerID, floor) }

func (c *contentStore) initializeTowerDrops(o *Operations, replays map[int]gamestate.TeamBattleReplay,
	parties map[int]multiplayer.CombatEnemyParty, enemies map[int]multiplayer.CombatEnemyDefinition) error {
	c.towerBosses = map[string]DropBoss{}
	c.towerDrops = map[string]TowerFloorDrops{}
	profiles := map[string]gamestate.TeamBattleRewardProfile{}
	for _, p := range c.base.TeamBattleRewards {
		if p.TowerID != 0 {
			profiles[towerFloorKey(p.TowerID, p.TowerFloor)] = p
		}
	}
	for _, tower := range c.base.TowerQuestProfiles {
		for _, floor := range tower.Floors {
			if floor.Story.TalkID != 0 {
				continue
			}
			key := towerFloorKey(tower.TowerID, floor.Floor)
			profile, ok := profiles[key]
			if !ok {
				return fmt.Errorf("炼金塔 %d 第 %d 层缺少战斗奖励定义", tower.TowerID, floor.Floor)
			}
			var boss struct {
				ID   int    `json:"0"`
				Name string `json:"11"`
			}
			if err := json.Unmarshal(floor.Boss, &boss); err != nil {
				return err
			}
			if boss.ID != profile.BossID {
				return errors.New("炼金塔楼层与战斗奖励身份不一致")
			}
			c.towerBosses[key] = DropBoss{Category: "tower", BossID: boss.ID, GroupID: tower.TowerID,
				TowerID: tower.TowerID, Floor: floor.Floor, Name: tower.Name,
				Difficulty: fmt.Sprintf("第 %d 层 · %s", floor.Floor, boss.Name),
				Targets:    dropTargetsForReplay(replays[boss.ID], parties, enemies)}
		}
	}
	doc, err := o.storage.ReadDocument(towerDropsKey)
	if err != nil {
		return err
	}
	c.towerDropRevision = doc.Revision
	if doc.Revision > 0 {
		if err := json.Unmarshal(doc.Payload, &c.towerDrops); err != nil {
			return err
		}
		for key, config := range c.towerDrops {
			row, ok := c.towerBosses[key]
			if config.TowerID <= 0 || config.Floor <= 0 || key != towerFloorKey(config.TowerID, config.Floor) {
				return errors.New("炼金塔掉落配置身份不一致")
			}
			if !ok {
				// Keep retired content settings stored, but only current tower
				// definitions participate in previews, admission and rewards.
				continue
			}
			if row.BossID != config.BossID {
				return errors.New("炼金塔掉落配置身份不一致")
			}
		}
	}
	return nil
}

func (c *contentStore) towerBaseDrops(row DropBoss) TowerFloorDrops {
	base := TowerFloorDrops{BossDrops: BossDrops{BossID: row.BossID, Drops: []gamestate.TeamBattleEnemyDrop{}}, TowerID: row.TowerID, Floor: row.Floor}
	for _, p := range c.base.TeamBattleRewards {
		if p.TowerID == row.TowerID && p.TowerFloor == row.Floor {
			base.Drops = editableDrops(p.EnemyDrops)
			base.FameRewards = slices.Clone(p.FameRewards)
			rewards := append([]gamestate.Reward{}, p.FirstClearRewards...)
			base.FirstClearRewards = &rewards
			break
		}
	}
	return base
}

func (c *contentStore) projectTowerDrops(state *gamestate.State) error {
	state.TowerQuestProfiles = slices.Clone(c.base.TowerQuestProfiles)
	views := map[string][]map[string]int{}
	firstClears := map[string][]gamestate.Reward{}
	for i, p := range state.TeamBattleRewards {
		if p.TowerID == 0 {
			continue
		}
		key := towerFloorKey(p.TowerID, p.TowerFloor)
		config, ok := c.towerDrops[key]
		if !ok {
			continue
		}
		p, view := projectBattleDrops(p, config.BossDrops)
		if config.FirstClearRewards != nil {
			p.FirstClearRewards = append([]gamestate.Reward{}, (*config.FirstClearRewards)...)
			firstClears[key] = p.FirstClearRewards
		}
		views[key] = view
		state.TeamBattleRewards[i] = p
	}
	for i, tower := range state.TowerQuestProfiles {
		state.TowerQuestProfiles[i].Floors = slices.Clone(tower.Floors)
		for j, floor := range tower.Floors {
			key := towerFloorKey(tower.TowerID, floor.Floor)
			if view, ok := views[key]; ok {
				var boss map[string]json.RawMessage
				if err := json.Unmarshal(floor.Boss, &boss); err != nil {
					return err
				}
				boss["12"], _ = json.Marshal(view)
				floor.Boss, _ = json.Marshal(boss)
			}
			if rewards, ok := firstClears[key]; ok {
				floor.ClearRewards = rewards
			}
			state.TowerQuestProfiles[i].Floors[j] = floor
		}
	}
	return nil
}

func (a *API) towerDropEditor(w http.ResponseWriter, r *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入炼金塔目录")
		return
	}
	if r.URL.Query().Get("tower_id") == "" && r.URL.Query().Get("floor") == "" {
		type towerRows struct {
			TowerID int        `json:"tower_id"`
			Name    string     `json:"name"`
			Floors  []DropBoss `json:"floors"`
		}
		towers := make([]towerRows, 0, len(c.base.TowerQuestProfiles))
		for _, tower := range c.base.TowerQuestProfiles {
			row := towerRows{TowerID: tower.TowerID, Name: tower.Name, Floors: []DropBoss{}}
			for _, floor := range tower.Floors {
				if boss, ok := c.towerBosses[towerFloorKey(tower.TowerID, floor.Floor)]; ok {
					row.Floors = append(row.Floors, boss)
				}
			}
			towers = append(towers, row)
		}
		sort.Slice(towers, func(i, j int) bool { return towers[i].TowerID < towers[j].TowerID })
		WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "towers": towers})
		return
	}
	towerID, err := strconv.Atoi(r.URL.Query().Get("tower_id"))
	floor, floorErr := strconv.Atoi(r.URL.Query().Get("floor"))
	if err != nil || floorErr != nil {
		WriteAdminError(w, 400, "请指定塔和楼层")
		return
	}
	key := towerFloorKey(towerID, floor)
	row, ok := c.towerBosses[key]
	if !ok {
		WriteAdminError(w, 404, "该炼金塔楼层没有可编辑的战斗掉落")
		return
	}
	base := c.towerBaseDrops(row)
	config, ok := c.towerDrops[key]
	if !ok {
		config = base
		config.FirstClearRewards = nil
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "boss": row, "config": config, "base": base, "revision": c.towerDropRevision})
}

func (a *API) saveTowerDropEditor(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int              `json:"expected_revision"`
		Configs  []TowerFloorDrops `json:"configs"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 4*1024*1024); err != nil || body.Expected == nil || len(body.Configs) < 1 || len(body.Configs) > 100 {
		WriteAdminError(w, 400, "请提交配置版本和1至100个楼层")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入炼金塔目录")
		return
	}
	next := make(map[string]TowerFloorDrops, len(c.towerDrops)+len(body.Configs))
	for key, config := range c.towerDrops {
		next[key] = config
	}
	seen := map[string]bool{}
	for _, config := range body.Configs {
		key := towerFloorKey(config.TowerID, config.Floor)
		row, ok := c.towerBosses[key]
		if !ok || row.BossID != config.BossID || seen[key] {
			WriteAdminError(w, 400, "炼金塔、楼层或战斗身份无效／重复")
			return
		}
		seen[key] = true
		if err := a.validateDropsFor(row, config.BossDrops); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
		if config.FirstClearRewards != nil {
			if len(*config.FirstClearRewards) > 120 {
				WriteAdminError(w, 400, "首通奖励最多120项")
				return
			}
			for _, reward := range *config.FirstClearRewards {
				if err := a.validateActivityReward(reward); err != nil {
					WriteAdminError(w, 400, err.Error())
					return
				}
			}
		}
		next[key] = config
	}
	projected := *c
	projected.towerDrops = next
	state, err := projected.project(c.drops, c.shops, c.rules)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	configuration, err := game.NewContentConfiguration(c.configuration.Revision+1, state)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := o.writeDocument(towerDropsKey, *body.Expected, next)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.towerDrops = next
	c.towerDropRevision = doc.Revision
	c.configuration = configuration
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision})
}
