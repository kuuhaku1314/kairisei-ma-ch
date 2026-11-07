package battlecore

import (
	"errors"
	"fmt"
)

// ResumeResults projects the current authoritative Go state through the
// dedicated RESUME_* command family consumed by the CN 6.0.2 client. This is
// the server-side counterpart of libbattle5 battle5_api_resume_data_get: it is
// a state snapshot, not a replay of already applied damage and buff rows.
// Like 26050 -> 3d680/40392, publishing it commits only display caches.
func (engine *BattleEngine) ResumeResults(haveDrops ...BattleDrop) ([]BattleResult, error) {
	if engine == nil || engine.phase == PhaseCreated {
		return nil, errors.New("combat resume state is unavailable")
	}
	// Build atomically on a copy; gameplay/RNG must not change. Native commits
	// submitted-card and Sphere display caches, which affect later 314/315 rows.
	live := engine
	view := *engine
	if engine.opponents != nil {
		party := *engine.opponents
		view.opponents = &party
	}
	engine = &view

	results := make([]BattleResult, 0, 160)
	if engine.pvp {
		for _, p := range engine.playerStates() {
			if p.HP > 0 {
				results = append(results, rewriteResult(p.MemberType, p.BaseAttribute))
			}
		}
	}
	// 1b3c0 restores identities before any RESUME_* rows. Unlike Start, the
	// comeback transport does not provide a separate CARD identity prefix.
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 {
			continue
		}
		for _, card := range player.Deck {
			if card.CardType != 0 && card.CardID != 0 {
				results = append(results, CardIdentityResult(player.MemberType, card))
			}
		}
		for slot, card := range player.SupportDeck {
			card.CardType = slot + 11
			results = append(results, CardIdentityResult(player.MemberType, card))
		}
	}
	results = append(results, engine.setupIdentityResults()...)
	// 26050 emits the caller's prior-wave reward vector after identity and
	// before turn state. It preserves order and duplicates without aggregating.
	for _, drop := range haveDrops {
		if !ValidBattleDrop(drop) {
			return nil, errors.New("invalid prior-wave resume reward")
		}
		results = append(results, BattleResult{Command: CommandResumeHaveDrop, Args: []int64{
			int64(drop.RewardType), int64(drop.Num), int64(drop.RewardTypeID),
		}})
	}
	results = append(results, BattleResult{Command: CommandResumeTurn, Args: []int64{
		int64(engine.turn), int64(engine.resumeBattleSide()),
	}})

	// a6078 follows the identity/turn prefix with cost and cost-block per user.
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 {
			continue
		}
		if engine.pvp || player.GameOver {
			continue
		}
		results = append(results,
			BattleResult{Command: CommandCost, Args: []int64{int64(player.MemberType), int64(engine.memberTurnCost(player))}},
			BattleResult{Command: CommandCostBlock, Args: []int64{int64(player.MemberType), int64(engine.playerEffectValue(player, "COST_BLOCK"))}},
		)
	}

	for _, player := range engine.playerStates() {
		if player.MemberType == 0 {
			continue
		}
		results = append(results, BattleResult{Command: CommandResumeCardDeck, Args: []int64{int64(player.MemberType), int64(player.remainingDeckCount())}})
		for _, slot := range player.Hand {
			// Original PVP resume retains token hand slots but omits ordinary
			// resume's111 token-identity prefix (19e70 calls423e5;1efa0 does not).
			if engine.pvp || !isTokenCardType(slot) {
				continue
			}
			card := player.handSlotCard(slot)
			skill, _, err := engine.handCardSkill(player, card)
			if err != nil {
				return nil, err
			}
			display := player.CardDisplay[slot]
			results = append(results, BattleResult{Command: 111, Args: []int64{int64(player.MemberType), int64(slot), int64(skill.ID), int64(card.Level), int64(display.Power), int64(card.CardID)}})
		}
		hand, err := engine.resumePlayerHandResults(player)
		if err != nil {
			return nil, err
		}
		results = append(results, hand)
		for _, hold := range player.CardHolds {
			selected := 0
			if hold.Action.sphereSlot != 0 && engine.selectedPlays[player.MemberType].SphereSlot == hold.Action.sphereSlot {
				selected = 1
			}
			for _, cardType := range engine.selectedPlays[player.MemberType].CardTypes {
				if cardType != 0 && cardType == hold.Action.cardType {
					selected = 1
				}
			}
			results = append(results, BattleResult{Command: CommandResumeHold, Args: []int64{
				int64(player.MemberType), int64(hold.Action.cardType), int64(hold.Action.sphereSlot), int64(selected),
				int64(hold.Remaining), int64(hold.Action.skill.ID), 0, int64(hold.Action.cardLevel),
			}})
		}
		// Original PVP restores ordinary holds, but omits the separate
		// CURSE/BLESS snapshot vector. These entries still remain live.
		if !engine.pvp {
			for _, hold := range player.BlessHolds {
				results = append(results, resumeHoldResult(player.MemberType, hold))
			}
		}
		for _, sphere := range player.Spheres {
			if sphere.SphereID != 0 && sphere.Type == SphereTypeChalice && sphere.ChalicePlayable {
				results = append(results,
					BattleResult{Command: CommandChaliceSpherePlayable, Args: []int64{int64(player.MemberType), int64(sphere.Slot)}},
					BattleResult{Command: CommandChaliceSphereReserve, Args: []int64{int64(player.MemberType), int64(player.ReservedChalice)}},
				)
				break
			}
		}
	}
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 {
			continue
		}
		if !engine.pvp {
			results = append(results, playerBaseParameterResult(player))
		}
		results = append(results,
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(
				player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic,
				player.Recovery, player.Defense, player.MDefense,
				player.LimitAttack, player.LimitMagic, player.LimitRecovery,
			)},
		)
	}
	// Original PVP publishes each party's card sweep then its sphere sweep.
	display, err := engine.selectionDisplayResults(1)
	if err != nil {
		return nil, err
	}
	results = append(results, display...)
	if engine.pvp {
		display, err = engine.selectionDisplayResults(maxRoomMembers + 1)
		if err != nil {
			return nil, err
		}
		results = append(results, display...)
	}
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 {
			continue
		}
		if !engine.pvp && player.HP > 0 && player.BurstState != burstGaugeUnavailable {
			results = append(results, engine.burstStateResult(player))
		}
	}
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 {
			continue
		}
		if player.GameOver {
			results = append(results, BattleResult{Command: CommandResumeGameOver, Args: []int64{int64(player.MemberType)}})
		}
	}

	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		dead := boolInt(enemy.HP <= 0)
		// JP106 publishes a part flag; the real parent index stays in the
		// registered input and internal member link.
		results = append(results,
			BattleResult{Command: CommandResumeEnemy, Args: []int64{
				// 26050 reads 166d2 separately from current HP; revival keeps
				// this one-time break/drop marker set (6fab0), even without drops.
				int64(enemy.MemberType), int64(boolInt(enemy.Parent > 0)), int64(dead), 0, 0, 0, int64(boolInt(enemy.DropResolved)),
			}},
		)
		results = append(results, resumeEnemyDropResults(enemy)...)
		results = append(results, resumeEnemyTrance(enemy)...)
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		results = append(results, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(
			enemy.MemberType, enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic,
			enemy.Recovery, enemy.Defense, enemy.MDefense,
			enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery,
		)},
		)
	}

	for _, p := range engine.playerStates() {
		results = appendResumeBuffs(results, p.MemberType, p.Effects, engine.pvp)
	}
	for index := 0; index < engine.enemyCount; index++ {
		results = appendResumeBuffs(results, engine.enemies[index].MemberType, engine.enemies[index].Effects, engine.pvp)
	}
	// PVP1efa0 ends after the retained buff/hand projection. The ordinary
	// snapshot additionally calls45eb9/b678b for status-display publication.
	if !engine.pvp {
		results = append(results, engine.publishStatusProjections()...)
	}
	for index := range engine.enemies {
		live.enemies[index].statusDisplay = engine.enemies[index].statusDisplay
	}
	for _, p := range engine.playerStates() {
		if p.MemberType == 0 {
			continue
		}
		target := live.player(p.MemberType)
		target.statusDisplay, target.CardDisplay = p.statusDisplay, p.CardDisplay
		for slot := range p.Spheres {
			target.Spheres[slot].Display = p.Spheres[slot].Display
		}
	}

	return results, nil
}

