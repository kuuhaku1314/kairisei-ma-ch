package multiplayer

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/gamestate"
)

func roomSpecBattles(spec RoomSpec) []gamestate.TeamBattleReplayBattle {
	if len(spec.Battles) != 0 {
		return spec.Battles
	}
	return []gamestate.TeamBattleReplayBattle{{EnemyPartyID: spec.EnemyPartyID, EnemyType: int8(spec.EnemyType)}}
}

func roomSpecWaveDrops(spec RoomSpec, index int) []BattleDrop {
	if spec.DropLedgerVersion == 0 {
		return spec.Drops
	}
	var drops []BattleDrop
	for _, drop := range gamestate.TeamBattleWaveDrops(spec.DropPlan, roomBattleEnemyTypes(spec.Battles), index) {
		if drop.BattleIndex == index {
			drops = append(drops, BattleDrop{EnemyIndex: drop.EnemyIndex, RewardType: drop.Reward.Type, RewardTypeID: drop.Reward.RewardTypeID, Num: drop.Reward.Num})
		}
	}
	return drops
}

func roomBattleEnemyTypes(waves []gamestate.TeamBattleReplayBattle) []int8 {
	types := make([]int8, len(waves))
	for i, wave := range waves {
		types[i] = wave.EnemyType
	}
	return types
}

func roomBodyRewardAlreadyReleased(current *room, wave int) bool {
	source := gamestate.TeamBattleBodyRewardWave(roomBattleEnemyTypes(current.battles), wave)
	for _, drop := range current.releasedDrops {
		if drop.EnemyIndex == 0 && drop.BattleIndex >= source && drop.BattleIndex < wave {
			return true
		}
	}
	return false
}

func roomCountdownPayload(current *room) string {
	waves := current.battles
	fields := []string{strconv.Itoa(len(waves))}
	for _, wave := range waves {
		fields = append(fields, strconv.Itoa(wave.EnemyPartyID), strconv.Itoa(int(wave.EnemyType)), "0", "", "0", "")
	}
	return joinCSV(fields...)
}

// BattleFunctionBase.calcNextBattleIndex skips optional AWAKE encounters on
// an ordinary win. Awakening does not advance the displayed progress counter.
func roomNextBattle(current *room) (int, bool) {
	return battlecore.NextWaveIndex(roomBattleEnemyTypes(current.battles), current.battleIndex, current.engineBattleEnd)
}

func roomBattleProgress(current *room, index int) int {
	progress := index
	for i := 0; i <= index && i < len(current.battles); i++ {
		if current.battles[i].EnemyType == 4 {
			progress--
		}
	}
	return max(0, progress)
}

func recordRoomWaveDrops(current *room) {
	if current.engine == nil {
		return
	}
	if _, recorded := current.waveOutcomes[current.battleIndex]; recorded {
		return
	}
	if current.waveOutcomes == nil {
		current.waveOutcomes = make(map[int]battlecore.WaveOutcome)
	}
	outcome := current.engine.WaveOutcome()
	current.destroyedEnemyBits |= outcome.DestroyedEnemyBits
	for index := 0; index < maxRoomMembers; index++ {
		if outcome.ReleasedDropBits&(1<<index) != 0 {
			if index == 0 && roomBodyRewardAlreadyReleased(current, current.battleIndex) {
				continue
			}
			for _, drop := range gamestate.TeamBattleWaveDrops(current.dropPlan, roomBattleEnemyTypes(current.battles), current.battleIndex) {
				if drop.BattleIndex == current.battleIndex && drop.EnemyIndex == index {
					current.releasedDrops = append(current.releasedDrops, cloneDropPlan([]gamestate.TeamBattleEnemyDrop{drop})[0])
				}
			}
		}
	}
	current.waveOutcomes[current.battleIndex] = outcome
}

