package battlecore

func (engine *BattleEngine) TurnNumber() int { return engine.turn }
func (engine *BattleEngine) IsEnded() bool   { return engine.phase == PhaseEnded }

func (engine *BattleEngine) StateView() StateView {
	view := StateView{Turn: engine.turn, EndType: engine.endType, Ended: engine.IsEnded(),
		Members: make([]CombatantView, 0, engine.playerCount()+engine.enemyCount)}
	for _, player := range engine.playerStates() {
		view.Members = append(view.Members, CombatantView{
			MemberType: player.MemberType, ArthurType: player.ArthurType, HP: player.HP, MaxHP: player.MaxHP,
			Attack: player.Attack, Magic: player.Magic, BuffCount: len(player.Effects), Cost: player.Cost,
		})
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		view.Members = append(view.Members, CombatantView{
			MemberType: enemy.MemberType, EnemyID: enemy.EnemyID, HP: enemy.HP, MaxHP: enemy.MaxHP,
			Attack: enemy.Attack, Magic: enemy.Magic, BuffCount: len(enemy.Effects),
			Flags: enemy.AIFlags, Variables: enemy.AIVariables,
		})
	}
	return view
}

// These queries expose battle facts by value. Account eligibility, presentation
// barriers and reward publication remain decisions made by the caller.
func (engine *BattleEngine) MemberAlive(memberType int) bool {
	return engine != nil && engine.playerMember(memberType) && engine.player(memberType).HP > 0
}

func (engine *BattleEngine) HasSubmission(memberType int) bool {
	if engine == nil {
		return false
	}
	_, exists := engine.selectedPlays[memberType]
	return exists
}

func (engine *BattleEngine) Selection(memberType int) (CardSelection, bool) {
	selection, exists := engine.selectedPlays[memberType]
	return selection, exists
}

// The owner serializes engine calls. Observers receive owned values and must
// not mutate the battle; adapters publish observations only after success.
func (engine *BattleEngine) SetDeckOrderObserver(observer func(DeckOrderChange)) {
	engine.deckObserver = observer
}

func (engine *BattleEngine) notifyDeckOrder(player *battlePlayer, opening bool) {
	if engine.deckObserver == nil {
		return
	}
	change := DeckOrderChange{State: engine.StateView(), MemberType: player.MemberType, Opening: opening}
	for i, slot := range player.DeckOrder {
		if slot >= 0 && slot < len(player.Deck) {
			change.Order[i] = player.Deck[slot].CardType
		}
	}
	engine.deckObserver(change)
}

func (engine *BattleEngine) ContinuePending() bool {
	return engine != nil && engine.continuePending
}

func (engine *BattleEngine) TotalTurns() int {
	return engine.elapsedWaveTurns + engine.turn
}

// WaveOutcome uses zero-based enemy indexes, matching the drop inputs. It
// records combat outcomes only; it never grants or persists a reward.
type WaveOutcome struct {
	DestroyedEnemyBits int
	ReleasedDropBits   int
}

func (engine *BattleEngine) WaveOutcome() WaveOutcome {
	var result WaveOutcome
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		if enemy.HP <= 0 || enemy.Broken {
			result.DestroyedEnemyBits |= 1 << index
		}
		if enemy.DropReleased {
			result.ReleasedDropBits |= 1 << index
		}
	}
	return result
}

// ChaliceSlots captures current execution state. A presentation adapter takes
// this snapshot before computing EnemyPhase, then owns its input window until
// the corresponding animation finishes; it must not read future engine state.
func (engine *BattleEngine) ChaliceSlots() [maxRoomMembers][deckSphereSlots]bool {
	var playable [maxRoomMembers][deckSphereSlots]bool
	for playerIndex, player := range engine.players {
		for slot, sphere := range player.Spheres {
			playable[playerIndex][slot] = sphere.SphereID != 0 &&
				sphere.Type == SphereTypeChalice && sphere.Count > 0 && sphere.ChalicePlayable
		}
	}
	return playable
}
