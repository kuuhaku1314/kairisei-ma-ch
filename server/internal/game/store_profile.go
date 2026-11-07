package game

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) UserName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentName
}

type playerProgressionState struct {
	Level               int
	Experience          int
	NowLevelExperience  int
	NextLevelExperience int
	FollowMax           int
	Jobs                []gamestate.JobParameter
}

func (s *Account) PlayerProgressionState() playerProgressionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return playerProgressionState{
		Level:               s.currentLevel,
		Experience:          s.currentExperience,
		NowLevelExperience:  s.currentLevelExperience,
		NextLevelExperience: s.nextLevelExperience,
		FollowMax:           s.currentFollowMax,
		Jobs:                append([]gamestate.JobParameter(nil), s.currentJobs...),
	}
}

func (s *Account) UserLevel() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentLevel
}

func (s *Account) JobParameter(jobType int8) gamestate.JobParameter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	index := int(jobType)
	if index < 0 || index >= len(s.currentJobs) {
		return gamestate.JobParameter{}
	}
	return s.currentJobs[index]
}

func (s *Account) UserCreated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.currentName) != ""
}

func (s *Account) ActiveArthurType() int8 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentActiveArthur
}

func (s *Account) ActiveArthurLeaderState() (int64, int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeArthurLeaderLocked()
}

func (s *Account) activeArthurLeaderLocked() (int64, int, bool) {
	deck, found := SelectPartnerDeck(s.decks, s.currentActiveArthur)
	if !found {
		return 0, 0, false
	}
	cardByUniqueID := make(map[int64]CardInfo, len(s.cards))
	for _, card := range s.cards {
		cardByUniqueID[card.UniqueID] = card
	}
	leader, found := PartnerLeaderCard(deck, cardByUniqueID)
	if !found {
		return 0, 0, false
	}
	return leader.UniqueID, leader.CardID, true
}

func (s *Account) SupportDeckState() (int, []int8) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Alchemist support slots are already fully configurable. Publish that
	// derived count alongside the four persisted ordinary unlock counts.
	counts := make([]int8, 5)
	for index := range counts {
		counts[index] = s.supportUnlockedForArthurLocked(int8(index + 1))
	}
	return s.supportDeckSetCardNum, counts
}

func (s *Account) supportUnlockedForArthurLocked(arthurType int8) int8 {
	if arthurType == 5 {
		return int8(s.supportSlotCapacity)
	}
	return s.supportUnlockedSlots[arthurType-1]
}

func (s *Account) CardCollectionState() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]int, 0, len(s.cardCollectionIDs))
	for _, page := range s.cardCollectionPages {
		for _, cardID := range page {
			if _, found := s.cardCollectionIDs[cardID]; cardID != 0 && found {
				result = append(result, cardID)
			}
		}
	}
	sort.Ints(result)
	return result
}

func (s *Account) UnlockSupportCardSlot(arthurType int8) ([]int8, int, error) {
	if arthurType < 1 || arthurType > 4 {
		return nil, 0, errors.New("arthur_type must be 1 through 4")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	arthurIndex := int(arthurType) - 1
	unlocked := int(s.supportUnlockedSlots[arthurIndex])
	if unlocked >= s.supportDeckSetCardNum {
		return nil, 0, errors.New("all active support-card slots are already unlocked")
	}
	rule := s.supportDeckRules[unlocked]
	if rule.SlotIndex != unlocked+1 {
		return nil, 0, errors.New("support-card slot rule order is invalid")
	}
	if s.currentLevel < rule.Level {
		return nil, 0, errors.New("Arthur level does not meet the support-card slot requirement")
	}
	if s.cardCollectionCountLocked() < rule.CardCollectionNum {
		return nil, 0, errors.New("card collection does not meet the support-card slot requirement")
	}
	if s.gold < rule.Gold {
		return nil, 0, ErrInsufficientGold
	}
	s.gold -= rule.Gold
	s.supportUnlockedSlots[arthurIndex]++
	return append([]int8(nil), s.supportUnlockedSlots...), s.gold, nil
}

func (s *Account) CreateUser(name string, arthurType int8) bool {
	name = strings.TrimSpace(name)
	if name == "" || arthurType < 1 || arthurType > 4 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.currentName) != "" {
		return false
	}
	s.currentName = name
	s.currentActiveArthur = arthurType
	if s.unlockedFeatureIDs == nil {
		s.unlockedFeatureIDs = make(map[uint]struct{})
	}
	for featureID := uint(0); featureID <= 3; featureID++ {
		delete(s.unlockedFeatureIDs, featureID)
	}
	s.unlockedFeatureIDs[uint(arthurType-1)] = struct{}{}
	return true
}

func (s *Account) UserComment() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentComment
}

func (s *Account) TutorialState() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tutorialFlag
}

func (s *Account) MergeTutorialFlag(flag int64) bool {
	if flag < 0 || flag >= int64(1)<<gamestate.TutorialFlagCount {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tutorialFlag |= flag
	return true
}

func (s *Account) SetUserName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentName = name
	return true
}

