package game

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) PresentState() ([]gamestate.Present, []gamestate.Present) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	histories := make([]gamestate.Present, 0, len(s.presentHistories))
	for _, present := range s.presentHistories {
		if present.State != gamestate.PresentStateAdminDeleted {
			histories = append(histories, present)
		}
	}
	return clonePresents(s.presents), clonePresents(histories)
}

func (s *Account) UnreceivedPresentCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for index := range s.presents {
		if s.presents[index].State == 0 {
			count++
		}
	}
	return count
}

type ReceivedReward struct {
	Reward       gamestate.Reward
	UniqueID     []int64
	IsNew        int8
	InPresentBox bool `json:",omitempty"`
}

type PresentReceiveResult struct {
	StampIDs     []int
	InPresentBox bool
	Rewards      []ReceivedReward
	Cards        []CardInfo
	StackCards   []gamestate.CardStack
	Items        []gamestate.Item
	Spheres      []gamestate.Sphere
	Buddies      []gamestate.Buddy
	PresentID    []int64
	FailedID     []int64
}

type LoginBonusClaim struct {
	Day    gamestate.LoginBonusDay
	Result PresentReceiveResult
}

type LoginBonusClaims struct {
	Daily            *LoginBonusClaim
	Beginner         *LoginBonusClaim
	Total            *LoginBonusClaim
	BeginnerSchedule []gamestate.LoginBonusDay
	TotalMilestones  []gamestate.LoginBonusDay
}

func (s *Account) validateLoginBonusLocked() error {
	policy := s.loginBonusPolicy
	state := s.loginBonusState
	if policy.ConfigVersion <= 0 || state.ConfigVersion != policy.ConfigVersion ||
		len(policy.Cycle) == 0 || state.CycleDay < 0 || state.CycleDay > len(policy.Cycle) ||
		len(policy.Beginner) == 0 || state.BeginnerDay < 0 || state.BeginnerDay > len(policy.Beginner) ||
		len(policy.TotalMilestones) == 0 || state.TotalClaims < state.CycleDay ||
		state.TotalClaims < state.BeginnerDay ||
		(state.LastClaimDay == "" && (state.CycleDay != 0 || state.BeginnerDay != 0 || state.TotalClaims != 0)) {
		return errors.New("card store login bonus state is invalid")
	}
	for _, schedule := range []struct {
		Name        string
		Days        []gamestate.LoginBonusDay
		Consecutive bool
	}{
		{Name: "daily", Days: policy.Cycle, Consecutive: true},
		{Name: "beginner", Days: policy.Beginner, Consecutive: true},
		{Name: "total", Days: policy.TotalMilestones},
	} {
		previousDay := 0
		for index, day := range schedule.Days {
			if day.Day <= previousDay || (schedule.Consecutive && day.Day != index+1) ||
				strings.TrimSpace(day.Comment) == "" {
				return fmt.Errorf("card store %s login bonus entry %d is invalid", schedule.Name, index+1)
			}
			if err := s.validateRewardLocked(day.Reward); err != nil {
				return fmt.Errorf("card store %s login bonus entry %d reward: %w", schedule.Name, index+1, err)
			}
			previousDay = day.Day
		}
	}
	return nil
}

