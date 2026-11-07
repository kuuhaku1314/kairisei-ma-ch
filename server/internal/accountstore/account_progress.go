package accountstore

import (
	"encoding/json"
	"fmt"
	"maps"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

// Only identities and player progress cross the persistence boundary. Names,
// artwork, costs, enemy definitions and reward lineups come from the catalog.
type catalogProgress struct {
	BossBuffUntil map[int]int64                          `json:"boss_buff_until,omitempty"`
	GachaPlays    map[int]int                            `json:"gacha_plays,omitempty"`
	ShopPurchases map[int]int                            `json:"shop_purchases,omitempty"`
	ShopPeriods   map[int]gamestate.ItemShopPeriodCounts `json:"shop_periods,omitempty"`
	BossStates    map[int]int                            `json:"boss_states,omitempty"`
	Areas         []masterdata.NormalQuestProgress       `json:"areas,omitempty"`
	MainStory     map[int]int                            `json:"main_story,omitempty"`
	CNMainStory   map[int]int                            `json:"cn_main_story,omitempty"`
	SubStory      map[int]int                            `json:"sub_story,omitempty"`
	EventStory    map[int]int                            `json:"event_story,omitempty"`
	QuestStory    []int                                  `json:"quest_story,omitempty"`
}

func collectCatalogProgress(state gamestate.State) (catalogProgress, error) {
	p := catalogProgress{
		GachaPlays: map[int]int{}, ShopPurchases: map[int]int{}, BossStates: map[int]int{}, BossBuffUntil: map[int]int64{},
		MainStory: map[int]int{}, CNMainStory: map[int]int{}, SubStory: map[int]int{}, EventStory: map[int]int{},
	}
	p.QuestStory = append([]int(nil), state.Story.QuestClearedIDs...)
	listed := make(map[int]bool, len(state.Gachas))
	for _, gacha := range state.Gachas {
		listed[gacha.GachaID] = true
		if gacha.PlayCount != 0 {
			p.GachaPlays[gacha.GachaID] = gacha.PlayCount
		}
	}
	for id, plays := range state.OperatorGachaPlays {
		if !listed[id] && plays != 0 {
			p.GachaPlays[id] = plays
		}
	}
	for _, tab := range state.ItemShopTabs {
		for _, item := range tab.Lineup {
			if used := item.StockNum - item.StockRemain; used > 0 {
				p.ShopPurchases[item.LineupID] = used
			}
		}
	}
	if err := gamestate.ValidateItemShopProgress(state.ItemShopPurchases, state.ItemShopPeriods); err != nil {
		return p, err
	}
	for id, count := range state.ItemShopPurchases {
		p.ShopPurchases[id] = count
	}
	p.ShopPeriods = maps.Clone(state.ItemShopPeriods)
	var solo map[string]json.RawMessage
	if len(state.TeamBattleSolo) > 0 {
		if err := json.Unmarshal(state.TeamBattleSolo, &solo); err != nil {
			return p, err
		}
	}
	for _, category := range masterdata.BattleProgressCategories {
		if len(solo[category]) == 0 {
			continue
		}
		var groups []struct {
			Bosses []struct {
				BossID int   `json:"0"`
				State  int   `json:"10"`
				Until  int64 `json:"16"`
			} `json:"10"`
		}
		if err := json.Unmarshal(solo[category], &groups); err != nil {
			return p, fmt.Errorf("decode battle progress category %s: %w", category, err)
		}
		for _, group := range groups {
			for _, boss := range group.Bosses {
				if boss.Until > p.BossBuffUntil[boss.BossID] {
					p.BossBuffUntil[boss.BossID] = boss.Until
				}
				if boss.State > p.BossStates[boss.BossID] {
					p.BossStates[boss.BossID] = boss.State
				}
			}
		}
	}
	areas := state.StageQuestAreas
	if len(areas) == 0 && len(state.MainQuest) > 0 {
		areas = []json.RawMessage{state.MainQuest}
	}
	for _, raw := range areas {
		var progress masterdata.NormalQuestProgress
		if err := json.Unmarshal(raw, &progress); err != nil {
			return p, err
		}
		p.Areas = append(p.Areas, progress)
	}
	collectMain := func(parts []gamestate.StoryMainPart, flags map[int]int) {
		for _, part := range parts {
			for _, section := range part.Sections {
				for _, story := range section.Stories {
					if flag := story.StateFlag & 7; flag != 0 {
						flags[story.StoryMainID] = flag
					}
				}
			}
		}
	}
	collectMain(state.Story.MainParts, p.MainStory)
	collectMain(state.Story.CNMainParts, p.CNMainStory)
	for _, character := range state.Story.SubCharacters {
		for _, section := range character.Sections {
			for _, story := range section.Stories {
				if flag := story.StateFlag & 7; flag != 0 {
					p.SubStory[story.StorySubID] = flag
				}
			}
		}
	}
	for _, event := range state.Story.Events {
		for _, story := range event.Stories {
			if flag := story.Sub.StateFlag & 7; flag != 0 {
				p.EventStory[story.Sub.StorySubID] = flag
			}
		}
	}
	return p, nil
}

func (p catalogProgress) apply(state *gamestate.State) error {
	if err := gamestate.ValidateItemShopProgress(p.ShopPurchases, p.ShopPeriods); err != nil {
		return err
	}
	state.Gachas = cloneGachas(state.Gachas)
	listed := make(map[int]bool, len(state.Gachas))
	for i := range state.Gachas {
		state.Gachas[i].PlayCount = p.GachaPlays[state.Gachas[i].GachaID]
		listed[state.Gachas[i].GachaID] = true
	}
	state.OperatorGachaPlays = map[int]int{}
	for id, plays := range p.GachaPlays {
		if !listed[id] {
			state.OperatorGachaPlays[id] = plays
		}
	}
	state.ItemShopTabs = CloneItemShopTabs(state.ItemShopTabs)
	state.ItemShopPurchases = maps.Clone(p.ShopPurchases)
	state.ItemShopPeriods = maps.Clone(p.ShopPeriods)
	for _, tab := range state.ItemShopTabs {
		for i := range tab.Lineup {
			item := &tab.Lineup[i]
			item.StockRemain = max(0, item.StockNum-p.ShopPurchases[item.LineupID])
		}
	}
	if len(state.TeamBattleSolo) > 0 {
		var solo map[string]json.RawMessage
		if err := json.Unmarshal(state.TeamBattleSolo, &solo); err != nil {
			return err
		}
		for _, category := range masterdata.BattleProgressCategories {
			if len(solo[category]) == 0 {
				continue
			}
			var groups []map[string]json.RawMessage
			if err := json.Unmarshal(solo[category], &groups); err != nil {
				return err
			}
			for _, group := range groups {
				var bosses []map[string]json.RawMessage
				if err := json.Unmarshal(group["10"], &bosses); err != nil {
					return err
				}
				for _, boss := range bosses {
					var id int
					if err := json.Unmarshal(boss["0"], &id); err != nil {
						return err
					}
					boss["10"] = json.RawMessage(fmt.Sprint(p.BossStates[id]))
					if until := p.BossBuffUntil[id]; until > 0 {
						boss["16"] = json.RawMessage(fmt.Sprint(until))
					}
				}
				encoded, err := json.Marshal(bosses)
				if err != nil {
					return err
				}
				group["10"] = encoded
			}
			encoded, err := json.Marshal(groups)
			if err != nil {
				return err
			}
			solo[category] = encoded
		}
		encodedSolo, err := json.Marshal(solo)
		if err != nil {
			return err
		}
		state.TeamBattleSolo = encodedSolo
	}
	byArea := make(map[int]json.RawMessage, len(p.Areas))
	for _, area := range p.Areas {
		encoded, err := json.Marshal(area)
		if err != nil {
			return err
		}
		byArea[area.StageQuest.AreaID] = encoded
	}
	applyArea := func(raw json.RawMessage) (json.RawMessage, error) {
		if len(raw) == 0 {
			return raw, nil
		}
		var identity masterdata.NormalQuestProgress
		if err := json.Unmarshal(raw, &identity); err != nil {
			return nil, err
		}
		if progress, exists := byArea[identity.StageQuest.AreaID]; exists {
			return masterdata.MergeNormalQuestAreaProgress(raw, progress)
		}
		return raw, nil
	}
	var err error
	state.MainQuest, err = applyArea(state.MainQuest)
	if err != nil {
		return err
	}
	state.StageQuestAreas = cloneRawMessages(state.StageQuestAreas)
	for i, area := range state.StageQuestAreas {
		state.StageQuestAreas[i], err = applyArea(area)
		if err != nil {
			return err
		}
	}
	// Clone the mutable story projection; the catalog remains shared/read-only.
	encoded, err := json.Marshal(state.Story)
	if err != nil {
		return err
	}
	var story gamestate.StoryCatalogState
	if err := json.Unmarshal(encoded, &story); err != nil {
		return err
	}
	story.StoryBattleIDs = state.Story.StoryBattleIDs
	story.StoryBattleReferences = state.Story.StoryBattleReferences
	applyMain := func(parts []gamestate.StoryMainPart, flags map[int]int) {
		for _, part := range parts {
			for _, section := range part.Sections {
				for i := range section.Stories {
					item := &section.Stories[i]
					item.StateFlag = item.StateFlag&8 | flags[item.StoryMainID]
				}
			}
		}
		masterdata.NormalizeStoryProgress(parts)
	}
	applyMain(story.MainParts, p.MainStory)
	applyMain(story.CNMainParts, p.CNMainStory)
	for _, character := range story.SubCharacters {
		for _, section := range character.Sections {
			for i := range section.Stories {
				item := &section.Stories[i]
				flag := p.SubStory[item.StorySubID]
				item.StateFlag = item.StateFlag&8 | flag
				if flag&3 != 0 {
					item.UnlockText = ""
				}
			}
		}
	}
	for _, event := range story.Events {
		for i := range event.Stories {
			item := &event.Stories[i].Sub
			flag := p.EventStory[item.StorySubID]
			item.StateFlag = item.StateFlag&(8|16) | flag
			if flag&3 != 0 {
				item.UnlockText = ""
			}
		}
	}
	state.Story = story
	state.Story.QuestClearedIDs = append([]int(nil), p.QuestStory...)
	return nil
}