func (s *Account) SetUserComment(comment string) bool {
	if len([]byte(comment)) > userCommentMaxBytes {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentComment = comment
	return true
}

func (s *Account) HonorState() ([]int, []int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	honorIDs := make([]int, 0, len(s.honorIDs))
	for honorID := range s.honorIDs {
		honorIDs = append(honorIDs, honorID)
	}
	sort.Ints(honorIDs)
	return append([]int(nil), s.deckHonorIDs...), honorIDs
}

func (s *Account) SetHonorDeck(honorIDs []int) bool {
	if len(honorIDs) != 4 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, honorID := range honorIDs {
		if honorID == 0 {
			continue
		}
		if _, exists := s.honorIDs[honorID]; !exists {
			return false
		}
	}
	s.deckHonorIDs = append([]int(nil), honorIDs...)
	return true
}

func (s *Account) SelectCostume(arthurType int8, costumeID int) bool {
	if arthurType < 1 || arthurType > 4 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if costumeID != 0 {
		if _, exists := s.costumeIDs[costumeID]; !exists {
			return false
		}
	}
	s.avatars[int(arthurType)-1].CostumeID = costumeID
	return true
}

func (s *Account) AvatarsState() []gamestate.Avatar {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneAvatars(s.avatars)
}

func cloneAvatars(avatars []gamestate.Avatar) []gamestate.Avatar {
	cloned := make([]gamestate.Avatar, len(avatars))
	for index, avatar := range avatars {
		cloned[index] = gamestate.Avatar{
			CostumeID:     avatar.CostumeID,
			AvatarPartIDs: append([]int(nil), avatar.AvatarPartIDs...),
		}
	}
	return cloned
}

const (
	FriendStateOther    int8 = 0
	FriendStateFriend   int8 = 3
	FriendStateFollow   int8 = 5
	FriendStateFollower int8 = 6
)

func (s *Account) FollowMaximum() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentFollowMax
}

func (s *Account) FollowerDisplayLimit() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.playerProgression.Friends.FollowerDisplayLimit
}

func (s *Account) StampState() ([]int, []int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	owned := make([]int, 0, len(s.stampIDs))
	for stampID := range s.stampIDs {
		owned = append(owned, stampID)
	}
	sort.Ints(owned)
	return owned, append([]int{}, s.stampDeck...)
}

func (s *Account) SetStampDeck(stampIDs []int) error {
	if len(stampIDs) != stampDeckSlots {
		return fmt.Errorf("stamp deck must contain exactly %d slots", stampDeckSlots)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, stampID := range stampIDs {
		if stampID == 0 {
			continue
		}
		if _, exists := s.stampIDs[stampID]; !exists {
			return fmt.Errorf("unknown stamp ID %d", stampID)
		}
	}
	s.stampDeck = append([]int(nil), stampIDs...)
	return nil
}

func normalizeStampDeck(stampIDs []int) []int {
	deck := make([]int, stampDeckSlots)
	copy(deck, stampIDs)
	return deck
}

func (s *Account) NaviID() int8 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentNaviID
}

func (s *Account) NaviUnlockState() (int64, []int8) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]int8, 0, len(s.selectableNaviIDs))
	for id := range s.selectableNaviIDs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	// The native mask stays nonnegative; the ID list includes IDs 63 and above.
	return s.naviUnlockFlag & math.MaxInt64, ids
}

func (s *Account) NaviOwnershipState() map[int8]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[int8]struct{}, len(s.selectableNaviIDs))
	for id := range s.selectableNaviIDs {
		result[id] = struct{}{}
	}
	return result
}

func (s *Account) SelectNavi(id int8) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.selectableNaviIDs[id]; !exists {
		return false
	}
	s.currentNaviID = id
	return true
}

func (s *Account) PurchaseNavi(id int8) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id < 0 {
		return errors.New("invalid navigator ID")
	}
	if _, exists := s.naviCatalogIDs[id]; !exists {
		return errors.New("navigator is absent from the local catalog")
	}
	if _, exists := s.selectableNaviIDs[id]; exists {
		return &BusinessError{-1, "已拥有这个看板。"}
	}
	price, enabled := s.naviPriceLocked(id)
	if !enabled {
		return &BusinessError{-1, "该看板暂未开放购买。"}
	}
	if s.coin+s.coinFree < price {
		return ErrInsufficientCrystals
	}
	freeSpend := min(s.coinFree, price)
	s.coinFree -= freeSpend
	s.coin -= price - freeSpend
	s.unlockNaviLocked(id)
	return nil
}

func (s *Account) unlockNaviLocked(id int8) {
	s.selectableNaviIDs[id] = struct{}{}
	if id < 63 {
		s.naviUnlockFlag |= int64(1) << uint(id)
	}
}

func (s *Account) OptionState() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gameOptionFlag, s.pushOptionFlag
}

func (s *Account) SetGameOption(flag int) bool {
	if flag < 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gameOptionFlag = flag
	return true
}

func (s *Account) SetPushOption(flag int) bool {
	if flag < 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushOptionFlag = flag
	return true
}
