package game

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) GachaState() []gamestate.GachaProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VisibleGachasLocked()
}

// homeBannerGachaID selects a crystal multi-draw profile because those rows
// remain visible whether or not the account owns a single-draw ticket. The CN
// client resolves the exact gacha ID to its group before displaying the scene.
func (s *Account) HomeBannerGachaID(now time.Time) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	selected := gamestate.GachaProfile{}
	groupPlays := s.gachaGroupPlaysLocked()
	dailyPlays := s.gachaDailyGroupPlaysLocked(gachaLocalDayKey(now))
	for _, gacha := range s.gachas {
		gacha.GroupPlayCount = groupPlays[gacha.GroupID]
		gacha.DailyGroupPlays = dailyPlays[gacha.GroupID]
		gacha = gacha.CurrentStep()
		if gacha.Exhausted() || gacha.DailyExhausted() || !s.gachaScheduledLocked(gacha.GachaID) || gacha.GachaType != 0 || gacha.PayType != 3 ||
			gacha.CardNum <= 1 || gacha.CardNumMax <= 1 ||
			int64(gacha.EndTime) <= now.Unix() {
			continue
		}
		if selected.GachaID == 0 ||
			gacha.CategoryNum < selected.CategoryNum ||
			(gacha.CategoryNum == selected.CategoryNum && gacha.OrderNum < selected.OrderNum) ||
			(gacha.CategoryNum == selected.CategoryNum && gacha.OrderNum == selected.OrderNum &&
				gacha.GachaID < selected.GachaID) {
			selected = gacha
		}
	}
	return selected.GachaID
}

