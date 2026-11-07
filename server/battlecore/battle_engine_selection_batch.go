package battlecore

import (
	"errors"
	"fmt"
	"maps"
)

// CardSelectionBatch exposes only selection operations on a tentative view.
// It cannot advance a phase. The caller serializes access to its battle and
// must not retain the batch after WithCardSelections returns.
type CardSelectionBatch struct {
	view *BattleEngine
}

// WithCardSelections commits selection state and RNG only if evaluate succeeds.
// Adapters may validate/encode result rows inside evaluate, before committing;
// they must publish those rows only after this call succeeds. Battle phases,
// accounts, transport acknowledgements and publication stay outside this API.
func (engine *BattleEngine) WithCardSelections(evaluate func(*CardSelectionBatch) error) error {
	if engine == nil || evaluate == nil {
		return errors.New("combat card selection batch is unavailable")
	}
	preview := engine.selectionView()
	batch := &CardSelectionBatch{view: &preview}
	defer func() { batch.view = nil }()
	if err := evaluate(batch); err != nil {
		return err
	}
	engine.commitSelections(&preview)
	return nil
}

func (batch *CardSelectionBatch) NPCSubmission(memberType, enemyType int) (cardPlaySubmission, error) {
	if batch.view == nil {
		return cardPlaySubmission{}, errors.New("combat card selection batch is closed")
	}
	if len(batch.view.catalog.NPC.Rules) == 0 && (!batch.view.playerMember(memberType) || batch.view.player(memberType).SummonSource == 0) {
		return cardPlaySubmission{}, errors.New("combat NPC content is unavailable")
	}
	return batch.view.npcSubmission(memberType, enemyType, batch.view.catalog.NPC)
}

func (batch *CardSelectionBatch) Submit(memberType int, submission cardPlaySubmission) ([]BattleResult, error) {
	if batch.view == nil {
		return nil, errors.New("combat card selection batch is closed")
	}
	return batch.view.Submit(memberType, submission)
}

// resetCardSubmission is the native mutable-input boundary. It refunds only
// this member's previous plan, preserving other members and live battle state.
// Both manual replacement and NPC takeover call it on a tentative batch.
func (engine *BattleEngine) resetCardSubmission(member int) error {
	if _, exists := engine.selectedPlays[member]; !exists {
		return nil
	}
	delete(engine.selectedPlays, member)
	player := engine.player(member)
	player.Cost = maxInt(0, engine.memberTurnCost(player)-engine.playerEffectValue(player, "COST_BLOCK"))
	var err error
	engine.turnActions, err = engine.selectedBattleActions()
	return err
}

// SubmitClientSelection applies the original managed combined card/sphere
// budget while retaining native Submit's own cost accounting. The caller must
// return any error from its batch callback so the tentative state is discarded.
func (batch *CardSelectionBatch) SubmitClientSelection(memberType int, submission CardSelection) ([]BattleResult, error) {
	results, err := batch.Submit(memberType, submission)
	if err != nil {
		return nil, err
	}
	if submission.SphereSlot != 0 {
		remaining, cost, err := batch.RemainingSphereBudget(memberType, submission.SphereSlot)
		if err != nil {
			return nil, err
		}
		if cost > remaining {
			return nil, fmt.Errorf("member %d card and sphere cost exceeds remaining cost", memberType)
		}
	}
	return results, nil
}

func (batch *CardSelectionBatch) HasSubmission(memberType int) bool {
	if batch.view == nil {
		return false
	}
	_, exists := batch.view.selectedPlays[memberType]
	return exists
}

func (batch *CardSelectionBatch) IsAlive(memberType int) bool {
	return batch.view != nil && batch.view.playerMember(memberType) && (*batch.view.player(memberType)).HP > 0
}

// Submissions returns values owned by the caller, never the engine's map.
func (batch *CardSelectionBatch) Submissions() map[int]cardPlaySubmission {
	if batch.view == nil {
		return nil
	}
	return maps.Clone(batch.view.selectedPlays)
}

// RemainingSphereBudget supports the original managed input-budget check.
// Native Submit's own sphere accounting is preserved independently.
func (batch *CardSelectionBatch) RemainingSphereBudget(memberType, sphereSlot int) (remaining, cost int, err error) {
	if batch.view == nil || !batch.view.playerMember(memberType) || sphereSlot < 1 || sphereSlot > deckSphereSlots {
		return 0, 0, errors.New("combat sphere budget input is invalid")
	}
	player := &(*batch.view.player(memberType))
	skill, _, err := batch.view.catalog.SphereSkill(player.Spheres[sphereSlot-1].SphereID)
	return player.Cost, skill.Cost, err
}