func appendResumeBuffs(results []BattleResult, member int, effects []battleEffect, pvp bool) []BattleResult {
	for _, list := range []int{0, 3, 5, 6} {
		if pvp && list >= 5 {
			continue
		}
		for _, effect := range effects {
			if effect.ListType != list {
				continue
			}
			if row, ok := resumeBuffResult(member, effect); ok {
				// Original PVP104 omits the ordinary snapshot's list-type column.
				if pvp {
					row.Args = append(row.Args[:1], row.Args[2:]...)
				}
				results = append(results, row)
			}
		}
	}
	return results
}

func (engine *BattleEngine) resumeBattleSide() int {
	if engine.pvp {
		return int(engine.turnDisplayLimit())
	}
	return engine.resumeSide
}

func (engine *BattleEngine) resumePlayerHandResults(player *battlePlayer) (BattleResult, error) {
	args := make([]int64, 1, 11)
	args[0] = int64(player.MemberType)
	for _, deckSlot := range player.Hand {
		if deckSlot == 0 {
			args = append(args, 0, 0)
			continue
		}
		if deckSlot < 1 || deckSlot > len(player.Deck) && !isTokenCardType(deckSlot) {
			return BattleResult{}, fmt.Errorf("combat resume member %d has invalid hand deck slot %d", player.MemberType, deckSlot)
		}
		card := player.handSlotCard(deckSlot)
		// 26050 writes zero here; 3d680 later publishes actual CARD_UPDATE.
		args = append(args, int64(card.CardType), 0)
	}
	return BattleResult{Command: CommandResumeCardHand, Args: args}, nil
}

