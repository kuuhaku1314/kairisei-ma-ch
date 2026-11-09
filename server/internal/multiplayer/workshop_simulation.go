//go:build workshop

package multiplayer

import (
	"context"
	"fmt"
	"sort"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/gamestate"
)

// These are explicit author-controlled test members, never loaded accounts.
type WorkshopTestMember struct {
	Supports [3]WorkshopTestSupport `json:"supports"`
	HP       int                    `json:"hp"`
	Attack   int                    `json:"attack"`
	Magic    int                    `json:"magic"`
	Recovery int                    `json:"recovery"`
	Cards    [10]int                `json:"cards"`
}

type WorkshopTestSupport struct {
	Card        int `json:"card"`
	LovePercent int `json:"love_percent"`
}

// UI percentages are converted to an actual native loyalty amount. Ceiling
// reaches the requested threshold even when a card's maximum is indivisible.
func WorkshopSupportLove(maximum, percent int) int {
	return int((int64(maximum)*int64(percent) + 99) / 100)
}

type WorkshopSimulationRequest struct {
	Parties     []int                            `json:"parties"`
	EnemyTypes  []int8                           `json:"enemy_types,omitempty"`
	ScorePolicy *gamestate.TeamBattleScorePolicy `json:"score_policy,omitempty"`
	Players     [4]WorkshopTestMember            `json:"players"`
	Turns       int                              `json:"turns"`
	Seed        uint32                           `json:"seed"`
	CostInitial int                              `json:"cost_initial"`
	EndTurn     int                              `json:"end_turn"`
	Automatic   bool                             `json:"automatic"`
}
type WorkshopCombatMember struct {
	Member    int      `json:"member"`
	EnemyID   int      `json:"enemy_id,omitempty"`
	Name      string   `json:"name"`
	HP        int      `json:"hp"`
	MaxHP     int      `json:"max_hp"`
	Attack    int      `json:"attack"`
	Magic     int      `json:"magic"`
	Buffs     int      `json:"buffs"`
	Cost      int      `json:"cost,omitempty"`
	Flags     uint32   `json:"flags,omitempty"`
	Variables [5]int32 `json:"variables"`
}
type WorkshopCombatFrame struct {
	Wave    int                    `json:"wave"`
	Turn    int                    `json:"turn"`
	Phase   string                 `json:"phase"`
	Results []BattleResult         `json:"native_results"`
	Members []WorkshopCombatMember `json:"members"`
	EndType int                    `json:"end_type"`
}
type WorkshopSimulation struct {
	State          string                `json:"state"`
	Turns          int                   `json:"turns"`
	CompletedWaves int                   `json:"completed_waves"`
	Frames         []WorkshopCombatFrame `json:"frames"`
	ScoreDamage    int64                 `json:"score_damage,omitempty"`
}

