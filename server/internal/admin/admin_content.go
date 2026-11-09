package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
)

const bossDropsKey = "boss-drops"

const exchangesKey = "exchange-shops"

type BossDrops struct {
	BossID      int                             `json:"boss_id"`
	Drops       []gamestate.TeamBattleEnemyDrop `json:"enemy_drops"`
	FameRewards []gamestate.Reward              `json:"fame_rewards"`
}

func editableDrops(drops []gamestate.TeamBattleEnemyDrop) []gamestate.TeamBattleEnemyDrop {
	result := make([]gamestate.TeamBattleEnemyDrop, 0, len(drops))
	for _, d := range drops {
		if editableDroppedReward(d.Reward) {
			result = append(result, d)
		}
	}
	return result
}

func editableDroppedReward(r gamestate.Reward) bool {
	return r.Type == 6 || r.Type == 8 || r.Type == 13
}

type dropTarget struct {
	BattleIndex int         `json:"battle_index"`
	EnemyIndex  int         `json:"enemy_index"`
	Name        string      `json:"name"`
	Stats       *enemyStats `json:"stats,omitempty"`
}

type enemyStats struct {
	EnemyID         int    `json:"enemy_id"`
	Attribute       string `json:"attribute"`
	HP              int    `json:"hp"`
	Attack          int    `json:"attack"`
	Magic           int    `json:"magic"`
	Defense         int    `json:"defense"`
	MagicDefense    int    `json:"magic_defense"`
	DamageReduction int    `json:"damage_reduction"`
	AttributeFixed  [5]int `json:"attribute_fixed"`
}

type DropBoss struct {
	Category   string       `json:"category"`
	BossID     int          `json:"boss_id"`
	GroupID    int          `json:"group_id"`
	Name       string       `json:"name"`
	Difficulty string       `json:"difficulty"`
	Targets    []dropTarget `json:"targets"`
	TowerID    int          `json:"tower_id,omitempty"`
	Floor      int          `json:"floor,omitempty"`
}

type contentStore struct {
	activities        activityRewards
	activityRevision  int
	base              gamestate.State
	bosses            map[int]DropBoss
	towerBosses       map[string]DropBoss
	towerDrops        map[string]TowerFloorDrops
	towerDropRevision int
	drops             map[int]BossDrops
	shops             map[int]exchangeShop
	dropRevision      int
	shopRevision      int
	rules             map[int]BossRules
	ruleRevision      int
	ruleBases         map[int]bossRuleEntry
	economy           map[string]bossEconomyOverride
	economyBases      map[int]bossEconomyBase
	economyRevision   int
	configuration     game.ContentConfiguration
}

type exchangeShop struct {
	gamestate.TradeShopProfile
	Deleted bool `json:"deleted,omitempty"`
}

