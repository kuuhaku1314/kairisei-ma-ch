package admin

import (
	"fmt"
	"slices"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func dropTargetsForReplay(r gamestate.TeamBattleReplay, parties map[int]multiplayer.CombatEnemyParty, enemies map[int]multiplayer.CombatEnemyDefinition) []dropTarget {
	targets := []dropTarget{}
	waves := r.Battles
	if len(waves) == 0 {
		waves = []gamestate.TeamBattleReplayBattle{{EnemyPartyID: r.EnemyPartyID, EnemyType: r.EnemyType}}
	}
	for wave, w := range waves {
		if w.EnemyType == 4 {
			continue
		}
		for slot, e := range parties[w.EnemyPartyID].Slots {
			if e.EnemyID > 0 {
				name := enemies[e.EnemyID].Name
				if name == "" {
					name = fmt.Sprintf("敌人 %d", e.EnemyID)
				}
				target := dropTarget{BattleIndex: wave, EnemyIndex: slot, Name: name}
				if def, ok := enemies[e.EnemyID]; ok {
					target.Stats = &enemyStats{EnemyID: def.ID, Attribute: def.Attribute,
						HP: def.HP * e.HPRate, Attack: def.Attack, Magic: def.Magic,
						Defense: def.Defense, MagicDefense: def.MagicDefense,
						DamageReduction: def.DamageReduction, AttributeFixed: def.AttributeFixed}
				}
				targets = append(targets, target)
			}
		}
	}
	return targets
}
func projectBattleDrops(p gamestate.TeamBattleRewardProfile, config BossDrops) (gamestate.TeamBattleRewardProfile, []map[string]int) {
	original := p
	// Gold and other pre-existing fixed rewards are not editable here.
	p.FameRewards = slices.Clone(config.FameRewards)
	p.EnemyDrops = make([]gamestate.TeamBattleEnemyDrop, 0, len(config.Drops)+1)
	for _, d := range original.EnemyDrops {
		if !editableDroppedReward(d.Reward) {
			p.EnemyDrops = append(p.EnemyDrops, d)
		}
	}
	p.EnemyDrops = append(p.EnemyDrops, config.Drops...)
	p.ResultRewards = make([]gamestate.Reward, 0)
	for _, r := range original.ResultRewards {
		if !multiplayer.ValidBattleDrop(multiplayer.BattleDrop{RewardType: r.Type, RewardTypeID: r.RewardTypeID, Num: r.Num}) {
			p.ResultRewards = append(p.ResultRewards, r)
		}
	}
	ids := map[int]bool{}
	view := []map[string]int{}
	for _, d := range p.EnemyDrops {
		if d.ChancePerMillion != nil && *d.ChancePerMillion == 0 {
			continue
		}
		p.ResultRewards = append(p.ResultRewards, d.Reward)
		if (d.Reward.Type == 6 || d.Reward.Type == 13) && !ids[d.Reward.RewardTypeID] {
			ids[d.Reward.RewardTypeID] = true
			view = append(view, map[string]int{"0": d.Reward.RewardTypeID, "1": 0})
		}
	}
	return p, view
}
