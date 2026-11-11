package masterdata

import (
	"encoding/json"
	"fmt"
	"slices"

	"kairisei.local/server/internal/gamestate"
)

// Archive portraits show effective drops. The GuaiLi list shows base cards
// whose GOD recipes consume those drops, not the drops' own evolved forms.
// Rebuild both views at catalog assembly and when operator drops change.
func ProjectPastBossDropCatalog(groups []json.RawMessage, profiles []gamestate.TeamBattleRewardProfile,
	transitions []gamestate.EvolutionTransition,
) ([]json.RawMessage, error) {
	dropsByBoss := make(map[int][]int, len(profiles))
	for _, profile := range profiles {
		if profile.StageQuestAreaID != 0 || profile.TowerID != 0 {
			continue
		}
		cards := make([]int, 0)
		add := func(reward gamestate.Reward) {
			if reward.Type == 6 && reward.Num > 0 && reward.RewardTypeID > 0 {
				cards = append(cards, reward.RewardTypeID)
			}
		}
		if profile.EnemyDrops == nil {
			for _, reward := range profile.ResultRewards {
				add(reward)
			}
		} else {
			for _, drop := range profile.EnemyDrops {
				if drop.ChancePerMillion == nil || *drop.ChancePerMillion > 0 {
					add(drop.Reward)
				}
			}
		}
		slices.Sort(cards)
		dropsByBoss[profile.BossID] = slices.Compact(cards)
	}
	guailiByMaterial := make(map[int][]int)
	for _, transition := range transitions {
		if transition.Type != 1 { // GOD / GuaiLi evolution
			continue
		}
		for _, material := range transition.Materials {
			guailiByMaterial[material.CardID] = append(guailiByMaterial[material.CardID], transition.FromCardID)
		}
	}
	projected := make([]json.RawMessage, 0, len(groups))
	for _, raw := range groups {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(raw, &group); err != nil {
			return nil, fmt.Errorf("decode past-boss drop view: %w", err)
		}
		var bosses []map[string]json.RawMessage
		if err := json.Unmarshal(group["13"], &bosses); err != nil {
			return nil, fmt.Errorf("decode past-boss difficulties: %w", err)
		}
		cards := make([]int, 0)
		for _, boss := range bosses {
			var bossID int
			if err := json.Unmarshal(boss["0"], &bossID); err != nil {
				return nil, fmt.Errorf("decode past-boss drop identity: %w", err)
			}
			ids, exists := dropsByBoss[bossID]
			if !exists || len(ids) == 0 {
				return nil, fmt.Errorf("past-boss %d has no obtainable reward card", bossID)
			}
			view := make([]map[string]int, 0, len(ids))
			for _, id := range ids {
				view = append(view, map[string]int{"0": id, "1": 0})
				if !slices.Contains(cards, id) {
					cards = append(cards, id)
				}
			}
			boss["12"], _ = json.Marshal(view)
		}
		if len(cards) == 0 {
			return nil, fmt.Errorf("past-boss group %s has no difficulties", group["0"])
		}
		guailiCards := make([]int, 0)
		for _, id := range cards {
			guailiCards = append(guailiCards, guailiByMaterial[id]...)
		}
		slices.Sort(guailiCards)
		guailiCards = slices.Compact(guailiCards)
		if len(guailiCards) == 0 {
			// TeamBtMgr reads evo_cards[0]; zero means no GuaiLi-material entry.
			guailiCards = []int{0}
		}
		group["7"], _ = json.Marshal(cards)
		group["8"], _ = json.Marshal(guailiCards)
		group["13"], _ = json.Marshal(bosses)
		encoded, err := json.Marshal(group)
		if err != nil {
			return nil, err
		}
		projected = append(projected, encoded)
	}
	return projected, nil
}