func (s *Account) ClaimLoginBonuses(now time.Time) (LoginBonusClaims, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.currentName) == "" {
		return LoginBonusClaims{}, nil
	}
	policy := s.loginBonusPolicy
	if err := s.validateLoginBonusLocked(); err != nil {
		return LoginBonusClaims{}, err
	}
	location := time.FixedZone(
		"CN login bonus",
		policy.DayBoundaryOffsetMinutes*60,
	)
	dayKey := now.In(location).Format("2006-01-02")
	if s.loginBonusState.LastClaimDay != "" && dayKey <= s.loginBonusState.LastClaimDay {
		return LoginBonusClaims{}, nil
	}
	if s.loginBonusState.TotalClaims == math.MaxInt {
		return LoginBonusClaims{}, errors.New("login bonus total claim counter overflow")
	}
	nextDailyDay := s.loginBonusState.CycleDay%len(policy.Cycle) + 1
	nextTotalClaims := s.loginBonusState.TotalClaims + 1
	dailyDay := policy.Cycle[nextDailyDay-1]
	rewards := []gamestate.Reward{dailyDay.Reward}
	var beginnerDay *gamestate.LoginBonusDay
	if s.loginBonusState.BeginnerDay < len(policy.Beginner) {
		day := policy.Beginner[s.loginBonusState.BeginnerDay]
		beginnerDay = &day
		rewards = append(rewards, day.Reward)
	}
	var totalDay *gamestate.LoginBonusDay
	for index := range policy.TotalMilestones {
		if policy.TotalMilestones[index].Day == nextTotalClaims {
			day := policy.TotalMilestones[index]
			totalDay = &day
			rewards = append(rewards, day.Reward)
			break
		}
	}
	if err := s.validateRewardBatchCapacityLocked(rewards); err != nil {
		return LoginBonusClaims{}, err
	}
	claims := LoginBonusClaims{
		BeginnerSchedule: cloneLoginBonusSchedule(policy.Beginner),
		TotalMilestones:  cloneLoginBonusSchedule(policy.TotalMilestones),
	}
	dailyResult := PresentReceiveResult{}
	if err := s.applyRewardLocked(dailyDay.Reward, &dailyResult); err != nil {
		return LoginBonusClaims{}, err
	}
	claims.Daily = &LoginBonusClaim{Day: dailyDay, Result: dailyResult}
	if beginnerDay != nil {
		beginnerResult := PresentReceiveResult{}
		if err := s.applyRewardLocked(beginnerDay.Reward, &beginnerResult); err != nil {
			return LoginBonusClaims{}, err
		}
		claims.Beginner = &LoginBonusClaim{Day: *beginnerDay, Result: beginnerResult}
		s.loginBonusState.BeginnerDay++
	}
	if totalDay != nil {
		totalResult := PresentReceiveResult{}
		if err := s.applyRewardLocked(totalDay.Reward, &totalResult); err != nil {
			return LoginBonusClaims{}, err
		}
		claims.Total = &LoginBonusClaim{Day: *totalDay, Result: totalResult}
	}
	s.loginBonusState.LastClaimDay = dayKey
	s.loginBonusState.CycleDay = nextDailyDay
	s.loginBonusState.TotalClaims = nextTotalClaims
	return claims, nil
}

func (s *Account) ReceivePresent(presentID int64) (PresentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for current := range s.presents {
		if s.presents[current].PresentID == presentID {
			index = current
			break
		}
	}
	if index < 0 || s.presents[index].State != 0 {
		// No pending reward: keep retries distinct from capacity failures.
		return PresentReceiveResult{}, nil
	}
	if err := s.validateRewardLocked(s.presents[index].Reward); err != nil {
		return PresentReceiveResult{}, err
	}
	if err := s.validateRewardBatchCapacityLocked([]gamestate.Reward{s.presents[index].Reward}); err != nil {
		return PresentReceiveResult{FailedID: []int64{presentID}}, nil
	}
	result := PresentReceiveResult{PresentID: []int64{presentID}}
	if err := s.applyRewardLocked(s.presents[index].Reward, &result); err != nil {
		return PresentReceiveResult{}, err
	}
	// Native MovePresent2Received keeps the entry visible until Delete.
	s.presents[index].State = 1
	return result, nil
}

// Original CN Const.PRESENT_BOX_MULTI_RECV_MAX bounds the client confirmation.
const presentMultiReceiveMax = 20