func (o *Operations) InitializeContent(base gamestate.State, battleMasterPath string) error {
	c := &contentStore{base: base, bosses: map[int]DropBoss{}, drops: map[int]BossDrops{}, shops: map[int]exchangeShop{}}
	c.ruleBases = map[int]bossRuleEntry{}
	// The runtime master and its CSV directory are packaged together.
	root := filepath.Join(filepath.Dir(battleMasterPath), "cn602-battle-master")
	parties := map[int]multiplayer.CombatEnemyParty{}
	enemies := map[int]multiplayer.CombatEnemyDefinition{}
	if _, err := os.Stat(filepath.Join(root, "enemy_party.csv")); err == nil {
		var err error
		parties, enemies, err = multiplayer.LoadOperationsEnemyCatalog(root)
		if err != nil {
			return err
		}
	}
	standalone := map[int]bool{}
	pastBosses := map[int]bool{}
	for _, raw := range base.TeamBattlePastBossGroups {
		var g masterdata.BattlePastBossGroupIdentity
		if err := json.Unmarshal(raw, &g); err != nil {
			return err
		}
		for _, b := range g.Bosses {
			pastBosses[b.BossID] = true
		}
	}
	for _, p := range base.TeamBattleRewards {
		if p.StageQuestAreaID == 0 && p.TowerID == 0 {
			standalone[p.BossID] = true
		}
	}
	replays := map[int]gamestate.TeamBattleReplay{}
	for _, r := range base.TeamBattleReplays {
		replays[r.BossID] = r
	}
	var top map[string][]json.RawMessage
	// Top also contains scalar keys; decode only the four group arrays.
	var rawTop map[string]json.RawMessage
	_ = json.Unmarshal(base.TeamBattleSolo, &rawTop)
	top = make(map[string][]json.RawMessage)
	for _, key := range []string{"9", "10", "11", "12"} {
		var rows []json.RawMessage
		_ = json.Unmarshal(rawTop[key], &rows)
		top[key] = rows
	}
	for _, rows := range top {
		for _, raw := range rows {
			var group struct {
				ID     int    `json:"0"`
				Name   string `json:"4"`
				Bosses []struct {
					ID         int    `json:"0"`
					Difficulty string `json:"4"`
					BPUse      int    `json:"5"`
					BPUseHalf  int    `json:"6"`
					Continue   int    `json:"7"`
					StartRule  int    `json:"24"`
					OnlyMyDeck int    `json:"1"`
				} `json:"10"`
			}
			if err := json.Unmarshal(raw, &group); err != nil {
				return err
			}
			for _, b := range group.Bosses {
				if !standalone[b.ID] {
					continue
				}
				row := DropBoss{BossID: b.ID, GroupID: group.ID, Name: group.Name, Difficulty: b.Difficulty, Targets: []dropTarget{}}
				if b.StartRule == 0 && b.OnlyMyDeck == 0 {
					rule := BossRules{BossID: b.ID, Standard: true, Continue: b.Continue != 0}
					c.ruleBases[b.ID] = bossRuleEntry{BossRules: rule, GroupID: group.ID, BPUse: b.BPUse, BPUseHalf: b.BPUseHalf, Default: rule}
				}
				row.Category = "boss"
				if pastBosses[b.ID] {
					row.Category = "past"
				} else if _, material := masterdata.BattleMaterialGroupIcons[b.ID/100]; material {
					row.Category = "material"
				}
				row.Targets = dropTargetsForReplay(replays[b.ID], parties, enemies)
				c.bosses[b.ID] = row
			}
		}
	}
	if err := c.initializeTowerDrops(o, replays, parties, enemies); err != nil {
		return err
	}
	for ownID, sourceID := range base.TeamBattleOwnDeckSources {
		if row, ok := c.ruleBases[sourceID]; ok {
			row.OwnDeckBossID = ownID
			row.OwnDeck = !base.DisabledTeamBattleBossIDs[ownID]
			row.Default.OwnDeck = row.OwnDeck
			c.ruleBases[sourceID] = row
		}
	}
	doc, err := o.storage.ReadDocument(bossDropsKey)
	if err != nil {
		return err
	}
	c.dropRevision = doc.Revision
	if doc.Revision > 0 {
		if err = json.Unmarshal(doc.Payload, &c.drops); err != nil {
			return err
		}
	}
	doc, err = o.storage.ReadDocument(exchangesKey)
	if err != nil {
		return err
	}
	c.shopRevision = doc.Revision
	if doc.Revision > 0 {
		if err = json.Unmarshal(doc.Payload, &c.shops); err != nil {
			return err
		}
	}
	c.rules = map[int]BossRules{}
	doc, err = o.storage.ReadDocument(bossRulesKey)
	if err != nil {
		return err
	}
	c.ruleRevision = doc.Revision
	if doc.Revision > 0 {
		if err = json.Unmarshal(doc.Payload, &c.rules); err != nil {
			return err
		}
		for id, rule := range c.rules {
			if id != rule.BossID {
				return errors.New("BOSS规则身份不一致")
			}
			// Cups previously published as ordinary bosses may have a saved
			// continue override. Preserve their publication, but adopt the new
			// finite-turn score contract when loading that historical override.
			for _, p := range c.base.TeamBattleRewards {
				if p.BossID == id && p.ScorePolicy != nil {
					rule.Continue = false
					c.rules[id] = rule
					break
				}
			}
			if err := c.validateBossRules(rule); err != nil {
				return err
			}
		}
	}
	doc, err = o.storage.ReadDocument(activityRewardsKey)
	if err != nil {
		return err
	}
	c.activityRevision = doc.Revision
	if doc.Revision > 0 {
		if err = json.Unmarshal(doc.Payload, &c.activities); err != nil {
			return err
		}
	}
	if err := c.loadBossEconomy(o, battleMasterPath); err != nil {
		return err
	}
	state, err := c.project(c.drops, c.shops, c.rules)
	if err != nil {
		return err
	}
	c.configuration, err = game.NewContentConfiguration(1, state)
	if err != nil {
		return err
	}
	o.content = c
	o.publishRuntimeLocked()
	return nil
}