func (s *Account) GachaStateWithOwnership() ([]gamestate.GachaProfile, map[int]struct{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.VisibleGachasLocked(), s.collectedCardIDsLocked()
}

func (s *Account) VisibleGachasLocked() []gamestate.GachaProfile {
	// The original guide selects the first category. The local client's selector
	// keeps a sole gacha category ahead of exchange, so no normal pool is needed
	// to make the reserved tutorial pool the landing page.
	tutorialGachaID := s.onboardingVisibleGachaForStepLocked()
	if tutorialGachaID != 0 {
		for _, gacha := range s.gachas {
			if gacha.GachaID == tutorialGachaID && gacha.PlayCount == 0 {
				return CloneGachaProfiles([]gamestate.GachaProfile{gacha})
			}
		}
		return []gamestate.GachaProfile{}
	}

	groupPlays := s.gachaGroupPlaysLocked()
	candidates := make([]gamestate.GachaProfile, 0, len(s.gachas))
	for _, gacha := range s.gachas {
		gacha.GroupPlayCount = groupPlays[gacha.GroupID]
		if gamestate.IsOnboardingGachaID(gacha.GachaID) || !s.gachaScheduledLocked(gacha.GachaID) || gacha.Exhausted() {
			continue
		}
		gacha = s.currentGachaLocked(gacha)
		if !gacha.UnownedOnly || len(gacha.CardIDs) > 0 {
			candidates = append(candidates, gacha)
		}
	}
	// The native list has one single-draw button per profession. Resolve its
	// payment without hiding another profession or an affordable alternative.
	key := func(g gamestate.GachaProfile) [2]int { return [2]int{g.GroupID, int(g.ArthurType)} }
	type singleChoice struct{ id, rank int }
	preferredSingles := make(map[[2]int]singleChoice)
	now := time.Now()
	dayKey := gachaLocalDayKey(now)
	dailyPlays := s.gachaDailyGroupPlaysLocked(dayKey)
	for _, gacha := range candidates {
		if gacha.CardNumMax != 1 {
			continue
		}
		group := key(gacha)
		gacha.DailyGroupPlays = dailyPlays[gacha.GroupID]
		rank := s.gachaSinglePaymentRankLocked(gacha, now.Unix(), dayKey)
		if current, exists := preferredSingles[group]; !exists || rank < current.rank {
			preferredSingles[group] = singleChoice{id: gacha.GachaID, rank: rank}
		}
	}

	visible := make([]gamestate.GachaProfile, 0, len(s.gachas))
	groupOrder := make(map[int]int)
	for _, gacha := range candidates {
		if _, exists := groupOrder[gacha.GroupID]; !exists {
			groupOrder[gacha.GroupID] = len(groupOrder)
		}
		if gacha.CardNumMax == 1 && preferredSingles[key(gacha)].id != gacha.GachaID {
			continue
		}
		visible = append(visible, gacha)
	}
	// The native list reads the first two rows and shows both only when their
	// draw counts increase. A fallback single may appear after the multi-draw.
	sort.SliceStable(visible, func(i, j int) bool {
		left, right := visible[i], visible[j]
		if left.GroupID != right.GroupID {
			return groupOrder[left.GroupID] < groupOrder[right.GroupID]
		}
		return left.CardNum < right.CardNum
	})
	result := CloneGachaProfiles(visible)
	for index := range result {
		gacha := &result[index]
		gacha.DailyGroupPlays = dailyPlays[gacha.GroupID]
		claim := s.gachaDailyClaims[gacha.GachaID]
		gacha.DailyFirstAvailable = gacha.DailyFirstFree && !gacha.DailyExhausted() && (claim.Day != dayKey || !claim.FreeClaimed)
	}
	return result
}

func (s *Account) gachaSinglePaymentRankLocked(gacha gamestate.GachaProfile, now int64, dayKey string) int {
	claim := s.gachaDailyClaims[gacha.GachaID]
	if gacha.DailyFirstFree && !gacha.DailyExhausted() && (claim.Day != dayKey || !claim.FreeClaimed) {
		return 0
	}
	// Keep a fallback visible for the client's insufficient-currency dialog.
	switch gacha.PayType {
	case 4:
		item := s.items[gacha.PayTypeID]
		if item.Num >= gacha.Price && (item.LimitTime <= 0 || now < int64(item.LimitTime)) {
			return 1
		}
		return 8
	case 2:
		if s.friendPoint >= gacha.Price {
			return 2
		}
		return 5
	case 3:
		if int64(s.coin)+int64(s.coinFree) >= int64(gacha.Price) {
			return 3
		}
		return 6
	case 6:
		if s.coin >= gacha.Price {
			return 4
		}
		return 7
	default:
		return 9
	}
}

// onboardingVisibleGachaForStepLocked follows the stock client's two reserved
// first-gacha families.  The single-draw profile is replaced by the multi-draw
// profile immediately after the fixed single draw; the multi remains visible
// while fusion, deck editing, and the first area are still guiding the player.
// Playback is gated separately by gachaAvailableForPlayLocked.
func (s *Account) onboardingVisibleGachaForStepLocked() int {
	switch s.onboarding.Step {
	case 1:
		return gamestate.OnboardingGachaID
	case 2, 3, 4, 5:
		return gamestate.OnboardingMultiGachaID
	default:
		return 0
	}
}

func (s *Account) GachaAvailableForPlayLocked(gachaID int) bool {
	profile := findGachaProfile(s.gachas, gachaID)
	if profile == nil || (profile.PlayCountMax > 0 && s.gachaGroupPlaysLocked()[profile.GroupID] >= profile.PlayCountMax) {
		return false
	}
	if !s.gachaScheduledLocked(gachaID) {
		return false
	}
	switch s.onboarding.Step {
	case 0, 2, 3, 4:
		return false
	case 1:
		return gachaID == gamestate.OnboardingGachaID
	case 5:
		return gachaID == gamestate.OnboardingMultiGachaID
	}
	return !gamestate.IsOnboardingGachaID(gachaID)
}

// Count successful requests, never the number of cards drawn. Existing per-ID
// play counters remain authoritative for persistence and independent step rules.
func (s *Account) gachaGroupPlaysLocked() map[int]int {
	result := make(map[int]int)
	for _, profile := range s.gachas {
		count := max(0, profile.PlayCount)
		if count > math.MaxInt-result[profile.GroupID] {
			result[profile.GroupID] = math.MaxInt
		} else {
			result[profile.GroupID] += count
		}
	}
	return result
}

func (s *Account) gachaDailyGroupPlaysLocked(day string) map[int]int {
	counts := make(map[int]int)
	for _, profile := range s.gachas {
		if claim := s.gachaDailyClaims[profile.GachaID]; claim.Day == day {
			counts[profile.GroupID] = int(min(int64(math.MaxInt32), int64(counts[profile.GroupID])+int64(claim.PlayCount)))
		}
	}
	return counts
}

var gachaBusinessZone = time.FixedZone("CN local service day", 8*60*60)

func gachaLocalDayKey(now time.Time) string {
	return now.In(gachaBusinessZone).Format("2006-01-02")
}

type GachaLineupEntry struct {
	Prize gamestate.Reward
	IsNew int8
}

func (s *Account) GachaSelectLineup(gachaID int) ([]GachaLineupEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile := findGachaProfile(s.gachas, gachaID)
	if profile == nil {
		return nil, ErrGachaUnavailable
	}
	if profile.UserSelectMax <= 0 {
		return nil, errors.New("gacha has no user-select lineup")
	}
	owned := s.collectedCardIDsLocked()
	result := make([]GachaLineupEntry, len(profile.CardIDs))
	for index, cardID := range profile.CardIDs {
		result[index] = GachaLineupEntry{
			Prize: GachaCardReward(cardID),
			IsNew: CardNewFlag(owned, cardID),
		}
	}
	return result, nil
}

func (s *Account) GachaSelectedLineup(gachaID int) ([]GachaLineupEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile := findGachaProfile(s.gachas, gachaID)
	if profile == nil {
		return nil, ErrGachaUnavailable
	}
	if profile.UserSelectMax <= 0 {
		return nil, errors.New("gacha has no user-select lineup")
	}
	selected := s.gachaSelections[gachaID]
	owned := s.collectedCardIDsLocked()
	result := make([]GachaLineupEntry, len(selected))
	for index, reward := range selected {
		result[index] = GachaLineupEntry{
			Prize: cloneReward(reward),
			IsNew: CardNewFlag(owned, reward.RewardTypeID),
		}
	}
	return result, nil
}

func (s *Account) collectedCardIDsLocked() map[int]struct{} {
	owned := make(map[int]struct{}, len(s.cardCollectionIDs)+len(s.cards)+len(s.containerCards))
	for id := range s.cardCollectionIDs {
		owned[id] = struct{}{}
	}
	for _, inventory := range [][]CardInfo{s.cards, s.containerCards} {
		for _, card := range inventory {
			owned[card.CardID] = struct{}{}
		}
	}
	return owned
}

func (s *Account) hasCollectedCardLocked(id int) bool {
	if _, ok := s.cardCollectionIDs[id]; ok {
		return true
	}
	for _, inventory := range [][]CardInfo{s.cards, s.containerCards} {
		for _, card := range inventory {
			if card.CardID == id {
				return true
			}
		}
	}
	return false
}

func findGachaProfile(gachas []gamestate.GachaProfile, gachaID int) *gamestate.GachaProfile {
	for index := range gachas {
		if gachas[index].GachaID == gachaID {
			return &gachas[index]
		}
	}
	return nil
}

func GachaCardReward(cardID int) gamestate.Reward {
	return gamestate.Reward{
		Type: 6, Num: 1, RewardTypeID: cardID,
		CardLevel: 1, CardFame: 1, CardSkillLevels: []int16{1},
	}
}

func validateGachaSelection(gacha gamestate.GachaProfile, rewards []gamestate.Reward) error {
	if gacha.UserSelectMax < 0 || gacha.UserSelectMax > len(gacha.CardIDs) {
		return errors.New("gacha user-select configuration is invalid")
	}
	if len(rewards) != gacha.UserSelectMax {
		return errors.New("gacha user-select lineup has an invalid length")
	}
	if len(rewards) == 0 {
		return nil
	}
	allowed := make(map[int]struct{}, len(gacha.CardIDs))
	for _, cardID := range gacha.CardIDs {
		allowed[cardID] = struct{}{}
	}
	seen := make(map[int]struct{}, len(rewards))
	for _, reward := range rewards {
		if reward.Type != 6 || reward.Num != 1 || reward.CardLevel != 1 ||
			reward.CardFame != 1 || reward.CardLove != 0 ||
			len(reward.CardSkillLevels) != 1 || reward.CardSkillLevels[0] != 1 {
			return errors.New("gacha user-select lineup contains an invalid reward")
		}
		if _, exists := allowed[reward.RewardTypeID]; !exists {
			return errors.New("gacha user-select lineup contains an unavailable card")
		}
		if _, duplicate := seen[reward.RewardTypeID]; duplicate {
			return errors.New("gacha user-select lineup repeats a card")
		}
		seen[reward.RewardTypeID] = struct{}{}
	}
	return nil
}

func cloneRewards(source []gamestate.Reward) []gamestate.Reward {
	result := make([]gamestate.Reward, len(source))
	for index, reward := range source {
		result[index] = cloneReward(reward)
	}
	return result
}

type gachaPlayResult struct {
	Gachas       []gamestate.GachaProfile
	Item         gamestate.Item
	Reward       PresentReceiveResult
	Expectancy   int
	PresentGifts []gamestate.Reward
}

// CN GACHA_EXPECTANCY is zero-based (RARE=2, SUPERRARE=3, ULTRARARE=4),
// whereas the official card master projects rarity_rank as 1..8. The whole
// draw uses its highest actual result, not the pool's maximum or an evolved card.
func gachaResultExpectancy(rewards []gamestate.Reward, definitions map[int]gamestate.Card) int {
	maxRank := 1
	for _, reward := range rewards {
		if reward.Type == 6 && reward.Num > 0 {
			maxRank = max(maxRank, definitions[reward.RewardTypeID].RarityRank)
		}
	}
	return min(maxRank, 8) - 1
}

type itemGachaPlayResult struct {
	Item   gamestate.Item
	Reward PresentReceiveResult
}

func (s *Account) PlayItemGacha(itemID int, playCount int) (itemGachaPlayResult, error) {
	if itemID <= 0 || playCount < 1 || playCount > 10 {
		return itemGachaPlayResult{}, errors.New("invalid item gacha selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	item, owned := s.items[itemID]
	if !owned || item.Num < playCount {
		return itemGachaPlayResult{}, ErrInsufficientMaterials
	}
	if item.LimitTime > 0 && time.Now().Unix() >= int64(item.LimitTime) {
		return itemGachaPlayResult{}, ErrItemExpired
	}
	definition, exists := s.itemDefinitions[itemID]
	if !exists || definition.ItemType != "GACHA" || definition.Function != "GACHA_EXEC" {
		return itemGachaPlayResult{}, errors.New("item cannot execute a gacha")
	}
	profile, exists := s.itemGachaProfiles[itemID]
	if !exists || profile.FunctionValue != definition.FunctionValue {
		return itemGachaPlayResult{}, errors.New("item gacha pool unavailable from official local data")
	}
	rewards := make([]gamestate.Reward, 0, len(profile.Rewards)*playCount)
	for play := 0; play < playCount; play++ {
		rewards = append(rewards, profile.Rewards...)
		if len(profile.RewardPool) > 0 {
			reward, err := drawWeightedReward(profile.RewardPool, 0)
			if err != nil {
				return itemGachaPlayResult{}, err
			}
			rewards = append(rewards, reward)
		}
	}
	if err := s.validateSettlementRewardsLocked(rewards); err != nil {
		return itemGachaPlayResult{}, err
	}

	item.Num -= playCount
	s.items[itemID] = item
	result := PresentReceiveResult{}
	for _, reward := range rewards {
		if err := s.applyRewardOrPresentLocked(reward, &result, "扭蛋奖励"); err != nil {
			return itemGachaPlayResult{}, err
		}
	}
	return itemGachaPlayResult{Item: s.items[itemID], Reward: result}, nil
}

func (s *Account) PlayGacha(
	gachaID int,
	payType int,
	selectedRewards []gamestate.Reward,
) (gachaPlayResult, error) {
	if gachaID <= 0 {
		return gachaPlayResult{}, errors.New("invalid gacha ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for current := range s.gachas {
		if s.gachas[current].GachaID == gachaID {
			index = current
			break
		}
	}
	if index < 0 {
		return gachaPlayResult{}, ErrGachaUnavailable
	}
	profile := s.currentGachaLocked(s.gachas[index])
	profile.GroupPlayCount = s.gachaGroupPlaysLocked()[profile.GroupID]
	if payType != profile.PayType || profile.Exhausted() {
		return gachaPlayResult{}, ErrGachaUnavailable
	}
	gacha := &profile
	if gacha.PlayCount == math.MaxInt || (gacha.UnownedOnly && len(gacha.CardIDs) == 0) {
		return gachaPlayResult{}, ErrGachaUnavailable
	}
	if !s.GachaAvailableForPlayLocked(gachaID) ||
		(gamestate.IsOnboardingGachaID(gachaID) && gacha.PlayCount != 0) {
		return gachaPlayResult{}, ErrGachaUnavailable
	}
	dayKey := gachaLocalDayKey(time.Now())
	if gacha.DailyPlayMax > 0 {
		gacha.DailyGroupPlays = s.gachaDailyGroupPlaysLocked(dayKey)[gacha.GroupID]
		if gacha.DailyExhausted() {
			return gachaPlayResult{}, errGachaDailyLimit
		}
	}
	if err := validateGachaSelection(*gacha, selectedRewards); err != nil {
		return gachaPlayResult{}, err
	}
	claim := s.gachaDailyClaims[gachaID]
	if claim.Day != dayKey {
		claim = gamestate.GachaDailyClaim{GachaID: gachaID, Day: dayKey}
	}
	dailyFree := gacha.DailyFirstFree && !claim.FreeClaimed
	drawCount := gacha.CardNum
	paymentCost := gacha.Price
	if dailyFree {
		paymentCost = 0
	} else if gacha.PayType == 2 && gacha.CardNumMax > gacha.CardNum {
		drawCount = min(gacha.CardNumMax, s.friendPoint/gacha.Price)
		if drawCount < gacha.CardNum {
			return gachaPlayResult{}, errInsufficientFriendPoints
		}
		if gacha.Price > math.MaxInt/drawCount {
			return gachaPlayResult{}, errors.New("friend-point gacha cost overflows")
		}
		paymentCost = gacha.Price * drawCount
	}
	paidItem := gamestate.Item{}
	switch gacha.PayType {
	case 2:
		if s.friendPoint < paymentCost {
			return gachaPlayResult{}, errInsufficientFriendPoints
		}
	case 3:
		if int64(s.coin)+int64(s.coinFree) < int64(paymentCost) {
			return gachaPlayResult{}, ErrInsufficientCrystals
		}
	case 4:
		paidItem = s.items[gacha.PayTypeID]
		if paidItem.Num < paymentCost || (paidItem.LimitTime > 0 && time.Now().Unix() >= int64(paidItem.LimitTime)) {
			return gachaPlayResult{}, ErrInsufficientMaterials
		}
	case 6:
		if s.coin < paymentCost {
			return gachaPlayResult{}, errInsufficientPaidCrystals
		}
	default:
		return gachaPlayResult{}, errors.New("unsupported gacha payment type")
	}

	drawCardIDs := gacha.CardIDs
	drawCardWeights := gacha.CardWeights
	if len(selectedRewards) > 0 {
		drawCardIDs = make([]int, len(selectedRewards))
		drawCardWeights = make([]int, len(selectedRewards))
		weightsByCardID := make(map[int]int, len(gacha.CardIDs))
		for cardIndex, cardID := range gacha.CardIDs {
			weightsByCardID[cardID] = gacha.CardWeights[cardIndex]
		}
		for selectedIndex, reward := range selectedRewards {
			drawCardIDs[selectedIndex] = reward.RewardTypeID
			drawCardWeights[selectedIndex] = weightsByCardID[reward.RewardTypeID]
		}
	}
	guaranteedCardIDs := []int(nil)
	guaranteedCardWeights := []int(nil)
	remainderCardIDs := drawCardIDs
	remainderCardWeights := drawCardWeights
	if gacha.GuaranteedCount > 0 {
		if len(selectedRewards) != 0 || gacha.GuaranteedCount >= drawCount ||
			gacha.GuaranteedRarityRank <= 0 || gacha.RemainderRarityRank <= 0 {
			return gachaPlayResult{}, errors.New("gacha guaranteed-result policy is invalid")
		}
		var err error
		guaranteedCardIDs, guaranteedCardWeights, err = s.gachaPoolForRarityLocked(
			drawCardIDs, drawCardWeights, gacha.GuaranteedRarityRank,
		)
		if err != nil {
			return gachaPlayResult{}, err
		}
		remainderCardIDs, remainderCardWeights, err = s.gachaPoolForRarityLocked(
			drawCardIDs, drawCardWeights, gacha.RemainderRarityRank,
		)
		if err != nil {
			return gachaPlayResult{}, err
		}
	}
	rewards := make([]gamestate.Reward, drawCount)
	for draw := range rewards {
		if len(gacha.RewardPool) > 0 {
			reward, err := drawWeightedReward(gacha.RewardPool, 0)
			if err != nil {
				return gachaPlayResult{}, err
			}
			rewards[draw] = reward
			continue
		}
		poolCardIDs := remainderCardIDs
		poolCardWeights := remainderCardWeights
		if draw < gacha.GuaranteedCount {
			poolCardIDs = guaranteedCardIDs
			poolCardWeights = guaranteedCardWeights
		}
		cardID, err := weightedGachaCard(poolCardIDs, poolCardWeights)
		if err != nil {
			return gachaPlayResult{}, err
		}
		rewards[draw] = GachaCardReward(cardID)
		rewards[draw].CardFame = int16(gacha.CardFame(cardID))
		if err := s.validateRewardLocked(rewards[draw]); err != nil {
			return gachaPlayResult{}, err
		}
	}
	gifts := gacha.CurrentGifts()
	for _, rule := range gacha.CurrentRandomGifts() {
		reward, err := drawWeightedReward(rule.Pool, rule.EmptyWeight)
		if err != nil {
			return gachaPlayResult{}, err
		}
		if reward.Num > 0 {
			gifts = append(gifts, reward)
		}
	}
	if err := s.validateSettlementRewardsLocked(rewards); err != nil {
		return gachaPlayResult{}, err
	}
	for _, gift := range gifts {
		if err := s.validateRewardLocked(gift); err != nil {
			return gachaPlayResult{}, err
		}
	}

	switch gacha.PayType {
	case 2:
		s.friendPoint -= paymentCost
	case 3:
		freeSpend := min(s.coinFree, paymentCost)
		s.coinFree -= freeSpend
		s.coin -= paymentCost - freeSpend
	case 4:
		paidItem.Num -= paymentCost
		s.items[paidItem.ItemID] = paidItem
	case 6:
		s.coin -= paymentCost
	}
	s.gachas[index].PlayCount++
	if s.gachaDailyClaims == nil {
		s.gachaDailyClaims = make(map[int]gamestate.GachaDailyClaim)
	}
	claim.PlayCount = min(math.MaxInt32, claim.PlayCount+1)
	claim.FreeClaimed = claim.FreeClaimed || dailyFree
	s.gachaDailyClaims[gachaID] = claim
	if len(selectedRewards) > 0 {
		s.gachaSelections[gachaID] = cloneRewards(selectedRewards)
	}
	result := PresentReceiveResult{}
	for _, reward := range rewards {
		if err := s.applyRewardOrPresentLocked(reward, &result, "扭蛋奖励"); err != nil {
			return gachaPlayResult{}, err
		}
	}
	// The client announces gifts as mail separately from the draw results.
	presentGifts := make([]gamestate.Reward, 0, len(gifts))
	for _, gift := range gifts {
		presentGifts = append(presentGifts, s.appendRewardPresentLocked(gift, "扭蛋附赠", ""))
	}
	if paidItem.ItemID != 0 {
		paidItem = s.items[paidItem.ItemID]
	}
	if err := s.advanceOnboardingLocked(onboardingEvent{kind: "gacha", gachaID: gachaID}); err != nil {
		return gachaPlayResult{}, err
	}
	return gachaPlayResult{
		Gachas:       s.VisibleGachasLocked(),
		Item:         paidItem,
		Reward:       result,
		Expectancy:   s.gachaMixedResultExpectancy(rewards),
		PresentGifts: presentGifts,
	}, nil
}

func (s *Account) gachaPoolForRarityLocked(
	cardIDs []int,
	weights []int,
	rarityRank int,
) ([]int, []int, error) {
	if len(cardIDs) == 0 || len(cardIDs) != len(weights) || rarityRank <= 0 {
		return nil, nil, errors.New("invalid gacha rarity pool request")
	}
	filteredIDs := make([]int, 0, len(cardIDs))
	filteredWeights := make([]int, 0, len(cardIDs))
	for index, cardID := range cardIDs {
		definition, exists := s.cardDefinitions[cardID]
		if !exists {
			return nil, nil, fmt.Errorf("gacha card definition %d is unavailable", cardID)
		}
		if definition.RarityRank != rarityRank {
			continue
		}
		filteredIDs = append(filteredIDs, cardID)
		filteredWeights = append(filteredWeights, weights[index])
	}
	if len(filteredIDs) == 0 {
		return nil, nil, fmt.Errorf("gacha rarity %d pool is empty", rarityRank)
	}
	return filteredIDs, filteredWeights, nil
}

func weightedGachaCard(cardIDs []int, weights []int) (int, error) {
	if len(cardIDs) == 0 || len(cardIDs) != len(weights) {
		return 0, errors.New("invalid gacha card weights")
	}
	total := 0
	for _, weight := range weights {
		if weight <= 0 || total > int(^uint(0)>>1)-weight {
			return 0, errors.New("invalid gacha card weight")
		}
		total += weight
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	if err != nil {
		return 0, fmt.Errorf("select gacha card: %w", err)
	}
	selected := int(value.Int64())
	for index, weight := range weights {
		if selected < weight {
			return cardIDs[index], nil
		}
		selected -= weight
	}
	return 0, errors.New("select gacha card outside configured weights")
}

func CloneGachaProfiles(source []gamestate.GachaProfile) []gamestate.GachaProfile {
	return gamestate.CloneGachas(source)
}