func (s *Account) ReceivePresents(receiveTypes []int, receiveCoin bool) (PresentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	typeSet := make(map[int]struct{}, len(receiveTypes))
	for _, receiveType := range receiveTypes {
		if receiveType < 0 || receiveType > 2 {
			return PresentReceiveResult{}, errors.New("unknown present receive type")
		}
		typeSet[receiveType] = struct{}{}
	}
	selected := make([]int, 0, presentMultiReceiveMax)
	rewards := make([]gamestate.Reward, 0, presentMultiReceiveMax)
	result := PresentReceiveResult{}
	candidates := 0
	// Match PresentMgr.ResortPresentList before its filtered Take(20).
	order := make([]int, len(s.presents))
	for i := range order {
		order[i] = i
	}
	now := time.Now().Unix()
	age := func(p gamestate.Present) uint32 {
		if p.IssuedAtUnix > 0 {
			return uint32(min(max(0, now-p.IssuedAtUnix), math.MaxUint32))
		}
		return p.AddElapsedSec
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := s.presents[order[i]], s.presents[order[j]]
		if a.State != b.State {
			return a.State < b.State
		}
		if age(a) != age(b) {
			return age(a) < age(b)
		}
		return int32(b.PresentID-a.PresentID) < 0
	})
	for _, index := range order {
		present := s.presents[index]
		if _, ok := typeSet[s.presentReceiveTypeLocked(present)]; !ok {
			continue
		}
		if candidates == presentMultiReceiveMax {
			break
		}
		candidates++
		// Received entries stay visible until Delete, but are not failed grants.
		if present.State != 0 {
			continue
		}
		// The client takes twenty filtered entries before asking about crystals.
		if present.Reward.Type == 10 && !receiveCoin {
			continue
		}
		if err := s.validateRewardLocked(present.Reward); err != nil {
			return PresentReceiveResult{}, err
		}
		prospective := append(rewards, present.Reward)
		if err := s.validateRewardBatchCapacityLocked(prospective); err != nil {
			// Leave only this gift in the inbox. The native callback supports
			// successful and failed IDs together, including a fully blocked batch.
			result.FailedID = append(result.FailedID, present.PresentID)
			continue
		}
		selected = append(selected, index)
		rewards = prospective
	}
	for _, index := range selected {
		present := s.presents[index]
		if err := s.applyRewardLocked(present.Reward, &result); err != nil {
			return PresentReceiveResult{}, err
		}
		result.PresentID = append(result.PresentID, present.PresentID)
		s.presents[index].State = 1
	}
	return result, nil
}