func (c *contentStore) project(drops map[int]BossDrops, shops map[int]exchangeShop, rules map[int]BossRules) (gamestate.State, error) {
	state := gamestate.State{TeamBattleSolo: c.base.TeamBattleSolo, TeamBattlePastBossGroups: c.base.TeamBattlePastBossGroups}
	state.TeamBattleRewards = append([]gamestate.TeamBattleRewardProfile(nil), c.base.TeamBattleRewards...)
	byBoss := map[int][]map[string]int{}
	for i, p := range state.TeamBattleRewards {
		config, ok := drops[c.rewardBossID(p.BossID)]
		if !ok {
			config, ok = drops[p.BossID]
		}
		if !ok || p.StageQuestAreaID != 0 || p.TowerID != 0 {
			continue
		}
		p, view := projectBattleDrops(p, config)
		if err := masterdata.ValidateBattleDropSlots(p.EnemyDrops); err != nil {
			return state, fmt.Errorf("BOSS %d: %w", p.BossID, err)
		}
		byBoss[p.BossID] = view
		state.TeamBattleRewards[i] = p
	}
	if len(drops) > 0 {
		var err error
		state.TeamBattleSolo, err = projectConfiguredDropViews(state.TeamBattleSolo, byBoss)
		if err != nil {
			return state, err
		}
		state.TeamBattlePastBossGroups, err = masterdata.ProjectPastBossDropCatalog(state.TeamBattlePastBossGroups, state.TeamBattleRewards, c.base.CardActions.EvolutionTransitions)
		if err != nil {
			return state, fmt.Errorf("往期入口需保留至少一张概率大于0的普通卡：%w", err)
		}
	}
	merged := map[int]gamestate.TradeShopProfile{}
	for _, s := range c.base.TradeShopProfiles {
		merged[s.TradeShopID] = s
	}
	for id, s := range shops {
		if s.Deleted {
			delete(merged, id)
		} else {
			merged[id] = s.TradeShopProfile
		}
	}
	ids := make([]int, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		state.TradeShopProfiles = append(state.TradeShopProfiles, merged[id])
	}
	if err := c.projectTowerDrops(&state); err != nil {
		return state, err
	}
	err := c.projectBossRules(&state, rules)
	if err == nil {
		err = c.projectActivityRewards(&state)
	}
	if err == nil {
		err = c.projectBossEconomy(&state)
	}
	return state, err
}

func projectConfiguredDropViews(raw json.RawMessage, byBoss map[int][]map[string]int) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	for _, key := range []string{"9", "10", "11", "12"} {
		if len(top[key]) == 0 {
			continue
		}
		var groups []map[string]json.RawMessage
		if err := json.Unmarshal(top[key], &groups); err != nil {
			return nil, err
		}
		for _, g := range groups {
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(g["10"], &bosses); err != nil {
				return nil, err
			}
			for _, b := range bosses {
				var id int
				_ = json.Unmarshal(b["0"], &id)
				if view, ok := byBoss[id]; ok {
					b["12"], _ = json.Marshal(view)
				}
			}
			g["10"], _ = json.Marshal(bosses)
		}
		top[key], _ = json.Marshal(groups)
	}
	return json.Marshal(top)
}

