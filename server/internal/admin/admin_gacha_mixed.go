package admin

import (
	"errors"
	"fmt"
	"reflect"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

// Historical reward identities and gifts are fixed by the audited preset;
// operators can tune each step's price and weights, plus its publication window.
func ValidateMixedGachaConfig(catalog map[string]AdminCatalogEntry, base gamestate.GachaProfile, config AdminGachaConfig) (map[string]any, error) {
	return validateMixedGachaConfig(catalog, base, config, false)
}

func validateMixedGachaConfig(catalog map[string]AdminCatalogEntry, base gamestate.GachaProfile, config AdminGachaConfig, editable bool) (map[string]any, error) {
	if err := validateAdminGachaPayment(catalog, config); err != nil {
		return nil, err
	}
	if len(config.CardIDs) != 0 || len(config.Weights) != 0 || len(config.Steps) != len(base.Steps) {
		return nil, errors.New("混合池不能使用普通卡牌表；阶段数量须与创建规则一致")
	}
	if len(config.RewardPool) == 0 {
		return nil, errors.New("混合池至少需要一项奖励")
	}
	configured := adminConfiguredGacha(base, config).Profile
	if err := gamestate.ValidateGachaRules(configured); err != nil {
		return nil, err
	}
	canonical := [][]gamestate.WeightedReward{base.RewardPool}
	edited := [][]gamestate.WeightedReward{config.RewardPool}
	for i, step := range config.Steps {
		if err := validateAdminGachaPayment(catalog, AdminGachaConfig{PayType: step.PayType, PayTypeID: step.PayTypeID}); err != nil {
			return nil, fmt.Errorf("第%d阶段：%w", i+1, err)
		}
		if step.Price < 1 || step.Price > 10000000 {
			return nil, errors.New("阶段价格须为1–10000000")
		}
		canonical = append(canonical, base.Steps[i].RewardPool)
		edited = append(edited, step.RewardPool)
	}
	if len(config.Steps) > 0 && (config.Price != config.Steps[0].Price || !reflect.DeepEqual(config.RewardPool, config.Steps[0].RewardPool)) {
		return nil, errors.New("首阶段价格和奖励须与首页一致")
	}
	names := map[string]string{}
	blocked := []int{}
	for i, pool := range edited {
		if !editable && len(pool) != len(canonical[i]) {
			return nil, errors.New("混合池须保留预设奖励身份，调整每项权重即可")
		}
		if len(pool) > 6000 {
			return nil, errors.New("每个奖励表最多6000项")
		}
		for j, entry := range pool {
			if !editable && !reflect.DeepEqual(entry.Reward, canonical[i][j].Reward) {
				return nil, errors.New("此池须保留原奖励身份和数量")
			}
			if entry.Weight < 1 || entry.Weight > 1000000 {
				return nil, errors.New("奖励权重须为1–1000000")
			}
			key := adminCatalogKey(entry.Reward.Type, entry.Reward.RewardTypeID)
			item, ok := catalog[key]
			if !ok {
				return nil, fmt.Errorf("混合奖励不存在：%s", key)
			}
			if editable {
				// Reuse the Admin reward contract (quantity and card progression).
				// Resource readiness is reported by preview below, so drafts can
				// include a known reward whose assets have not been published yet.
				r := entry.Reward
				canonical, err := adminCatalogReward(AdminMailRequest{RewardType: r.Type, RewardTypeID: r.RewardTypeID, Quantity: r.Num, CardLevel: int(r.CardLevel), CardFame: int(r.CardFame), CardLove: r.CardLove}, item)
				if err != nil {
					return nil, fmt.Errorf("奖励 %s：%w", key, err)
				}
				if !reflect.DeepEqual(canonical, r) {
					return nil, fmt.Errorf("奖励 %s 的卡牌成长字段无效", key)
				}
			}
			names[key] = item.Name
			if item.ResourceState == "unavailable" {
				blocked = append(blocked, entry.Reward.RewardTypeID)
			}
		}
	}
	stages := []game.GachaOddsStage{}
	if len(config.Steps) == 0 {
		var err error
		stages, err = game.PreviewGachaStages(configured, nil)
		if err != nil {
			return nil, err
		}
	} else {
		for i := range config.Steps {
			configured.PlayCount = i
			rows, err := game.PreviewGachaStages(configured, nil)
			if err != nil {
				return nil, err
			}
			current := configured.CurrentStep()
			payment := map[int]string{3: "水晶", 6: "付费水晶"}[current.PayType]
			if current.PayType == 4 {
				payment = catalog[adminCatalogKey(8, current.PayTypeID)].Name
			}
			rows[0].Name = fmt.Sprintf("第%d阶段 · %s × %d", i+1, payment, config.Steps[i].Price)
			stages = append(stages, rows...)
		}
	}
	warning := "混合池奖励与赠礼按原规则保留；各阶段价格和权重为本地运营配置。"
	if editable {
		warning = "奖励身份、数量和权重使用本卡池的独立配置。"
	}
	if len(config.Steps) > 0 {
		warning += "成功抽取后推进阶段，最后阶段重复；重新发布不重置玩家进度。"
	}
	return map[string]any{"config": config, "base": adminConfiguredGacha(base, config).Profile, "odds_scaled": []int{}, "odds_scale": 100000, "rarities": map[int]int{}, "blocked_card_ids": blocked, "publishable": len(blocked) == 0, "warnings": []string{warning}, "stages": stages, "reward_names": names}, nil
}
