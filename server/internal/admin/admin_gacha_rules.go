package admin

import (
	"errors"

	"kairisei.local/server/internal/gamestate"
)

const maxGachaDrawCounts = 2

// Rules belong to the operator pool. Content, payment and publication are edited
// separately in draft/live documents; no built-in gacha identity is needed.
type gachaPoolRule struct {
	Type             string `json:"type"`
	DrawCount        int    `json:"draw_count"`
	MaxDrawCount     int    `json:"max_draw_count,omitempty"`
	DailyFirstFree   bool   `json:"daily_first_free,omitempty"`
	GuaranteedCount  int    `json:"guaranteed_count,omitempty"`
	GuaranteedRarity int    `json:"guaranteed_rarity,omitempty"`
	RemainderRarity  int    `json:"remainder_rarity,omitempty"`
	StepCount        int    `json:"step_count,omitempty"`
	UserSelectMax    int    `json:"user_select_max,omitempty"`
}

type gachaRuleType struct {
	ID                string        `json:"id"`
	Name              string        `json:"name"`
	Summary           []string      `json:"summary"`
	Default           gachaPoolRule `json:"default"`
	ProfessionChoices bool          `json:"profession_choices"`
}

func gachaRuleTypes() []gachaRuleType {
	return []gachaRuleType{
		{ID: "standard", Name: "普通卡牌", Default: gachaPoolRule{Type: "standard", DrawCount: 1}, ProfessionChoices: true,
			Summary: []string{"每次按固定抽数，从所选卡牌中按权重独立抽取，允许重复。", "可同时创建单抽、十连等抽法；每种抽法单独配置卡牌、价格和消耗，可选职业分池。"}},
		{ID: "select", Name: "自选卡牌", Default: gachaPoolRule{Type: "select", DrawCount: 1, UserSelectMax: 2},
			Summary: []string{"玩家从候选中选满指定张数，随后仅从所选卡牌按权重抽取。", "候选不能重复，不与职业分池叠加。"}},
		{ID: "guaranteed", Name: "指定星级保底", Default: gachaPoolRule{Type: "guaranteed", DrawCount: 11, GuaranteedCount: 1, GuaranteedRarity: 6, RemainderRarity: 5}, ProfessionChoices: true,
			Summary: []string{"每次固定抽出指定数量的保底星级卡，其余位置只抽另一指定星级；两组分别按权重抽取。", "这是单次抽取内的星级配额，不累计保底次数。卡池必须同时包含两种指定星级，且不能加入其他星级。"}},
		{ID: "unowned", Name: "未入手六星", Default: gachaPoolRule{Type: "unowned", DrawCount: 1},
			Summary: []string{"固定单抽，仅接受六星卡牌；按图鉴同系排除玩家已获得的卡，再按剩余权重抽取。", "所有候选均已获得时隐藏。不会把已经获得后出售的卡重新视为未入手；不支持职业选择。"}},
		{ID: "friend", Name: "友情点", Default: gachaPoolRule{Type: "friend", DrawCount: 1, MaxDrawCount: 10, DailyFirstFree: true},
			Summary: []string{"以友情点支付，价格为每张卡的费用；连抽数量由余额与连抽上限决定。", "可选每日首次免费单抽，按服务器业务日判定；不支持职业选择。切换为其他消耗会取消免费和变动抽数。"}},
		{ID: "mixed", Name: "混合奖励", Default: gachaPoolRule{Type: "mixed", DrawCount: 1},
			Summary: []string{"按固定次数从奖励表独立抽取，可组合卡牌、道具及受支持的货币等奖励，允许重复。", "创建后自行添加奖励、数量和权重；不叠加星级保底、未入手限制或职业选择。"}},
		{ID: "step", Name: "阶段扭蛋", Default: gachaPoolRule{Type: "step", DrawCount: 11, StepCount: 3},
			Summary: []string{"每阶段使用独立的混合奖励表、价格和消耗方式，抽数保持固定。", "成功抽取后推进一个阶段，失败不推进，最后阶段重复。进度按玩家、分池保存，不会因重新发布而归零。"}},
	}
}

func (r gachaPoolRule) mixed() bool { return r.Type == "mixed" || r.Type == "step" }

func (r gachaPoolRule) validate() error {
	if r.DrawCount < 1 || r.DrawCount > 100 {
		return errors.New("每次抽数须为1–100")
	}
	if r.Type != "friend" && (r.MaxDrawCount != 0 || r.DailyFirstFree) || r.Type != "step" && r.StepCount != 0 ||
		r.Type != "select" && r.UserSelectMax != 0 ||
		r.Type != "guaranteed" && (r.GuaranteedCount != 0 || r.GuaranteedRarity != 0 || r.RemainderRarity != 0) {
		return errors.New("规则包含不属于该类型的参数")
	}
	switch r.Type {
	case "standard", "mixed":
	case "select":
		if r.UserSelectMax < 1 || r.UserSelectMax > 100 {
			return errors.New("玩家自选候选数量须为1–100张")
		}
	case "guaranteed":
		if r.GuaranteedCount < 1 || r.GuaranteedCount >= r.DrawCount || r.GuaranteedRarity < 1 || r.GuaranteedRarity > 7 || r.RemainderRarity < 1 || r.RemainderRarity >= r.GuaranteedRarity {
			return errors.New("保底张数须小于总抽数；保底星级须为2–7且高于其余位置星级")
		}
	case "unowned":
		if r.DrawCount != 1 {
			return errors.New("未入手六星规则仅支持单抽")
		}
	case "friend":
		if r.DrawCount != 1 || r.MaxDrawCount < 1 || r.MaxDrawCount > 100 {
			return errors.New("友情点规则以单抽为基础，连抽上限须为1–100")
		}
	case "step":
		if r.StepCount < 2 || r.StepCount > 10 {
			return errors.New("阶段数量须为2–10")
		}
	default:
		return errors.New("未知扭蛋规则类型")
	}
	return nil
}