func (a *API) validateContentReward(r gamestate.Reward) error {
	if r.Type != 6 && r.Type != 8 && r.Type != 13 && r.Type != 15 && r.Type != 19 && !gamestate.IsCollectionReward(r.Type) {
		return errors.New("请选择卡牌、素材、道具、召唤石、传承卡、皮肤、对话表情或称号")
	}
	canonical, _, err := a.mailReward(AdminMailRequest{RewardType: r.Type, RewardTypeID: r.RewardTypeID, Quantity: r.Num, CardLevel: int(r.CardLevel), CardFame: int(r.CardFame), CardLove: r.CardLove})
	if err != nil {
		return err
	}
	if canonical.CardLevel != r.CardLevel || canonical.CardFame != r.CardFame || !slices.Equal(canonical.CardSkillLevels, r.CardSkillLevels) || r.CardSkillLevels == nil {
		return errors.New("奖励的卡牌等级／技能格式不正确")
	}
	return nil
}

func (a *API) validateDropConfig(c BossDrops) error {
	row, ok := a.operations.content.bosses[c.BossID]
	if !ok {
		return errors.New("该BOSS没有可编辑的活动掉落配置")
	}
	return a.validateDropsFor(row, c)
}

func (a *API) validateDropsFor(row DropBoss, c BossDrops) error {
	if c.Drops == nil || len(c.Drops) > 120 {
		return errors.New("掉落表须为数组，每个难度最多120项")
	}
	if len(c.FameRewards) > 120 {
		return errors.New("名声奖励候选最多120项")
	}
	for _, reward := range c.FameRewards {
		if !editableDroppedReward(reward) {
			return errors.New("名声奖励支持普通卡、素材卡和道具")
		}
		if err := a.validateContentReward(reward); err != nil {
			return err
		}
	}
	for _, d := range c.Drops {
		if !editableDroppedReward(d.Reward) {
			return errors.New("战斗掉落仅支持普通卡、素材卡和道具")
		}
		if !slices.ContainsFunc(row.Targets, func(t dropTarget) bool { return t.BattleIndex == d.BattleIndex && t.EnemyIndex == d.EnemyIndex }) {
			return errors.New("掉落目标不是此难度的有效波次／怪物／部位")
		}
		if d.ChancePerMillion != nil && (*d.ChancePerMillion < 0 || *d.ChancePerMillion > 1000000) {
			return errors.New("掉落概率须在0%至100%之间")
		}
		if err := a.validateContentReward(d.Reward); err != nil {
			return err
		}
	}
	return masterdata.ValidateBattleDropSlots(c.Drops)
}

func (a *API) dropEditor(w http.ResponseWriter, r *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入掉落目录")
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("boss_id"))
	if id == 0 {
		rows := make([]DropBoss, 0, len(c.bosses))
		for _, row := range c.bosses {
			if c.rewardBossID(row.BossID) != row.BossID {
				continue
			}
			rows = append(rows, row)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].BossID < rows[j].BossID })
		WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "bosses": rows})
		return
	}
	id = c.rewardBossID(id)
	row, ok := c.bosses[id]
	if !ok {
		WriteAdminError(w, 404, "BOSS不存在")
		return
	}
	base := BossDrops{BossID: id, Drops: []gamestate.TeamBattleEnemyDrop{}}
	for _, p := range c.base.TeamBattleRewards {
		if p.BossID == id && p.StageQuestAreaID == 0 && p.TowerID == 0 {
			base.Drops = editableDrops(p.EnemyDrops)
			base.FameRewards = slices.Clone(p.FameRewards)
			break
		}
	}
	config, ok := c.drops[id]
	if !ok {
		config = base
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "boss": row, "config": config, "base": base, "revision": c.dropRevision})
}

