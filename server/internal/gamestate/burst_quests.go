package gamestate

import "encoding/json"

// BurstQuest describes the retained CN character learning scripts. Initial
// rewards are LOCAL_POLICY; script/talk/battle identities and the four Buddy
// families come from the original CN bundles.
type BurstQuest struct {
	ArthurType int8
	StoryIDs   [3]int
	BuddyID    int
}

func BurstQuests() [4]BurstQuest {
	return [4]BurstQuest{
		{1, [3]int{45001000, 45001010, 45001020}, 1000010},
		{2, [3]int{45002000, 45002010, 45002020}, 1000020},
		{3, [3]int{45003000, 45003010, 45003020}, 1000030},
		{4, [3]int{45004000, 45004010, 45004020}, 1000040},
	}
}

func FindBurstStory(storyID int) (BurstQuest, int, bool) {
	for _, quest := range BurstQuests() {
		for index, id := range quest.StoryIDs {
			if id == storyID {
				return quest, index, true
			}
		}
	}
	return BurstQuest{}, 0, false
}

func ArthurBurstUnlocked(features []uint, arthurType int8) int8 {
	if arthurType >= 1 && arthurType <= 4 {
		for _, feature := range features {
			if feature == uint(32+arthurType) {
				return 1
			}
		}
	}
	return 0
}

// StoryTeamBattleSession is process-local, like an active solo battle. Only
// completed chapters, rewards and feature bits belong in the account snapshot.
type StoryTeamBattleSession struct {
	StoryID  int
	Response json.RawMessage
}