func resumeHoldResult(memberType int, hold battleBlessHold) BattleResult {
	// The second (appended-skill) list in 26050 fixes the fourth value to
	// zero; it is not the repeat flag used by the BLESS execution producer.
	return BattleResult{Command: CommandResumeHold, Args: []int64{
		int64(memberType), int64(hold.CardType), 0, 0,
		int64(hold.Remaining), int64(hold.Skill.ID), int64(hold.AppendIndex),
		int64(hold.CardLevel),
	}}
}

func resumeBuffResult(memberType int, effect battleEffect) (BattleResult, bool) {
	// Native 26050 serializes only lists 0/3/5/6 with positive duration.
	// PASSIVE stays in the engine, not a temporary UI buff replayed on resume.
	if effect.ListType != 0 && effect.ListType != 3 && effect.ListType != 5 && effect.ListType != 6 {
		return BattleResult{}, false
	}
	if effect.Remaining <= 0 {
		return BattleResult{}, false
	}
	code, exists := battleBuffCodes[effect.Function]
	if !exists {
		return BattleResult{}, false
	}
	// 26050 initializes four zeros, then 453cc fills status-specific restore
	// fields. This is a UI restore record, not the producer's calculation
	// parameters or even a replay of the initial ResultCmd62 notification.
	parameters := nativeStatusUIParameters(code, effect)
	attributeFlags := 1 << combatAttributeCode(effect.Attribute)
	if effect.Function == "ENCHANT" || effect.Function == "ATTR_DEF_DOWN_BOOST" {
		attributeFlags = sphereSupportAttributeFlags(effect.Attribute)
	}
	if code == 206 || code == 22 || code == 15 || code == 106 {
		attributeFlags = 1
	} // Resume uses neutral status flags; CARD_SEAL's attribute filters card selection.
	return BattleResult{Command: CommandResumeBuff, Args: []int64{
		int64(memberType), int64(effect.ListType), int64(code), int64(effect.Remaining), int64(battleBuffKind(code)),
		int64(retainedEffectParameterFlags(effect)), int64(attributeFlags),
		int64(parameters[0]), int64(parameters[1]), int64(parameters[2]), int64(parameters[3]),
	}}, true
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