func (s *Account) DeletePresents(presentID int64) ([]int64, error) {
	if presentID < 0 {
		return nil, errors.New("invalid present ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	selected := make(map[int]struct{})
	if presentID == 0 {
		for index, present := range s.presents {
			if present.State == 1 && present.Reason != 19 {
				selected[index] = struct{}{}
			}
		}
	} else {
		index := -1
		for current, present := range s.presents {
			if present.PresentID == presentID {
				index = current
				break
			}
		}
		if index < 0 {
			for _, archived := range s.presentHistories {
				if archived.PresentID == presentID {
					// A delayed/retried native Delete must still remove its cached row.
					return []int64{presentID}, nil
				}
			}
			return nil, &BusinessError{-1, "这封礼物已不存在，请重新打开礼物箱。"}
		}
		if s.presents[index].State != 1 {
			return nil, &BusinessError{-1, "请先领取这封礼物，再删除。"}
		}
		if s.presents[index].Reason == 19 {
			return nil, &BusinessError{-1, "这封礼物暂时不能删除。"}
		}
		selected[index] = struct{}{}
	}

	deleted := make([]int64, 0, len(selected))
	remaining := make([]gamestate.Present, 0, len(s.presents)-len(selected))
	for index, present := range s.presents {
		if _, exists := selected[index]; !exists {
			remaining = append(remaining, present)
			continue
		}
		deleted = append(deleted, present.PresentID)
		removed := clonePresent(present)
		removed.State = 1
		s.presentHistories = append(s.presentHistories, removed)
	}
	s.presents = remaining
	s.presentHistories = gamestate.TrimPresentHistories(s.presentHistories)
	return deleted, nil
}

func (s *Account) presentReceiveTypeLocked(present gamestate.Present) int {
	// PresentBoxInfoEx.OnThatDayOnly reads item.csv's limit flag. A gift's
	// own expiry does not make gold, crystals or ordinary items ITEM_TODAY.
	if present.Reward.Type == 8 && s.itemDefinitions[present.Reward.RewardTypeID].DailyLimited != 0 {
		return 1
	}
	if present.Reward.Type == 6 {
		return 0
	}
	return 2
}

func (s *Account) validateRewardLocked(reward gamestate.Reward) error {
	// NAVI_CHARACTER is an unlock, not a counted inventory entry. The native
	// login mission uses Num=0; retain that value in mail and result payloads.
	if reward.Num <= 0 && !(reward.Type == 11 && reward.Num == 0) {
		return errors.New("reward quantity must be positive")
	}
	switch reward.Type {
	case 0:
		if s.playerProgression.ConfigVersion <= 0 {
			return errors.New("player EXP reward requires a progression policy")
		}
		return nil
	case 4, 9, 10, 12:
		if reward.RewardTypeID != 0 {
			return errors.New("scalar reward has an unexpected ID")
		}
		return nil
	case 11:
		if reward.Num > 1 || reward.RewardTypeID < 0 || reward.RewardTypeID > math.MaxInt8 {
			return errors.New("navigator reward must unlock one catalog identity")
		}
		if _, exists := s.naviCatalogIDs[int8(reward.RewardTypeID)]; !exists {
			return errors.New("reward references an unknown navigator")
		}
		return nil
	case 6:
		definition, ok := s.cardDefinitions[reward.RewardTypeID]
		if !ok {
			return errors.New("reward references an unknown card")
		}
		level := int(reward.CardLevel)
		if level == 0 {
			level = 1
		}
		fame := int(reward.CardFame)
		if fame == 0 {
			fame = 1
		}
		if level < 1 || level > definition.LevelMax || fame < 1 || fame > definition.FameMax ||
			reward.CardLove < 0 || reward.CardLove > definition.LoveMax || len(reward.CardSkillLevels) == 0 {
			return errors.New("card reward progression fields are invalid")
		}
		return nil
	case 8:
		if _, exists := s.itemDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("reward references an unknown item")
		}
		return nil
	case 13:
		if _, exists := s.stackCardTemplates[reward.RewardTypeID]; exists {
			return nil
		}
		return errors.New("reward references an unknown stack card")
	case 15:
		if _, exists := s.sphereDefinitions[reward.RewardTypeID]; !exists {
			return errors.New("reward references an unknown sphere")
		}
		return nil
	case 19:
		definition, exists := s.buddyDefinitions[reward.RewardTypeID]
		if !exists {
			return errors.New("reward references an unknown buddy")
		}
		if reward.CardLevel < 0 || int(reward.CardLevel) > definition.MaxLevel ||
			reward.CardFame != 0 || reward.CardLove != 0 || len(reward.CardSkillLevels) != 0 {
			return errors.New("buddy reward progression fields are invalid")
		}
		return nil
	case 14, 16, 18:
		if _, exists := s.collectionRewardIDs[[2]int{reward.Type, reward.RewardTypeID}]; !exists || reward.Num != 1 {
			return errors.New("collection reward must be one known skin, stamp or honor")
		}
		return nil
	default:
		return fmt.Errorf("unsupported local reward type %d", reward.Type)
	}
}

func (s *Account) validateRewardBatchCapacityLocked(rewards []gamestate.Reward) error {
	gold := s.gold
	friendPoint := s.friendPoint
	coinFree := s.coinFree
	cardCount := len(s.cards)
	sphereCount := len(s.spheres)
	buddyCount := len(s.buddies)
	nextCardUniqueID := s.nextUniqueID
	nextSphereUniqueID := s.nextSphereUniqueID
	nextBuddyUniqueID := s.nextBuddyUniqueID
	itemCounts := make(map[int]int)
	stackCounts := make(map[int]int)
	for itemID, item := range s.items {
		itemCounts[itemID] = item.Num
	}
	for _, stack := range s.stackCards {
		stackCounts[stack.CardID] = stack.Num
	}
	for _, reward := range rewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return err
		}
		switch reward.Type {
		case 4:
			if reward.Num > math.MaxInt-gold {
				return errors.New("gold reward overflows")
			}
			gold += reward.Num
		case 9:
			if reward.Num > math.MaxInt-friendPoint {
				return errors.New("friend-point reward overflows")
			}
			friendPoint += reward.Num
		case 6:
			if reward.Num > s.cardMax-cardCount || int64(reward.Num) > math.MaxInt64-nextCardUniqueID {
				return ErrCardCapacity
			}
			cardCount += reward.Num
			nextCardUniqueID += int64(reward.Num)
		case 8:
			definition := s.itemDefinitions[reward.RewardTypeID]
			owned := itemCounts[reward.RewardTypeID]
			if reward.Num > definition.MaxOwned-owned {
				return errItemCapacity
			}
			itemCounts[reward.RewardTypeID] = owned + reward.Num
		case 10:
			if reward.Num > math.MaxInt-coinFree {
				return errors.New("crystal reward overflows")
			}
			coinFree += reward.Num
		case 13:
			owned := stackCounts[reward.RewardTypeID]
			if reward.Num > math.MaxInt-owned {
				return errors.New("stack-card reward overflows")
			}
			stackCounts[reward.RewardTypeID] = owned + reward.Num
		case 15:
			if reward.Num > s.sphereMax-sphereCount || int64(reward.Num) > math.MaxInt64-nextSphereUniqueID {
				return ErrSphereCapacity
			}
			sphereCount += reward.Num
			nextSphereUniqueID += int64(reward.Num)
		case 19:
			if reward.Num > s.buddyMax-buddyCount || int64(reward.Num) > math.MaxInt64-nextBuddyUniqueID {
				return ErrBuddyCapacity
			}
			buddyCount += reward.Num
			nextBuddyUniqueID += int64(reward.Num)
		}
	}
	return nil
}

