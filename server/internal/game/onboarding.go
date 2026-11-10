package game

import (
	"errors"
	"fmt"
	"sort"

	"kairisei.local/server/internal/gamestate"
)

type onboardingEvent struct {
	kind         string
	areaID       int
	stageID      int
	gachaID      int
	activityBoss bool
}

type onboardingQuestDefinition struct {
	QuestID       int
	Title         string
	Description   string
	ReceiveText   string
	UnlockFeature uint
	Reward        *gamestate.Reward
	EventKind     string
	GachaID       int
}

func onboardingQuestForStep(step int, arthurType int8) (onboardingQuestDefinition, error) {
	if arthurType < 1 || arthurType > 4 {
		return onboardingQuestDefinition{}, errors.New("CN onboarding Arthur type is invalid")
	}
	itemReward := func(itemID int) *gamestate.Reward {
		return &gamestate.Reward{
			Type: 8, Num: 1, RewardTypeID: itemID, CardSkillLevels: []int16{},
		}
	}
	stackReward := func(cardID int) *gamestate.Reward {
		return &gamestate.Reward{
			Type: 13, Num: 1, RewardTypeID: cardID, CardSkillLevels: []int16{},
		}
	}
	crystalReward := func(amount int) *gamestate.Reward {
		return &gamestate.Reward{
			Type: 10, Num: amount, CardSkillLevels: []int16{},
		}
	}
	switch step {
	case 0:
		return onboardingQuestDefinition{
			QuestID: 1010 + int(arthurType), Title: "最初的一步",
			Description: "通关副本『最初的一步』", ReceiveText: "训练已经完成了！恭喜恭喜！",
			UnlockFeature: 20, Reward: itemReward(2001), EventKind: "battle",
		}, nil
	case 1:
		return onboardingQuestDefinition{
			QuestID: 1020, Title: "超级扭蛋",
			Description: "通过超级扭蛋，获得骑士卡牌", ReceiveText: "获得了新的骑士卡牌！",
			UnlockFeature: 16, Reward: stackReward(20000001), EventKind: "gacha",
			GachaID: gamestate.OnboardingGachaID,
		}, nil
	case 2:
		return onboardingQuestDefinition{
			QuestID: 1030 + int(arthurType), Title: "强化骑士卡牌",
			Description: "通过进行『强化』，提升骑士卡牌的等级", ReceiveText: "是的！训练已经完成了！恭喜恭喜！",
			UnlockFeature: 15, Reward: crystalReward(250), EventKind: "fusion",
		}, nil
	case 3:
		return onboardingQuestDefinition{
			QuestID: 1040 + int(arthurType), Title: "编辑卡组",
			Description: "请把骑士卡牌放入到卡组里，组成卡组", ReceiveText: "从这里开始",
			UnlockFeature: 11, Reward: stackReward(20000011), EventKind: "deck",
		}, nil
	case 4:
		return onboardingQuestDefinition{
			QuestID: 1048, Title: "大地起源",
			Description: "通关『大地起源』中的所有关卡", ReceiveText: "『大地起源』中的所有关卡都已通关！",
			Reward: crystalReward(150), EventKind: "battle_area",
		}, nil
	// LOCAL_POLICY: the retained client establishes the sequence, not the
	// retired service reward amounts. Each remaining step grants 50 crystals.
	case 5:
		return onboardingQuestDefinition{
			QuestID: 1049, Title: "首次十连扭蛋",
			Description: "通过首次十连扭蛋，获得骑士卡牌", ReceiveText: "获得了新的骑士卡牌！",
			Reward: crystalReward(50), EventKind: "gacha", GachaID: gamestate.OnboardingMultiGachaID,
		}, nil
	case 6:
		return onboardingQuestDefinition{
			QuestID: 1045, Title: "探索",
			Description: "通过探索发现宝箱", ReceiveText: "探索训练已经完成了！",
			UnlockFeature: 8, Reward: crystalReward(50), EventKind: "explore",
		}, nil
	case 7:
		return onboardingQuestDefinition{
			QuestID: 1046, Title: "阅读剧情",
			Description: "完成一段主线剧情", ReceiveText: "新的道路已经开启！",
			UnlockFeature: 14, Reward: crystalReward(50), EventKind: "story",
		}, nil
	case 8:
		return onboardingQuestDefinition{
			QuestID: 1047, Title: "挑战活动副本",
			Description: "通关一次活动副本", ReceiveText: "新手训练全部完成！",
			Reward: crystalReward(50), EventKind: "activity",
		}, nil
	default:
		return onboardingQuestDefinition{}, errors.New("CN onboarding step is complete")
	}
}

func (definition onboardingQuestDefinition) receiveWire() map[string]any {
	rewards := []gamestate.Reward{}
	if definition.Reward != nil {
		rewards = append(rewards, cloneReward(*definition.Reward))
	}
	return map[string]any{
		"questid": definition.QuestID,
		"reward":  rewards,
		// The CN 6.0.2 quest guide updates the account unlock mask separately.
		// Its official str_table.csv leaves every 501/1 feature-unlock label empty,
		// so publishing the account bit here makes the stock client render its
		// built-in Japanese placeholder instead of a tutorial reward.
		"feature_flag":    int64(0),
		"title":           definition.Title,
		"description":     definition.Description,
		"receive_comment": []string{definition.ReceiveText},
		"home_banner":     []any{},
	}
}

