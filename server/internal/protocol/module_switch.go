package protocol

import "slices"

// ModuleSwitch is the CN 6.0.2 MODULE_SWITCH bit position contract. Values come
// from Assembly-CSharp's enum; they are not server feature IDs or ordinal iota.
type ModuleSwitch int64

const (
	ModuleEverydayTask    ModuleSwitch = 1 << 2
	ModuleGiftCode        ModuleSwitch = 1 << 7
	ModuleActivity        ModuleSwitch = 1 << 3
	ModuleBossCoin        ModuleSwitch = 1 << 5
	ModuleMonthCardTip    ModuleSwitch = 1 << 18
	ModuleRecommendDeck   ModuleSwitch = 1 << 4
	ModuleStrategyButton  ModuleSwitch = 1 << 22
	ModulePVP             ModuleSwitch = 1 << 28
	ModuleFailureAdvise   ModuleSwitch = 1 << 29
	ModulePastBoss        ModuleSwitch = 1 << 30
	ModuleCopyCard        ModuleSwitch = 1 << 31
	ModuleMultiChain      ModuleSwitch = 1 << 32
	ModuleChatDefine      ModuleSwitch = 1 << 33
	ModuleChangeCloth     ModuleSwitch = 1 << 34
	ModuleChangeModel     ModuleSwitch = 1 << 35
	ModuleReturnToDungeon ModuleSwitch = 1 << 37
)

type ModuleSwitchDefinition struct {
	Key   string
	Label string
	Bit   ModuleSwitch
}

// These are the switches already open in the shipped CN profile.
var operatorModuleSwitches = [...]ModuleSwitchDefinition{
	{"everyday_task", "每日任务入口", ModuleEverydayTask},
	{"activity", "活动公告入口", ModuleActivity},
	{"boss_coin", "副本货币入口", ModuleBossCoin},
	{"gift_code", "礼包码入口", ModuleGiftCode},
	{"month_card_tip", "月卡提示", ModuleMonthCardTip},
	{"pvp", "竞技场入口", ModulePVP},
	{"failure_advise", "战败建议", ModuleFailureAdvise},
	{"past_boss", "往期 BOSS 入口", ModulePastBoss},
	{"copy_card", "卡牌培养入口", ModuleCopyCard},
	{"multi_chain", "战斗连携数提示", ModuleMultiChain},
	{"chat_define", "自定义聊天", ModuleChatDefine},
	{"change_cloth", "换装入口", ModuleChangeCloth},
	{"change_model", "模型切换入口", ModuleChangeModel},
	{"return_to_dungeon", "结算后返回副本", ModuleReturnToDungeon},
}

func OperatorModuleSwitches() []ModuleSwitchDefinition {
	return slices.Clone(operatorModuleSwitches[:])
}

var defaultModuleSwitches = func() ModuleSwitch {
	var mask ModuleSwitch
	for _, module := range operatorModuleSwitches {
		mask |= module.Bit
	}
	return mask
}()

func DefaultModuleSwitches() ModuleSwitch { return defaultModuleSwitches }