func (s *Account) validateEngagementLocked() error {
	if err := s.validateMissionsLocked(); err != nil {
		return err
	}
	presentIDs := make(map[int64]struct{}, len(s.presents)+len(s.presentHistories))
	for _, group := range [][]gamestate.Present{s.presents, s.presentHistories} {
		for _, present := range group {
			if present.PresentID <= 0 || present.Title == "" {
				return errors.New("present configuration is incomplete")
			}
			if _, duplicate := presentIDs[present.PresentID]; duplicate {
				return errors.New("active and received present IDs must be unique")
			}
			presentIDs[present.PresentID] = struct{}{}
		}
	}
	for _, present := range s.presents {
		if err := s.validateRewardLocked(present.Reward); err != nil {
			return err
		}
	}
	return nil
}

func (s *Account) applyRewardLocked(reward gamestate.Reward, result *PresentReceiveResult) error {
	received := ReceivedReward{Reward: cloneReward(reward), UniqueID: []int64{}}
	switch reward.Type {
	case 11:
		s.unlockNaviLocked(int8(reward.RewardTypeID))
	case 14, 16, 18:
		owned := s.ownsCollectionRewardLocked(reward)
		switch reward.Type {
		case 14:
			s.costumeIDs[reward.RewardTypeID] = struct{}{}
		case 16:
			s.stampIDs[reward.RewardTypeID] = struct{}{}
			if !owned {
				result.StampIDs = append(result.StampIDs, reward.RewardTypeID)
			}
		case 18:
			s.honorIDs[reward.RewardTypeID] = struct{}{}
		}
	case 0:
		s.applyPlayerExperienceLocked(reward.Num)
	case 4:
		s.gold += reward.Num
	case 9:
		s.friendPoint += reward.Num
	case 6:
		owned := s.hasCollectedCardLocked(reward.RewardTypeID)
		if !owned {
			received.IsNew = 1
		}
		prepared, err := s.prepareRewardCardsLocked(reward, s.nextUniqueID, int(time.Now().Unix()))
		if err != nil {
			return err
		}
		for _, card := range prepared {
			received.UniqueID = append(received.UniqueID, card.UniqueID)
		}
		s.nextUniqueID += int64(len(prepared))
		s.cards = append(s.cards, prepared...)
		result.Cards = append(result.Cards, prepared...)
		for _, card := range prepared {
			s.recordCollectedCardLocked(card)
		}
	case 8:
		item := s.items[reward.RewardTypeID]
		item.ItemID = reward.RewardTypeID
		item.Num += reward.Num
		s.items[item.ItemID] = item
		result.Items = append(result.Items, item)
	case 10:
		s.coinFree += reward.Num
	case 12:
		if s.bp >= s.bpMax {
			// Capped reward recovery must not discard overflow from a potion.
		} else if reward.Num >= s.bpMax-s.bp {
			s.bp = s.bpMax
		} else {
			s.bp += reward.Num
		}
		if s.bp >= s.bpMax {
			s.bpNextRecovery = time.Time{}
		}
	case 13:
		owned := false
		for index := range s.stackCards {
			if s.stackCards[index].CardID != reward.RewardTypeID {
				continue
			}
			s.stackCards[index].Num += reward.Num
			owned = true
			break
		}
		delta := s.stackCardTemplates[reward.RewardTypeID]
		delta.Num = reward.Num
		result.StackCards = append(result.StackCards, delta)
		s.recordCollectedCardLocked(CardInfo{CardID: reward.RewardTypeID})
		if !owned {
			s.stackCards = append(s.stackCards, delta)
			// CN 6.0.2 consumes type-13 rewards through new_stack_cards. Marking
			// the result row as new instead routes the stack material through the
			// normal-card detail animation, where CardInfoWindow dereferences a
			// CardInfo shape that stack materials do not provide. Keep the local
			// inventory delta and suppress the normal-card UI flag.
		}
	case 15:
		isNew := true
		for _, sphere := range s.spheres {
			if sphere.SphereID == reward.RewardTypeID {
				isNew = false
				break
			}
		}
		if isNew {
			received.IsNew = 1
		}
		definition := s.sphereDefinitions[reward.RewardTypeID]
		prepared := make([]gamestate.Sphere, reward.Num)
		now := int(time.Now().Unix())
		for count := 0; count < reward.Num; count++ {
			sphere, err := s.normalizeSphereLocked(gamestate.Sphere{
				UniqueID: s.nextSphereUniqueID + int64(count), SphereID: reward.RewardTypeID,
				Level: 1, CreateTime: now,
			}, definition)
			if err != nil {
				return err
			}
			prepared[count] = sphere
			received.UniqueID = append(received.UniqueID, sphere.UniqueID)
		}
		s.nextSphereUniqueID += int64(len(prepared))
		s.spheres = append(s.spheres, prepared...)
		result.Spheres = append(result.Spheres, prepared...)
	case 19:
		isNew := true
		for _, buddy := range s.buddies {
			if buddy.BuddyID == reward.RewardTypeID {
				isNew = false
				break
			}
		}
		if isNew {
			received.IsNew = 1
		}
		definition := s.buddyDefinitions[reward.RewardTypeID]
		level := max(1, int(reward.CardLevel))
		experience, err := s.buddyExperienceAtLevelLocked(definition, level)
		if err != nil {
			return err
		}
		prepared := make([]gamestate.Buddy, reward.Num)
		now := int(time.Now().Unix())
		for count := 0; count < reward.Num; count++ {
			buddy, err := s.normalizeBuddyLocked(gamestate.Buddy{
				UniqueID: s.nextBuddyUniqueID + int64(count), BuddyID: reward.RewardTypeID,
				Level: level, Experience: experience, CreateTime: now,
			}, definition)
			if err != nil {
				return err
			}
			prepared[count] = buddy
			received.UniqueID = append(received.UniqueID, buddy.UniqueID)
		}
		s.nextBuddyUniqueID += int64(len(prepared))
		s.buddies = append(s.buddies, prepared...)
		result.Buddies = append(result.Buddies, prepared...)
	default:
		return fmt.Errorf("unsupported local reward type %d", reward.Type)
	}
	result.Rewards = append(result.Rewards, received)
	return nil
}

func clonePresent(present gamestate.Present) gamestate.Present {
	present.Reward = cloneReward(present.Reward)
	present.Reward0 = cloneReward(present.Reward0)
	present.Reward1 = cloneReward(present.Reward1)
	present.Reward2 = cloneReward(present.Reward2)
	return present
}

func clonePresents(presents []gamestate.Present) []gamestate.Present {
	result := make([]gamestate.Present, len(presents))
	for index, present := range presents {
		result[index] = clonePresent(present)
	}
	return result
}