// The original resume API takes prior segments' rewards as caller input.
// A completed/pending wave may already be recorded in the settlement ledger;
// keep that wave in 108 only, so reconnect does not also replay it as 109.
func priorWaveResumeDrops(ledger []gamestate.TeamBattleEnemyDrop, battleIndex int) []BattleDrop {
	var drops []BattleDrop
	for _, drop := range ledger {
		if drop.BattleIndex < battleIndex {
			drops = append(drops, BattleDrop{EnemyIndex: drop.EnemyIndex, RewardType: drop.Reward.Type,
				Num: drop.Reward.Num, RewardTypeID: drop.Reward.RewardTypeID})
		}
	}
	return drops
}

func (c *clientConn) handleGameNextFinish(payload string) error {
	if payload != "" {
		return errors.New("GameNextFinish payload is not empty")
	}
	hub := c.server.hub
	session := hub.lockRoomSession(c.roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || current.connections[c.memberType] != c || c.comebackPending {
		session.Unlock()
		return errors.New("GameNextFinish has no active member")
	}
	if !current.nextBattlePending { // Retransmitted ACK must never start twice.
		session.Unlock()
		return nil
	}
	current.gameNextFinished[c.memberType] = true
	roomID := current.RoomID
	session.Unlock()
	return c.server.tryAdvanceNextBattle(roomID)
}

func (s *Server) tryAdvanceNextBattle(roomID int64) error {
	hub := s.hub
	session := hub.lockRoomSession(roomID)
	defer session.Unlock()
	current, exists := session.room, session.room != nil
	if !exists || current.State != RoomStateBattle || !current.nextBattlePending || !roomBarrierReady(current.connections, current.gameNextFinished) {
		session.Unlock()
		return nil
	}
	index := current.nextBattleIndex
	wave := current.battles[index]
	drops := roomSpecWaveDrops(RoomSpec{DropLedgerVersion: current.dropLedgerVersion, DropPlan: current.dropPlan, Battles: current.battles}, index)
	if roomBodyRewardAlreadyReleased(current, index) {
		kept := drops[:0]
		for _, drop := range drops {
			if drop.EnemyIndex != 0 {
				kept = append(kept, drop)
			}
		}
		drops = kept
	}
	engine, err := current.engine.NextBattle(wave.EnemyPartyID, drops)
	if err != nil {
		session.Unlock()
		return err
	}
	results, err := engine.Start()
	if err != nil {
		session.Unlock()
		return fmt.Errorf("start next battle: %w", err)
	}
	result, err := roomStartResult(current, results)
	if err != nil {
		session.Unlock()
		return err
	}
	current.engine, current.engineBattleEnd = engine, 0
	current.battleIndex, current.progress = index, roomBattleProgress(current, index)
	current.enemyPartyID, current.enemyType, current.drops = wave.EnemyPartyID, int(wave.EnemyType), drops
	current.nextBattlePending = false
	current.gameNextFinished = nil
	current.gameStartFinished = make(map[int]bool)
	current.turnPhaseStarted, current.userPhaseStarted, current.userAttackStarted = false, false, false
	current.chaliceUserStarted, current.enemyPhaseStarted, current.chaliceEnemyStarted = false, false, false
	current.turnNumber = 0
	current.turnPhaseFinished, current.userAttackFinished = make(map[int]bool), make(map[int]bool)
	current.chaliceUserFinished, current.enemyPhaseFinished, current.chaliceEnemyFinished = make(map[int]bool), make(map[int]bool), make(map[int]bool)
	current.cardPlayPlans, current.cardPlaySubmissions = make(map[int]cardPlaySubmission), make(map[int]cardPlaySubmission)
	connections := append([]*clientConn(nil), roomConnections(current)...)
	payload := strconv.FormatInt(time.Now().Unix(), 10) + "," + result
	deliveries := reserveRoomFramesLocked(connections, battleFrame{"ApiGameStart", payload})
	session.Unlock()
	broadcastRoomFrames(s, roomID, deliveries)
	s.logger.Info("local multiplayer next wave started", "room_id", roomID, "battle_index", index, "enemy_party_id", wave.EnemyPartyID)
	return nil
}