// A reproducible starter test deck, not a recommended player build. Authors can
// replace the ten IDs; the engine handles all actual card and enemy effects.
func WorkshopTestSquad(c *CombatCatalog) ([4]WorkshopTestMember, error) {
	var squad [4]WorkshopTestMember
	ids := make([]int, 0, len(c.Cards))
	for id := range c.Cards {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for role := 1; role <= 4; role++ {
		member := WorkshopTestMember{HP: 100000, Attack: 15000, Magic: 15000, Recovery: 15000}
		used := map[int]bool{}
		count := 0
		for _, id := range ids {
			card := c.Cards[id]
			if card.MaxLevel < 1 || card.PassiveSkillID != 0 || card.CallSkillID != 0 {
				continue
			}
			skill, _, err := c.CardSkill(id, role)
			if err != nil || skill.Job != ArthurJob(role) || skill.Cost < 1 || skill.Cost > 3 {
				continue
			}
			if role == 1 && skill.Kind != "ATTACK" || role == 3 && skill.Kind != "SORCERY" {
				continue
			}
			if role == 4 && skill.Kind != "RECOVERY" {
				continue
			}
			base := card.BaseCardID
			if base == 0 {
				base = id
			}
			if used[base] {
				continue
			}
			if err = c.ValidateMainCardPassives(card); err != nil {
				continue
			}
			used[base] = true
			member.Cards[count] = id
			count++
			if count == 10 {
				break
			}
		}
		if count < 10 {
			return squad, fmt.Errorf("职业 %d 缺少足够的基础演练卡牌，请提供测试卡组", role)
		}
		squad[role-1] = member
	}
	return squad, nil
}

func WorkshopSimulateRows(ctx context.Context, catalog *CombatCatalog, rows map[string][][]string, replaceOwned bool, req WorkshopSimulationRequest) (WorkshopSimulation, error) {
	c, err := workshopCatalogRows(catalog, rows, replaceOwned)
	if err != nil {
		return WorkshopSimulation{}, err
	}
	return WorkshopSimulate(ctx, c, req)
}

// Drive the production phase API, including passives, enemy effects, expiry,
// deaths and ordinary wave handover. This is computation, not Android replay.
func WorkshopSimulate(ctx context.Context, catalog *CombatCatalog, req WorkshopSimulationRequest) (report WorkshopSimulation, runErr error) {
	if len(req.Parties) < 1 || len(req.Parties) > 64 || req.Turns < 1 || req.Turns > 100 || req.CostInitial < 1 || req.CostInitial > 10 || req.EndTurn < 0 || req.EndTurn > 999 {
		return report, fmt.Errorf("演练波数／回合／COST设置无效")
	}
	if len(req.EnemyTypes) != 0 && len(req.EnemyTypes) != len(req.Parties) {
		return report, fmt.Errorf("阶段类型数量不匹配")
	}
	waveTypes := make([]int8, len(req.Parties))
	for i := range req.Parties {
		kind := int8(1)
		if len(req.EnemyTypes) > 0 {
			kind = req.EnemyTypes[i]
		}
		if kind != 1 && kind != 4 || i == 0 && kind != 1 {
			return report, fmt.Errorf("阶段类型无效")
		}
		waveTypes[i] = kind
	}
	if err := gamestate.ValidateTeamBattleScorePolicy(req.ScorePolicy); err != nil {
		return report, err
	}
	if req.ScorePolicy != nil && len(waveTypes) != 1 {
		return report, fmt.Errorf("计分演练仅支持单场")
	}
	members := make([]battlecore.Participant, 4)
	for index, p := range req.Players {
		if p.HP < 1 || p.HP > 10000000 || p.Attack < 0 || p.Attack > 1000000 || p.Magic < 0 || p.Magic > 1000000 || p.Recovery < 0 || p.Recovery > 1000000 {
			return report, fmt.Errorf("演练队员属性超出范围")
		}
		member := battlecore.Participant{MemberType: index + 1, ArthurType: index + 1, HP: p.HP, Attack: p.Attack, Magic: p.Magic, Mind: p.Recovery}
		for slot, id := range p.Cards {
			card, ok := catalog.Cards[id]
			if !ok || card.MaxLevel < 1 {
				return report, fmt.Errorf("队员 %d 的第 %d 张测试卡不可用", index+1, slot+1)
			}
			if _, _, err := catalog.CardSkill(id, index+1); err != nil {
				return report, err
			}
			member.DeckCards = append(member.DeckCards, BattleCard{CardType: slot + 1, CardID: id, Level: card.MaxLevel})
		}
		for slot, support := range p.Supports {
			if support.LovePercent < 0 || support.LovePercent > 100 {
				return report, fmt.Errorf("EX忠诚度比例需在0–100内")
			}
			if support.Card == 0 {
				continue
			}
			card, ok := catalog.Cards[support.Card]
			if !ok || card.MaxLevel < 1 {
				return report, fmt.Errorf("队员%d的EX槽%d卡牌不可用", index+1, slot+1)
			}
			entry := BattleCard{CardType: 11 + slot, CardID: card.ID, Level: card.MaxLevel, Love: WorkshopSupportLove(card.LoveMax, support.LovePercent)}
			if _, _, e := catalog.CardSupportSkill(entry); e != nil {
				return report, e
			}
			member.SupportCards = append(member.SupportCards, entry)
		}
		members[index] = member
	}
	for _, party := range req.Parties {
		if _, ok := catalog.EnemyParties[party]; !ok {
			return report, fmt.Errorf("缺少演练波次 %d", party)
		}
	}
	config := battlecore.Config{EnemyPartyID: req.Parties[0], EnemyType: int(waveTypes[0]), CostInitial: req.CostInitial, HoldMax: 6, Seed: req.Seed, EndTurn: req.EndTurn}
	if req.ScorePolicy != nil {
		config.EndTurn = req.ScorePolicy.EndTurn
		config.MemberDeadEnd = req.ScorePolicy.MemberDeadEnd
	}
	engine, err := NewBattleEngine(catalog, config, members)
	if err != nil {
		return report, err
	}
	wave, rowCount := 0, 0
	record := func(phase string, rows []BattleResult) error {
		if req.ScorePolicy != nil {
			report.ScoreDamage = engine.ScoreDamage()
		}
		rowCount += len(rows)
		if rowCount > 100000 {
			return fmt.Errorf("演练事件超过十万条，请减少回合")
		}
		view := engine.StateView()
		frame := WorkshopCombatFrame{Wave: wave + 1, Turn: view.Turn, Phase: phase, Results: rows, EndType: view.EndType, Members: []WorkshopCombatMember{}}
		for _, member := range view.Members {
			name := ArthurJob(member.ArthurType)
			if member.EnemyID != 0 {
				name = catalog.Enemies[member.EnemyID].Name
			}
			frame.Members = append(frame.Members, WorkshopCombatMember{
				Member: member.MemberType, EnemyID: member.EnemyID, Name: name,
				HP: member.HP, MaxHP: member.MaxHP, Attack: member.Attack, Magic: member.Magic,
				Buffs: member.BuffCount, Cost: member.Cost, Flags: member.Flags, Variables: member.Variables,
			})
		}
		report.Frames = append(report.Frames, frame)
		return nil
	}
	invoke := func(phase string, fn func() ([]BattleResult, error)) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		rows, e := fn()
		if e != nil {
			return fmt.Errorf("第 %d 波／回合 %d／%s: %w", wave+1, engine.TurnNumber(), phase, e)
		}
		return record(phase, rows)
	}
	if err = invoke("start", engine.Start); err != nil {
		return report, err
	}
	for {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		if engine.IsEnded() {
			if req.ScorePolicy != nil {
				report.State = "score_complete"
				report.CompletedWaves = 1
				return report, nil
			}
			if engine.EndType() != 1 && engine.EndType() != 4 {
				report.State = "defeat"
				return report, nil
			}
			report.CompletedWaves++
			at, exists := battlecore.NextWaveIndex(waveTypes, wave, engine.EndType())
			if !exists {
				report.State = "victory"
				return report, nil
			}
			next, e := engine.NextBattle(req.Parties[at], nil)
			if e != nil {
				return report, e
			}
			engine = next
			wave = at
			if err = invoke("start", engine.Start); err != nil {
				return report, err
			}
		}
		if report.Turns >= req.Turns {
			report.State = "turn_limit"
			return report, nil
		}
		report.Turns++
		for _, phase := range []struct {
			name string
			fn   func() ([]BattleResult, error)
		}{{"turn", engine.TurnPhase}, {"user", engine.UserPhase}, {"selection", func() ([]BattleResult, error) {
			var rows []BattleResult
			err := engine.WithCardSelections(func(batch *CardSelectionBatch) error {
				for member := 1; member <= 4; member++ {
					var choice battlecore.CardSelection
					var e error
					if req.Automatic {
						choice, e = npcCardSubmission(batch, member, int(waveTypes[wave]))
						if e != nil {
							return e
						}
					}
					r, e := batch.Submit(member, choice)
					if e != nil {
						return e
					}
					rows = append(rows, r...)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			return rows, nil
		}}, {"user_attack", engine.UserAttack}, {"chalice_user", engine.ExecuteChaliceUserPhase}, {"enemy", engine.EnemyPhase}, {"chalice_enemy", engine.ExecuteChaliceEnemyPhase}} {
			if engine.IsEnded() {
				break
			}
			if err = invoke(phase.name, phase.fn); err != nil {
				return report, err
			}
		}
	}
}