// The client wire gacha_type is a list category (0 regular, 1 friend), not
// this rule enum. Mixed, guaranteed and step mechanics all use regular DTOs.
func (r gachaPoolRule) profile() gamestate.GachaProfile {
	p := gamestate.GachaProfile{CardNum: r.DrawCount, CardNumMax: r.DrawCount, PayType: 3, Price: 50 * r.DrawCount,
		CategoryNum: 4, CategoryPictID: 2, BannerKey: "local_standard", ResultPolicySourceState: "LOCAL_POLICY",
		GuaranteedCount: r.GuaranteedCount, GuaranteedRarityRank: r.GuaranteedRarity, RemainderRarityRank: r.RemainderRarity,
		UnownedOnly: r.Type == "unowned", UserSelectMax: r.UserSelectMax}
	if r.Type == "friend" {
		p.GachaType, p.PayType, p.Price, p.CardNumMax = 1, 2, 200, r.MaxDrawCount
		p.CategoryNum, p.CategoryPictID, p.BannerKey = 3, 5, "local_friend"
		p.DailyFirstFree = r.DailyFirstFree
		if p.DailyFirstFree {
			p.DailyFirstFreeSourceState = "LOCAL_POLICY"
		}
	}
	for i := 0; i < r.StepCount; i++ {
		p.Steps = append(p.Steps, gamestate.GachaStep{Price: p.Price, RewardPool: []gamestate.WeightedReward{}})
	}
	return p
}

func copiedGachaRule(p gamestate.GachaProfile) (gachaPoolRule, error) {
	if len(p.RewardPool) != 0 || len(p.Steps) != 0 || p.UnownedOnly || p.UserSelectMax != 0 || p.GachaType > 1 {
		return gachaPoolRule{}, errors.New("只能复制普通卡牌或友情点卡池；其他规则请按类型新建")
	}
	r := gachaPoolRule{Type: "standard", DrawCount: p.CardNum}
	if p.PayType == 2 {
		r.Type, r.MaxDrawCount = "friend", p.CardNumMax
	} else if p.GuaranteedCount > 0 {
		r.Type, r.GuaranteedCount = "guaranteed", p.GuaranteedCount
		r.GuaranteedRarity, r.RemainderRarity = p.GuaranteedRarityRank, p.RemainderRarityRank
	}
	return r, r.validate()
}

// The native picker groups by draw count before Arthur type; two payment
// alternatives for the same draw count would make that choice ambiguous.
func professionChoicesAllowed(profiles []gamestate.GachaProfile) bool {
	seen := map[[2]int]bool{}
	for _, p := range profiles {
		key := [2]int{p.CardNum, p.CardNumMax}
		if p.GachaType != 0 || p.ArthurType != 0 || p.UserSelectMax != 0 || p.CardNum != p.CardNumMax || p.DailyFirstFree || p.UnownedOnly || len(p.RewardPool) != 0 || len(p.Steps) != 0 || seen[key] {
			return false
		}
		seen[key] = true
	}
	return len(profiles) > 0
}

func (o *Operations) filterGachaProfession(p gamestate.GachaProfile) gamestate.GachaProfile {
	if p.ArthurType < 1 || p.ArthurType > 4 {
		return p
	}
	ids, weights := []int{}, []int{}
	fames := map[int]int{}
	for i, id := range p.CardIDs {
		job, known := o.gachaCardJobs[id]
		if known && (job == 0 || job == p.ArthurType) {
			ids, weights = append(ids, id), append(weights, p.CardWeights[i])
			if fame := p.CardFames[id]; fame > 1 {
				fames[id] = fame
			}
		}
	}
	p.CardIDs, p.CardWeights, p.CardFames = ids, weights, fames
	return p
}

func (o *Operations) ownedGachaRule(id int) *gachaPoolRule { return o.customGachas[id].Rule }

func (o *Operations) mixedGacha(id int) bool {
	r := o.ownedGachaRule(id)
	return len(o.gachaBases[id].RewardPool) > 0 || r != nil && r.mixed()
}

func validateCreationRules(rules []gachaPoolRule) error {
	if len(rules) == 0 || len(rules) > maxGachaDrawCounts {
		return errors.New("请选择规则类型，游戏内每个卡池最多显示两种抽数")
	}
	seen := map[int]bool{}
	for _, rule := range rules {
		if err := rule.validate(); err != nil {
			return err
		}
		if rule.Type != rules[0].Type || seen[rule.DrawCount] {
			return errors.New("同一卡池须使用同一规则类型，抽数不能重复")
		}
		seen[rule.DrawCount] = true
	}
	if len(rules) > 1 && rules[0].Type == "step" {
		return errors.New("阶段规则只创建一种抽法，阶段在成功抽取后推进")
	}
	return nil
}