func (definition onboardingQuestDefinition) clearWire() map[string]any {
	rewards := []any{}
	if definition.Reward != nil {
		rewards = append(rewards, map[string]any{
			"reward":           cloneReward(*definition.Reward),
			"uniqid":           []int64{},
			"is_new":           0,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		})
	}
	return map[string]any{
		"questid":      definition.QuestID,
		"reward":       rewards,
		"feature_flag": int64(0),
		"title":        definition.Title,
		"description":  definition.Description,
		"clear_comment": []string{
			definition.ReceiveText,
		},
	}
}

func emptyOnboardingQuestInfo() map[string]any {
	return map[string]any{
		"new_quest": []any{}, "clear_quest": []any{}, "now_quest": []any{},
	}
}

// homeOnboardingQuests advances only the one-shot Home announcement state.
// Gameplay completion and rewards are committed by advanceOnboardingLocked.
func (s *Account) HomeOnboardingQuests() ([]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := emptyOnboardingQuestInfo()
	if s.onboarding.Step < 0 || s.onboarding.Step > gamestate.OnboardingStepCount {
		return nil, errors.New("CN onboarding state is invalid")
	}
	if s.onboarding.PendingClearQuestID != 0 {
		previousStep := s.onboarding.Step - 1
		definition, err := onboardingQuestForStep(previousStep, s.currentActiveArthur)
		if err != nil || definition.QuestID != s.onboarding.PendingClearQuestID {
			return nil, errors.New("CN onboarding clear quest identity is invalid")
		}
		result["clear_quest"] = []any{definition.clearWire()}
		s.onboarding.PendingClearQuestID = 0
	}
	if s.onboarding.Step == gamestate.OnboardingStepCount {
		return []any{result}, nil
	}
	definition, err := onboardingQuestForStep(s.onboarding.Step, s.currentActiveArthur)
	if err != nil {
		return nil, err
	}
	if s.onboarding.CurrentAnnounced {
		result["now_quest"] = []any{definition.receiveWire()}
	} else {
		result["new_quest"] = []any{definition.receiveWire()}
		s.onboarding.CurrentAnnounced = true
	}
	return []any{result}, nil
}

func (s *Account) OnboardingInProgress() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.onboarding.Step < gamestate.OnboardingStepCount
}

func (s *Account) UnlockedFeatureState() []uint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]uint, 0, len(s.unlockedFeatureIDs))
	for featureID := range s.unlockedFeatureIDs {
		result = append(result, featureID)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func (s *Account) FeatureUnlocked(featureID uint) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, unlocked := s.unlockedFeatureIDs[featureID]
	return unlocked
}

// The local nine-step training replaces the historical follow-up quests.
// Completing it must release their ordinary menu gates as well as Collection.
// Profession selection, deck-rank gates and Tower stay separate.
func (s *Account) reconcileOnboardingFeatureUnlocksLocked() {
	if s.onboarding.Step != gamestate.OnboardingStepCount {
		return
	}
	for featureID := uint(4); featureID <= 31; featureID++ {
		s.unlockedFeatureIDs[featureID] = struct{}{}
	}
}

func (s *Account) advanceOnboardingLocked(event onboardingEvent) error {
	if s.onboarding.Step == gamestate.OnboardingStepCount {
		return nil
	}
	if s.onboarding.Step < 0 || s.onboarding.Step > gamestate.OnboardingStepCount {
		return errors.New("CN onboarding state is invalid")
	}
	definition, err := onboardingQuestForStep(s.onboarding.Step, s.currentActiveArthur)
	if err != nil {
		return err
	}
	matched := definition.EventKind == event.kind
	switch definition.EventKind {
	case "battle":
		matched = matched && event.areaID == 100001 && event.stageID == 10000101
	case "battle_area":
		matched = matched && event.areaID == 100001
	case "gacha":
		matched = matched && event.gachaID == definition.GachaID
	case "activity":
		matched = matched && event.activityBoss
	}
	if !matched {
		return nil
	}
	mail := s.tutorialCompletionMail
	sendMail := s.onboarding.Step == gamestate.OnboardingStepCount-1 && mail.Enabled
	if sendMail {
		for _, reward := range mail.Rewards {
			if err := s.validateRewardLocked(reward); err != nil {
				return fmt.Errorf("validate tutorial completion mail: %w", err)
			}
		}
	}
	if definition.Reward != nil {
		if err := s.validateRewardBatchCapacityLocked([]gamestate.Reward{*definition.Reward}); err != nil {
			return fmt.Errorf("validate CN onboarding reward: %w", err)
		}
		if err := s.applyRewardLocked(*definition.Reward, &PresentReceiveResult{}); err != nil {
			return fmt.Errorf("apply CN onboarding reward: %w", err)
		}
	}
	if definition.UnlockFeature > 0 {
		s.unlockedFeatureIDs[definition.UnlockFeature] = struct{}{}
	}
	s.onboarding.PendingClearQuestID = definition.QuestID
	s.onboarding.Step++
	s.reconcileOnboardingFeatureUnlocksLocked()
	if err := s.completeTrainingBurstLocked(); err != nil {
		return err
	}
	s.onboarding.CurrentAnnounced = false
	if sendMail {
		for _, reward := range mail.Rewards {
			s.appendRewardPresentLocked(reward, mail.Title, mail.Message)
		}
	}
	return nil
}