func (a *API) saveDropEditor(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int        `json:"expected_revision"`
		Configs  []BossDrops `json:"configs"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 4*1024*1024); err != nil || body.Expected == nil || len(body.Configs) == 0 || len(body.Configs) > 100 {
		WriteAdminError(w, 400, "请提交配置版本和1至100个难度")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入目录")
		return
	}
	next := make(map[int]BossDrops, len(c.drops))
	for k, v := range c.drops {
		next[k] = v
	}
	seen := map[int]bool{}
	for _, config := range body.Configs {
		config.BossID = c.rewardBossID(config.BossID)
		if seen[config.BossID] {
			WriteAdminError(w, 400, "难度重复")
			return
		}
		seen[config.BossID] = true
		if err := a.validateDropConfig(config); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
		next[config.BossID] = config
	}
	state, err := c.project(next, c.shops, c.rules)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	configuration, err := game.NewContentConfiguration(c.configuration.Revision+1, state)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := o.writeDocument(bossDropsKey, *body.Expected, next)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.drops = next
	c.dropRevision = doc.Revision
	c.configuration = configuration
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision})
}

func writeContentError(w http.ResponseWriter, err error) {
	status := 500
	if errors.Is(err, accountstore.ErrDocumentConflict) {
		status = 409
	}
	WriteAdminError(w, status, err.Error())
}

func (a *API) exchangeEditor(w http.ResponseWriter, _ *http.Request) {
	o := a.operations
	o.configMu.RLock()
	defer o.configMu.RUnlock()
	if o.content == nil {
		WriteAdminError(w, 503, "未载入兑换目录")
		return
	}
	shops := o.content.exchangeShops()
	nextShop, nextLineup := 61000001, 62000001
	for _, s := range shops {
		if s.TradeShopID >= nextShop {
			nextShop = s.TradeShopID + 1
		}
		for _, l := range s.Lineups {
			if l.LineupID >= nextLineup {
				nextLineup = l.LineupID + 1
			}
		}
	}
	templates := map[string][]AdminCatalogEntry{"fusion": {}, "evolution": {}}
	evolutionIDs := map[int]bool{}
	for _, transition := range o.content.base.CardActions.EvolutionTransitions {
		for _, material := range transition.Materials {
			evolutionIDs[material.CardID] = true
		}
	}
	for _, material := range o.content.base.StackCardTemplates {
		entry, ok := a.catalogByKey[adminCatalogKey(13, material.CardID)]
		if !ok || entry.ResourceState == "unavailable" {
			continue
		}
		if material.MaterialType == 1 && material.AddExperience > 0 {
			templates["fusion"] = append(templates["fusion"], entry)
		}
		if evolutionIDs[material.CardID] {
			templates["evolution"] = append(templates["evolution"], entry)
		}
	}
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "shops": shops, "revision": o.content.shopRevision, "next_shop_id": nextShop, "next_lineup_id": nextLineup, "templates": templates})
}

func (a *API) validateExchange(s gamestate.TradeShopProfile, others []gamestate.TradeShopProfile) error {
	if s.TradeShopID <= 0 || len([]rune(strings.TrimSpace(s.Name))) < 1 || len([]rune(s.Name)) > 40 || len([]rune(s.Text)) > 250 || s.ShopType != 1 || s.TabType < 0 || s.TabType > 1 || s.EndTime < 0 || s.EndTime > 2147483647 || s.PictID != 0 || s.IsNew != 0 || len(s.Lineups) == 0 || len(s.Lineups) > 500 {
		return errors.New("兑换所名称、分类、时间或商品数无效（最多500项）")
	}
	if s.StartTime < 0 || s.StartTime > 2147483647 || s.StartTime > 0 && s.EndTime > 0 && s.EndTime < 2147483647 && s.StartTime >= s.EndTime {
		return errors.New("兑换所开始时间须早于结束时间")
	}
	ids := map[int]bool{}
	old := map[int]gamestate.TradeShopLineupProfile{}
	existed := false
	for _, o := range others {
		if o.TradeShopID == s.TradeShopID {
			existed = true
			for _, l := range o.Lineups {
				old[l.LineupID] = l
			}
		} else {
			for _, l := range o.Lineups {
				ids[l.LineupID] = true
			}
		}
	}
	if !existed && s.TradeShopID < 61000001 {
		return errors.New("新增兑换所ID须使用后台分配的范围")
	}
	for _, l := range s.Lineups {
		if l.LineupID <= 0 || ids[l.LineupID] || strings.TrimSpace(l.LineupName) == "" || len([]rune(l.LineupName)) > 60 || l.StockNum == 0 || l.StockNum < -1 || l.StockNum > 999999 || l.PictID != 0 || l.IsLineupNew != 0 || l.IsLineupOld != 0 || len(l.Prices) != 1 || len(l.Rewards) != 1 {
			return errors.New("商品ID、名称、库存或奖励格式无效；库存-1表示不限量")
		}
		ids[l.LineupID] = true
		p := l.Prices[0]
		if p.Type != 4 || p.Num < 1 || p.Num > 10000000 || p.PointCardCondition == nil || len(p.PointCardCondition) > 0 {
			return errors.New("原兑换流程需要一项道具货币，价格须为1至10000000")
		}
		if _, ok := a.catalogByKey[adminCatalogKey(8, p.ID)]; !ok {
			return errors.New("兑换货币不在当前道具目录中")
		}
		if err := a.validateContentReward(l.Rewards[0]); err != nil {
			return err
		}
		if previous, ok := old[l.LineupID]; ok {
			if previous.Rewards[0].Type != l.Rewards[0].Type || previous.Rewards[0].RewardTypeID != l.Rewards[0].RewardTypeID {
				return errors.New("已有商品不能复用ID更换奖励；请下架旧商品并新增，保留兑换次数")
			}
			delete(old, l.LineupID)
		} else if l.LineupID < 62000001 {
			return errors.New("新增商品ID须使用后台分配的范围")
		}
	}
	if len(old) > 0 {
		return errors.New("请下架已有商品，不能删除历史商品及其限购身份")
	}
	return nil
}

func (a *API) saveExchangeEditor(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int                       `json:"expected_revision"`
		Shop     gamestate.TradeShopProfile `json:"shop"`
	}
	if err := DecodeAdminJSONLimit(r, &body, 1024*1024); err != nil || body.Expected == nil {
		WriteAdminError(w, 400, "缺少兑换配置或版本")
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "shopID"))
	if err != nil || id != body.Shop.TradeShopID {
		WriteAdminError(w, 400, "兑换所ID不一致")
		return
	}
	o := a.operations
	o.configMu.Lock()
	defer o.configMu.Unlock()
	c := o.content
	if c == nil {
		WriteAdminError(w, 503, "未载入目录")
		return
	}
	if c.shops[id].Deleted {
		WriteAdminError(w, 409, "请先从回收站恢复兑换所")
		return
	}
	if err := a.validateExchange(body.Shop, c.exchangeProfiles()); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	next := make(map[int]exchangeShop, len(c.shops))
	for k, v := range c.shops {
		next[k] = v
	}
	next[id] = exchangeShop{TradeShopProfile: body.Shop}
	state, err := c.project(c.drops, next, c.rules)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if len(state.TradeShopProfiles) > 50 {
		WriteAdminError(w, 400, "最多50个兑换所")
		return
	}
	configuration, err := game.NewContentConfiguration(c.configuration.Revision+1, state)
	if err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := o.writeDocument(exchangesKey, *body.Expected, next)
	if err != nil {
		writeContentError(w, err)
		return
	}
	c.shops = next
	c.shopRevision = doc.Revision
	c.configuration = configuration
	o.publishRuntimeLocked()
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "revision": doc.Revision, "shop": body.Shop})
}
