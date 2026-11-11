package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

type teamBattlePartnerDeckSelect struct {
	UserID     int  `json:"userid"`
	ArthurType int8 `json:"arthur_type"`
	DeckIndex  int8 `json:"deck_idx"`
}

func (a *API) teamBattleRecommendDeckShow(writer http.ResponseWriter, _ *http.Request) {
	recommendations := make([]gamestate.TeamBattleRecommendation, len(a.initialState.TeamBattleRecommendations))
	for index, recommendation := range a.initialState.TeamBattleRecommendations {
		recommendation.RecommendIDs = append([]int(nil), recommendation.RecommendIDs...)
		recommendations[index] = recommendation
	}
	if len(recommendations) == 0 {
		writeError(writer, http.StatusInternalServerError, "team battle recommendations are unavailable")
		return
	}
	a.writeProtocol(writer, map[string]any{"recommends": recommendations})
}

func (a *API) teamBattlePastBossShow(writer http.ResponseWriter, _ *http.Request) {
	if len(a.initialState.TeamBattlePastBossGroups) == 0 {
		writeError(writer, http.StatusInternalServerError, "team battle past-boss archive is unavailable")
		return
	}
	partnerArthurs, err := a.localTeamBattlePartnerArthurs()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	numericArthurs, err := teamBattlePartnerArthursNumeric(partnerArthurs)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	groups, err := pastBossProgress(a.initialState.TeamBattlePastBossGroups, a.account.TeamBattleSoloState())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"0": groups,
		"1": numericArthurs,
	})
}

// Archive metadata is shared; clear state belongs to the requesting account.
func pastBossProgress(source []json.RawMessage, progress json.RawMessage) ([]json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(progress, &top); err != nil {
		return nil, err
	}
	states := map[int]int{}
	for _, key := range []string{"10", "11", "12"} {
		if len(top[key]) == 0 {
			continue
		}
		var groups []struct {
			Bosses []struct {
				ID    int `json:"0"`
				State int `json:"10"`
			} `json:"10"`
		}
		if err := json.Unmarshal(top[key], &groups); err != nil {
			return nil, err
		}
		for _, group := range groups {
			for _, boss := range group.Bosses {
				if boss.State > states[boss.ID] {
					states[boss.ID] = boss.State
				}
			}
		}
	}
	result := make([]json.RawMessage, len(source))
	for i, raw := range source {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(raw, &group); err != nil {
			return nil, err
		}
		var bosses []map[string]json.RawMessage
		if err := json.Unmarshal(group["13"], &bosses); err != nil {
			return nil, err
		}
		for _, boss := range bosses {
			var id int
			if err := json.Unmarshal(boss["0"], &id); err != nil {
				return nil, err
			}
			boss["10"], _ = json.Marshal(states[id])
		}
		group["13"], _ = json.Marshal(bosses)
		result[i], _ = json.Marshal(group)
	}
	return result, nil
}

// challengeShow closes the original client's optional challenge-detail
// preflight without inventing retired live-service content. The immutable CN
// container has no challenge condition, reward, or schedule master, and every
// currently published boss carries an empty challenge progress list. ProtoGen
// deliberately leaves infos nil when the wire array is empty, so the original
// stage, room, and pause-window callbacks keep the challenge window closed.
func (a *API) challengeShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID     int `json:"bossid"`
		TargetTime int `json:"target_time"`
	}
	if err := decodeExact(request, []string{"bossid", "target_time"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.TargetTime < 0 {
		writeError(writer, http.StatusBadRequest, "invalid challenge target time")
		return
	}
	catalog, err := a.account.TeamBattleCatalog()
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if _, found := teamBattleReplayForBoss(a.initialState.TeamBattleReplays, payload.BossID); !found ||
		!catalog.HasBoss(payload.BossID) {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}
	a.writeProtocol(writer, map[string]any{"infos": []any{}})
}

// All stages require this preflight; only configured score stages expose rewards.
func (a *API) teamBattleScoreRewardLineup(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID int                       `json:"bossid"`
		Mode   gamestate.BattleScoreMode `json:"mode"`
	}
	if err := decodeExact(request, []string{"bossid", "mode"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !payload.Mode.Valid() {
		writeError(writer, http.StatusBadRequest, "invalid score reward mode")
		return
	}
	catalog, err := a.account.TeamBattleCatalog()
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if _, found := teamBattleReplayForBoss(a.initialState.TeamBattleReplays, payload.BossID); !found ||
		!catalog.HasBoss(payload.BossID) {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}

	highScore, rewards := a.account.ScoreLineup(a.scoreBossID(payload.BossID), a.initialState.TeamBattleRewards, payload.Mode)
	a.writeProtocol(writer, map[string]any{"mode": payload.Mode, "high_score": highScore, "rewards": rewards})
}

func (a *API) userBuffExec(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UserBuffID int `json:"user_buff_id"`
	}
	if err := decodeExact(request, []string{"user_buff_id"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.ExecUserBuff(payload.UserBuffID, time.Now())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	unlockedBosses := make([]any, 0, len(result.Bosses))
	for _, raw := range result.Bosses {
		var numeric map[string]any
		if err := json.Unmarshal(raw, &numeric); err != nil {
			writeError(writer, http.StatusInternalServerError, "decode unlocked user-buff boss")
			return
		}
		named, err := teamBattleBossNamedDTO(numeric)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		unlockedBosses = append(unlockedBosses, named)
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"unlock_bosses": unlockedBosses,
		"update_items":  []gamestate.Item{result.Item},
	})
}

func (a *API) teamBattleSoloStart(writer http.ResponseWriter, request *http.Request) {
	payload, err := decodeSoloBattleStart(request)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.DeckArthurType < 1 || payload.DeckArthurType > 5 ||
		payload.DeckArthurTypeIndex < 0 ||
		len(payload.PartnerDeckSelects) > 3 {
		writeError(writer, http.StatusBadRequest, "invalid local team battle start")
		return
	}
	replay, found := teamBattleReplayForBoss(a.initialState.TeamBattleReplays, payload.BossID)
	catalog, err := a.account.TeamBattleCatalog()
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	isTowerBoss, towerOpen := a.account.TowerQuestBossAdmission(payload.BossID)
	if isTowerBoss && !towerOpen {
		a.writeStoreError(writer, &game.BusinessError{Code: -1, Message: "炼金副本暂未开放。"})
		return
	}
	isAccessible := isTowerBoss || catalog.BossAccessible(payload.BossID, time.Now().Unix())
	if !found || !isAccessible {
		writeError(writer, http.StatusBadRequest, "unknown team battle replay")
		return
	}
	rules, rulesFound := catalog.EntryRules(payload.BossID)
	if !rulesFound {
		// Tower and past entries can live outside the ordinary publication view.
		rules, rulesFound = a.account.TeamBattleEntryRulesForBoss(payload.BossID)
	}
	if !rulesFound {
		writeError(writer, http.StatusInternalServerError, "team battle entry rules are unavailable")
		return
	}
	if !rules.AllowsSolo() {
		writeError(writer, http.StatusBadRequest, "this battle requires multiplayer")
		return
	}
	party := make([]game.TeamBattleResultPartner, len(payload.PartnerDeckSelects))
	for index, selection := range payload.PartnerDeckSelects {
		party[index] = game.TeamBattleResultPartner{UserID: selection.UserID, ArthurType: selection.ArthurType, IsSelf: selection.UserID == a.initialState.User.UserID}
	}
	if err := rules.ValidateSoloParty(payload.DeckArthurType, party); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	battleSegments, err := teamBattleReplayBattles(replay)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}

	player, err := a.account.BattleLoadout(a.initialState.User.UserID, payload.DeckArthurType, payload.DeckArthurTypeIndex)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if player.LeaderFame <= 0 || player.LeaderFame > 100 {
		writeError(writer, http.StatusBadRequest, "selected player leader card is unavailable")
		return
	}
	var externalViews []friendPointPartnerView
	var externalUserIDs []int
	var ownState gamestate.State
	for _, selection := range payload.PartnerDeckSelects {
		if selection.UserID == a.initialState.User.UserID {
			if ownState.User.UserID == 0 {
				ownState = a.account.Snapshot(a.initialState)
			}
		} else {
			if rules.OnlyMyDeck != 0 {
				writeError(writer, http.StatusBadRequest, "this battle requires the player's own decks")
				return
			}
			externalUserIDs = append(externalUserIDs, selection.UserID)
		}
	}
	if len(externalUserIDs) > 0 {
		externalViews, err = a.friendPointPartnerViewsForUsers(externalUserIDs)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "load selected local-account team battle partners")
			return
		}
	}

	partnerDecks := make([]any, 0, 3)
	friendPointReward := 0
	rentalCredits := make([]game.FriendPointRentalCredit, 0, 3)
	selectedResultPartners := make([]game.TeamBattleResultPartner, 0, 3)
	clearDecks := make([]gamestate.BattleClearDeck, 0, 4)
	if a.battleHistory != nil {
		clearDecks = append(clearDecks, gamestate.BattleClearDeck{UserID: player.UserID, Name: player.Name, ArthurType: player.ArthurType, HonorIDs: player.DeckHonorIDs, Deck: player.ClearDeck})
	}
	deckHonorIDs, _ := a.account.HonorState()
	localHonorIDs := make([]int, 4)
	copy(localHonorIDs, deckHonorIDs)
	for _, selection := range payload.PartnerDeckSelects {
		if selection.ArthurType < 1 || selection.ArthurType > 4 || selection.DeckIndex < 0 {
			writeError(writer, http.StatusBadRequest, "invalid partner deck selection")
			return
		}
		if selection.UserID == a.initialState.User.UserID {
			// The original SetDeckData sends the same account ID for each
			// selected own profession. Reuse the ordinary partner deck DTO,
			// with state SELF at result, without rental points or follow offers.
			view, available := partnerViewFromState(ownState, selection.ArthurType)
			if !available {
				writeError(writer, http.StatusBadRequest, "selected own partner profession is unavailable")
				return
			}
			if err := a.validatePartnerDeckComposition(view, selection.DeckIndex); err != nil {
				a.writeStoreError(writer, err)
				return
			}
			deck, deckErr := view.deckWire(selection.DeckIndex)
			if deckErr != nil {
				writeError(writer, http.StatusBadRequest, deckErr.Error())
				return
			}
			// Own-deck battles read their member label from the start DTO's
			// deck name. Override only this response; deck browsers still need
			// the actual deck name and rental partners use PartnerInfo.name.
			if a.battleHistory != nil {
				encoded, err := json.Marshal(deck)
				if err != nil {
					a.writeStoreError(writer, err)
					return
				}
				clearDecks = append(clearDecks, gamestate.BattleClearDeck{UserID: view.UserID, Name: view.Name, ArthurType: int(view.ArthurType), HonorIDs: view.HonorIDs, Deck: encoded})
			}
			deck["name"] = view.Name
			partnerDecks = append(partnerDecks, deck)
			selectedDeck, _ := game.ExactDeck(view.Decks, view.ArthurType, selection.DeckIndex)
			leader, _ := game.PartnerLeaderCard(selectedDeck, view.Cards)
			selectedResultPartners = append(selectedResultPartners, game.TeamBattleResultPartner{
				IsBurst:       view.IsBurst,
				LastLoginUnix: time.Now().Unix(),
				UserID:        view.UserID, IsSelf: true, Name: view.Name, ArthurType: view.ArthurType,
				Level: view.Level, DeckRank: selectedDeck.DeckRank, LeaderCardID: leader.CardID,
				LeaderLevel: leader.Level, LeaderFame: leader.Fame, Comment: view.Comment,
				PVPPoint: view.PVPPoint, HonorIDs: append([]int(nil), view.HonorIDs...),
			})
			continue
		}
		view, viewFound := findFriendPointPartnerView(externalViews, selection.UserID)
		if !viewFound || view.ArthurType != selection.ArthurType {
			writeError(writer, http.StatusBadRequest, "unknown persistent team battle partner")
			return
		}
		if err := a.validatePartnerDeckComposition(view, selection.DeckIndex); err != nil {
			a.writeStoreError(writer, err)
			return
		}
		partnerDeck, deckErr := view.deckWire(selection.DeckIndex)
		if deckErr == nil {
			partnerReward := a.account.FriendPointRewardForState(view.FriendState)
			friendPointReward += partnerReward
			deck, _ := game.ExactDeck(view.Decks, view.ArthurType, selection.DeckIndex)
			leader, leaderFound := game.PartnerLeaderCard(deck, view.Cards)
			if !leaderFound {
				deckErr = errors.New("selected persistent partner leader is unavailable")
			} else {
				selectedResultPartners = append(selectedResultPartners, game.TeamBattleResultPartner{
					IsBurst:       view.IsBurst,
					LastLoginUnix: view.LastLoginUnix,
					UserID:        view.UserID, Name: view.Name, ArthurType: view.ArthurType,
					Level: view.Level, DeckRank: deck.DeckRank, LeaderCardID: leader.CardID,
					LeaderLevel: leader.Level, LeaderFame: leader.Fame, Comment: view.Comment,
					PVPPoint: view.PVPPoint, HonorIDs: append([]int(nil), view.HonorIDs...),
				})
			}
			rentalCredits = append(rentalCredits, game.FriendPointRentalCredit{
				OwnerUserID: view.UserID,
				FriendPoint: partnerReward,
			})
		}
		if deckErr != nil {
			writeError(writer, http.StatusInternalServerError, deckErr.Error())
			return
		}
		if a.battleHistory != nil {
			encoded, err := json.Marshal(partnerDeck)
			if err != nil {
				a.writeStoreError(writer, err)
				return
			}
			clearDecks = append(clearDecks, gamestate.BattleClearDeck{UserID: view.UserID, Name: view.Name, ArthurType: int(view.ArthurType), HonorIDs: view.HonorIDs, Deck: encoded})
		}
		partnerDecks = append(partnerDecks, partnerDeck)
	}
	context, _, started, startErr := a.beginSoloBattleEntry(payload, game.SoloBattleEntry{
		Opening:        &gamestate.TeamBattleOpening{CostInitial: replay.CostInitial, BurstGaugeInitial: replay.BurstGaugeInitial},
		ScoreBossID:    a.scoreBossID(payload.BossID),
		DeckArthurType: payload.DeckArthurType, UserID: a.initialState.User.UserID, BossID: payload.BossID,
		EnemyTypes: teamBattleEnemyTypes(battleSegments),
		Profiles:   a.initialState.TeamBattleRewards,
		FameSources: []game.TeamBattleFameSource{{
			MemberType: 1,
			LeaderFame: player.LeaderFame,
		}},
		FriendPointPartners: game.RentalPartnerCount(selectedResultPartners), FriendPointReward: friendPointReward,
		FriendPointRentalCredits: rentalCredits, SelectedPartners: selectedResultPartners,
	})
	if startErr != nil {
		writeError(writer, http.StatusBadRequest, startErr.Error())
		return
	} else if !started {
		writeError(writer, http.StatusBadRequest, "team battle points are insufficient")
		return
	}
	_, found = game.TeamBattleRewardProfileForContext(a.initialState.TeamBattleRewards, context)
	if !found {
		writeError(writer, http.StatusInternalServerError, "team battle drop profile is unavailable")
		return
	}
	if context.Opening != nil {
		replay.CostInitial = context.Opening.CostInitial
		replay.BurstGaugeInitial = context.Opening.BurstGaugeInitial
	}
	if context.ScorePolicy != nil {
		if context.ScorePolicy.IsDamageScore() {
			replay.BurstGaugeInitial = context.ScorePolicy.BurstGaugeInitial
		}
		replay.EndTurn = context.ScorePolicy.EndTurn
		if context.ScorePolicy.TeamCostInitial != 0 {
			replay.CostInitial = context.ScorePolicy.TeamCostInitial
		}
	}
	if a.battleHistory != nil {
		if err := a.account.SetTeamBattleClearDecks(payload.BossID, clearDecks); err != nil {
			a.writeStoreError(writer, err)
			return
		}
	}
	if !a.persistOrError(writer) {
		return
	}
	battlesWire := make([]any, len(battleSegments))
	for index, segment := range battleSegments {
		drop := teamBattleDropPlanWire(context.DropPlan, context.BattleEnemyTypes, index)
		battlesWire[index] = map[string]any{
			"enemy_partyid":        segment.EnemyPartyID,
			"enemy_type":           segment.EnemyType,
			"drop":                 drop,
			"event_special_reward": []any{},
			"talk_infos": []any{
				map[string]any{"talkid": 0, "script_name": ""},
				map[string]any{"talkid": 0, "script_name": ""},
			},
			"play_logs": []any{},
		}
	}

	a.writeProtocol(writer, map[string]any{
		"avatar": map[string]any{
			"costumeid":       player.CostumeID,
			"avatar_partsids": append([]int(nil), player.PartsIDs...),
		},
		"partner_deck":                       a.partnerDeckParameterViews(partnerDecks),
		"seed":                               context.Seed,
		"battles":                            battlesWire,
		"is_unlimited_free_continue":         0,
		"unlimited_free_continue_lv":         0,
		"free_continue_remain":               0,
		"cost_initial":                       replay.CostInitial,
		"burst_gauge_initial":                replay.BurstGaugeInitial,
		"is_active_only_burst_gauge_initial": 0,
		"hold_max":                           replay.HoldMax,
		"end_turn":                           replay.EndTurn,
		"score_policy":                       soloScoreStartPolicy(context.ScorePolicy),
		"is_solo_online":                     0,
		"container":                          []any{},
		"start_time":                         context.StartTime,
	})
}

func (a *API) teamBattleSoloContinue(writer http.ResponseWriter, request *http.Request) {
	var payload game.TeamBattleContinueReport
	fields := []string{"pay_type", "progress", "input_cmd", "enemy_dead_bit"}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	coin, coinFree, err := a.account.ContinueTeamBattle(payload, a.initialState, a.persistState)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"coin":                 coin,
		"coin_free":            coinFree,
		"free_continue_remain": 0,
	})
}

func (a *API) teamBattleSoloEnd(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Progress     int      `json:"progress"`
		IsClear      int8     `json:"is_clear"`
		InputCommand []string `json:"input_cmd"`
		EnemyDeadBit []int    `json:"enemy_dead_bit"`
		BossID       int      `json:"bossid"`
		ScoreDamage  *int64   `json:"score_damage,omitempty"`
	}
	fields := []string{
		"progress",
		"is_clear",
		"input_cmd",
		"enemy_dead_bit",
		"bossid",
	}
	if err := decodeExact(request, fields, &payload, "score_damage"); err != nil {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, err)
		return
	}
	if payload.Progress < 0 || (payload.IsClear != 0 && payload.IsClear != 1) ||
		len(payload.InputCommand) == 0 || len(payload.EnemyDeadBit) == 0 || payload.BossID <= 0 {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, errors.New("invalid local team battle result"))
		return
	}
	canonicalRequest, err := json.Marshal(payload)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode local team battle result identity")
		return
	}
	requestDigest := fmt.Sprintf("%x", sha256.Sum256(canonicalRequest))
	a.teamBattleResultMu.Lock()
	defer a.teamBattleResultMu.Unlock()
	if cached, exists := a.account.TeamBattleSoloResultReceipt(requestDigest, payload.BossID); exists {
		a.writeTeamBattleResultSnapshot(writer, cached, false)
		return
	}
	catalog, err := a.account.TeamBattleCatalog()
	if err != nil {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, err)
		return
	}
	if !catalog.HasBoss(payload.BossID) &&
		!a.account.TowerQuestHasBoss(payload.BossID) {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, errors.New("local team battle boss is unavailable"))
		return
	}
	battleEnemyTypes, found := a.account.ActiveTeamBattleEnemyTypes(payload.BossID)
	if !found {
		if payload.IsClear == 0 {
			// After server restart there is no live run to retire. The original
			// onTeamBattleSoloEnd clears its local save then returns to home for
			// a non-win with an empty score. Acknowledge without any mutation.
			a.writeProtocol(writer, map[string]any{"is_clear": 0, "score": []any{}, "user": a.userPayload()})
		} else {
			// SV_ERR.TEAMBATTLE_REWARD_NOT_FOUND. The original callback clears
			// the pending local save before showing this business error.
			a.writeExpiredBattleReport(writer)
		}
		return
	}
	if err := validateNativeTeamBattleReport(
		payload.Progress,
		payload.IsClear != 0,
		payload.InputCommand,
		payload.EnemyDeadBit,
		battleEnemyTypes,
	); err != nil {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, err)
		return
	}
	report := game.TeamBattleDropReport{SoloChallenge: true, EnemyDeadBits: payload.EnemyDeadBit, Turns: nativeScoreTurns(payload.InputCommand)}
	if payload.ScoreDamage != nil {
		report.ScoreVerified, report.ScoreDamage = true, *payload.ScoreDamage
	}
	settlement, err := a.account.CompleteTeamBattle(
		payload.BossID,
		payload.IsClear != 0,
		a.initialState.TeamBattleRewards,
		report,
	)
	if err != nil {
		a.rejectTeamBattleSoloReport(writer, payload.BossID, err)
		return
	}
	partnerUserIDs := make([]int, 0, len(settlement.Context.SelectedPartners))
	for _, partner := range settlement.Context.SelectedPartners {
		if !partner.IsSelf {
			partnerUserIDs = append(partnerUserIDs, partner.UserID)
		}
	}
	friendStates, err := a.friendPointAccountStates(partnerUserIDs)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "load result partner friend states")
		return
	}
	partners, err := teamBattleSoloResultPartners(settlement.Context.SelectedPartners, friendStates)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	snapshot := settlement.ResultSnapshot(int(payload.IsClear), 1, partners)
	encodedSnapshot, err := json.Marshal(snapshot)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode solo team battle settlement snapshot")
		return
	}
	if err := a.account.RecordTeamBattleSoloResultReceipt(requestDigest, payload.BossID, encodedSnapshot, time.Now()); err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	commit := game.SoloBattleCommit{BossID: payload.BossID, ReceiptSHA: requestDigest, Score: settlement.RankingScore}
	if payload.IsClear != 0 {
		commit.RentalEventKey, commit.RentalCredits = settlement.Context.FriendPointRentalEventKey, settlement.Context.FriendPointRentalCredits
		if len(settlement.Context.ClearDecks) != 0 {
			commit.Clear = &gamestate.BattleClearRecord{BossID: payload.BossID, UserID: a.initialState.User.UserID, Mode: 0,
				CompletedAt: time.Now().Unix(), EventKey: settlement.Context.FameSeed, Decks: settlement.Context.ClearDecks}
		}
	}
	if a.battleHistory != nil {
		if err := a.battleHistory.PersistSoloBattle(a.account.Snapshot(a.initialState), commit); err != nil {
			a.logger.Error("persist solo battle outcome", "error", err)
			writeError(writer, http.StatusInternalServerError, "persist solo battle result")
			return
		}
	} else if len(commit.RentalCredits) != 0 || commit.Score != nil {
		writeError(writer, http.StatusInternalServerError, "solo settlement repository is unavailable")
		return
	} else if !a.persistOrError(writer) {
		return
	}
	a.writeTeamBattleResultSnapshot(writer, encodedSnapshot, false)
}

func (a *API) rejectTeamBattleSoloReport(writer http.ResponseWriter, bossID int, err error) {
	a.logger.Warn("reject native solo battle report", "boss_id", bossID, "error", err)
	// The original result callback clears its pending local report on a
	// protocol response. HTTP 400 instead traps every subsequent login in
	// the same retry, before HomeShow can retire this run. Persistence errors
	// remain retryable and never pass through this business rejection path.
	a.writeExpiredBattleReport(writer)
}

// validateNativeTeamBattleReport enforces the wire shape produced by the CN
// client's battle5_api_input_cmd_get. The native verifier replays these records
// against the same seed, decks and master data and additionally checks HP/CRC;
// the Go server deliberately does not duplicate that engine. The managed CN
// client allocates both report arrays from TeamBattleSoloStart.battles.Length,
// so validation is driven by the exact segment contract stored at start.
func validateNativeTeamBattleReport(
	progress int,
	isClear bool,
	inputCommands []string,
	enemyDeadBits []int,
	enemyTypes []int8,
) error {
	if len(enemyTypes) == 0 || len(enemyTypes) > game.MaxTeamBattleSegments ||
		len(inputCommands) != len(enemyTypes) || len(enemyDeadBits) != len(enemyTypes) {
		return errors.New("local team battle report does not match the started battle count")
	}
	if isClear {
		if progress != len(enemyTypes) {
			return errors.New("clear result does not reach the final battle segment")
		}
		for index := range enemyTypes {
			if err := game.ValidateNativeTeamBattleCompletedSegment(
				inputCommands[index], enemyDeadBits[index], enemyTypes[index] == 4,
			); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		}
		return nil
	}
	if progress < 0 || progress > len(enemyTypes) {
		return errors.New("non-clear result advanced beyond the active battle segment")
	}
	// ResultMgr sends raw battle_idx. A normal defeat has already called
	// battleEnd and advanced it; manual retire bypasses battleEnd. The latter
	// may retain a nonempty report from a previous Continue in this same wave.
	// Accept either original envelope, keeping prior completed and future empty
	// segments checked. Neither non-clear form grants rewards.
	if progress > 0 {
		err := validateNativeTeamBattleUnfinishedSegments(progress-1, inputCommands, enemyDeadBits, enemyTypes)
		if err == nil || progress == len(enemyTypes) {
			return err
		}
	}
	return validateNativeTeamBattleUnfinishedSegments(progress, inputCommands, enemyDeadBits, enemyTypes)
}

func validateNativeTeamBattleUnfinishedSegments(activeIndex int, inputCommands []string, enemyDeadBits []int, enemyTypes []int8) error {
	for index := range enemyTypes {
		command := strings.TrimSpace(inputCommands[index])
		if enemyDeadBits[index] < 0 {
			return fmt.Errorf("battle segment %d: local team battle enemy state is invalid", index)
		}
		switch {
		case index < activeIndex:
			if err := game.ValidateNativeTeamBattleCompletedSegment(
				command, enemyDeadBits[index], enemyTypes[index] == 4,
			); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		case index == activeIndex:
			if err := game.ValidateNativeBattleCommand(command, enemyDeadBits[index] != 0); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		default:
			if command != "" || enemyDeadBits[index] != 0 {
				return fmt.Errorf("battle segment %d contains future battle state", index)
			}
		}
	}
	return nil
}

func (a *API) teamBattleMultiRoomSearch(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		DeckArthurType      int8   `json:"deck_arthur_type"`
		DeckArthurTypeIndex int8   `json:"deck_arthur_type_idx"`
		Password            string `json:"pass"`
		IsRookie            int8   `json:"is_rookie"`
		BossID              int    `json:"bossid"`
		BossGroupID         int    `json:"boss_groupid"`
		RookieType          int8   `json:"rookie_type"`
		SearchID            int    `json:"searchid"`
		QuestGetTime        int    `json:"quest_get_time"`
		IsAuto              int8   `json:"is_auto"`
		EmptyTime           int    `json:"empty_time"`
	}
	fields := []string{
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"pass",
		"is_rookie",
		"bossid",
		"boss_groupid",
		"rookie_type",
		"searchid",
		"quest_get_time",
		"is_auto",
		"empty_time",
	}
	if err := decodeExact(request, fields, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.DeckArthurType < 1 || payload.DeckArthurType > 4 ||
		payload.DeckArthurTypeIndex < 0 || payload.BossGroupID < 0 ||
		payload.SearchID < 0 || payload.QuestGetTime < 0 || payload.EmptyTime < 0 ||
		(payload.IsRookie != 0 && payload.IsRookie != 1) ||
		(payload.IsAuto != 0 && payload.IsAuto != 1) {
		writeError(writer, http.StatusBadRequest, "invalid local team battle room query")
		return
	}
	// The native quick-room entry sends bossid=0 to request every open room.
	// Positive values remain an exact official boss filter.
	if payload.BossID < 0 {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}
	catalog, err := a.account.TeamBattleCatalog()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	if payload.BossID > 0 && !catalog.HasBoss(payload.BossID) {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}

	rooms, err := a.teamBattleMultiRoomWires(multiplayer.RoomSearch{
		BossID: payload.BossID, BossGroupID: payload.BossGroupID, Password: payload.Password,
		UserID: a.initialState.User.UserID, ArthurType: int(payload.DeckArthurType),
	}, payload.SearchID)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	groups, err := teamBattleMultiGroups(catalog)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	reserveRoomID := int64(0)
	reserveLimitTime := int64(0)
	if a.multiplayer != nil {
		if roomID, limit, found := a.multiplayer.ReservationFor(a.initialState.User.UserID); found {
			reserveRoomID = roomID
			reserveLimitTime = limit
		}
	}
	a.writeProtocol(writer, map[string]any{
		"rooms":                  rooms,
		"search_datas":           []map[string]any{{"searchid": 0, "name": "全部"}},
		"normal_groups":          groups["normal_groups"],
		"special_groups":         groups["special_groups"],
		"key_groups":             groups["key_groups"],
		"event_groups":           groups["event_groups"],
		"reserve_roomid":         reserveRoomID,
		"reserve_limit_time":     reserveLimitTime,
		"retry_interval":         0,
		"auto_search_time":       0,
		"punished_left_sec":      0,
		"punished_host_left_sec": 0,
	})
}

func (a *API) teamBattleMultiRoomWires(
	query multiplayer.RoomSearch,
	searchID int,
) ([]any, error) {
	rooms := make([]any, 0)
	if a.multiplayer == nil {
		return rooms, nil
	}
	snapshots := a.multiplayer.List(query)
	ownerUserIDs := make([]int, 0, len(snapshots))
	for _, room := range snapshots {
		for _, member := range room.Members {
			if member.MemberType == room.OwnerMemberType {
				ownerUserIDs = append(ownerUserIDs, member.UserID)
				break
			}
		}
	}
	friendStates, err := a.friendPointAccountStates(ownerUserIDs)
	if err != nil {
		return nil, fmt.Errorf("load room owner friend states: %w", err)
	}
	for _, room := range snapshots {
		ownerUserID := 0
		for _, member := range room.Members {
			if member.MemberType == room.OwnerMemberType {
				ownerUserID = member.UserID
				break
			}
		}
		friendState := friendStates[ownerUserID]
		if !multiplayerRoomAllowsVisitor(room.RoomType, friendState) {
			continue
		}
		wire, err := multiplayerRoomWire(
			room,
			a.account.FriendPointRewardForState(friendState),
			friendState,
		)
		if err != nil {
			return nil, err
		}
		if searchID > 0 && wire["boss_group"].(map[string]any)["searchid"] != float64(searchID) {
			continue
		}
		rooms = append(rooms, wire)
	}
	return rooms, nil
}

func (a *API) teamBattleSoloPartnerShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID int `json:"bossid"`
	}
	if err := decodeExact(request, []string{"bossid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	catalog, err := a.account.TeamBattleCatalog()
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !catalog.HasBoss(payload.BossID) {
		writeError(writer, http.StatusBadRequest, "unknown team battle boss")
		return
	}

	arthurs, err := a.localTeamBattlePartnerArthurs()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, map[string]any{"arthurs": arthurs})
}

func (a *API) localTeamBattlePartnerArthurs() ([]any, error) {
	partnerBuckets := make(map[int8][]any, 4)
	preferredBuckets := make(map[int8][]any, 4)
	systemFallbacks := make(map[int8]any, 4)
	externalViews, err := a.friendPointPartnerViews()
	if err != nil {
		return nil, fmt.Errorf("list local-account team battle partners: %w", err)
	}
	for _, view := range externalViews {
		partnerReward := a.account.FriendPointRewardForState(view.FriendState)
		partner, err := view.listWire(partnerReward, view.FriendState)
		if err != nil {
			return nil, err
		}
		if view.System {
			systemFallbacks[view.ArthurType] = partner
		} else if view.FriendState == game.FriendStateFriend || view.FriendState == game.FriendStateFollow {
			preferredBuckets[view.ArthurType] = append(preferredBuckets[view.ArthurType], partner)
		} else {
			partnerBuckets[view.ArthurType] = append(partnerBuckets[view.ArthurType], partner)
		}
	}
	arthurs := make([]any, 0, 4)
	for arthurType := int8(1); arthurType <= 4; arthurType++ {
		// Preserve variety within each tier, but never shuffle strangers ahead
		// of a usable friend or followed player's deck.
		for _, bucket := range [][]any{preferredBuckets[arthurType], partnerBuckets[arthurType]} {
			rand.Shuffle(len(bucket), func(left, right int) {
				bucket[left], bucket[right] = bucket[right], bucket[left]
			})
		}
		partnerBuckets[arthurType] = append(preferredBuckets[arthurType], partnerBuckets[arthurType]...)
		// The recent quota is global. Only an empty profession needs a system
		// helper; a nonempty one is never padded to a target recommendation count.
		if len(partnerBuckets[arthurType]) == 0 {
			if fallback, found := systemFallbacks[arthurType]; found {
				partnerBuckets[arthurType] = append(partnerBuckets[arthurType], fallback)
			}
		}
		if len(partnerBuckets[arthurType]) == 0 {
			return nil, fmt.Errorf("team battle partner profession %d has no persistent candidate", arthurType)
		}
		arthurs = append(arthurs, map[string]any{
			"partners":    partnerBuckets[arthurType],
			"arthur_type": arthurType,
		})
	}
	return arthurs, nil
}

func teamBattlePartnerArthursNumeric(arthurs []any) ([]any, error) {
	numeric := make([]any, 0, len(arthurs))
	for _, arthurValue := range arthurs {
		arthur, ok := arthurValue.(map[string]any)
		if !ok {
			return nil, errors.New("team battle partner Arthur projection is invalid")
		}
		partners, ok := arthur["partners"].([]any)
		if !ok || len(partners) == 0 {
			return nil, errors.New("team battle partner projection is invalid")
		}
		numericPartners := make([]any, 0, len(partners))
		for _, partnerValue := range partners {
			partner, ok := partnerValue.(map[string]any)
			if !ok {
				return nil, errors.New("team battle partner projection is invalid")
			}
			leader, ok := partner["leader_card"].(map[string]any)
			if !ok {
				return nil, errors.New("team battle partner leader projection is invalid")
			}
			numericLeader := map[string]any{
				"0": leader["cardid"], "1": leader["lv"], "2": leader["skill_lv"],
				"3": leader["love"], "4": leader["hp"], "5": leader["atkp"],
				"6": leader["intp"], "7": leader["mndp"], "8": leader["fame"],
			}
			numericPartners = append(numericPartners, map[string]any{
				"0": partner["userid"], "1": partner["name"], "2": partner["lv"],
				"3": partner["arthur_type"], "4": partner["is_burst"], "5": partner["job_type"],
				"6": partner["hp"], "7": partner["atkp"], "8": partner["intp"],
				"9": partner["mndp"], "10": partner["deck_rank"], "11": numericLeader,
				"12": partner["get_fp"], "13": partner["comment"], "14": partner["friend_state"],
				"15": partner["rookie_type"], "16": partner["deck_name"], "17": partner["rental_idx"],
				"18": partner["play_log_turn"], "19": partner["attr_nums"], "20": partner["kind_nums"],
				"21": partner["deck_honorids"],
			})
		}
		numeric = append(numeric, map[string]any{
			"0": numericPartners,
			"1": arthur["arthur_type"],
		})
	}
	if len(numeric) != 4 {
		return nil, errors.New("team battle partner Arthur coverage is incomplete")
	}
	return numeric, nil
}

func (a *API) teamBattleSoloPartnerRentalDeck(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UserID int `json:"userid"`
	}
	if err := decodeExact(request, []string{"userid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	views, err := a.friendPointPartnerViewsForUsers([]int{payload.UserID})
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "list persistent team battle partners")
		return
	}
	view, found := findFriendPointPartnerView(views, payload.UserID)
	if !found {
		writeError(writer, http.StatusBadRequest, "unknown persistent team battle partner")
		return
	}
	partnerDecks, err := view.rentalDeckWires()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, map[string]any{"partner_deck": a.partnerDeckParameterViews(partnerDecks)})
}

func teamBattleSoloWithRuntimeState(
	configuration json.RawMessage,
	status game.BattlePointStatus,
	medalCount int,
	alchemistGroups []map[string]any,
) (json.RawMessage, error) {
	// These balances come from the account, not the request. Project them as-is;
	// recovery items can raise BP above its natural regeneration cap.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, fmt.Errorf("decode local team battle point response: %w", err)
	}
	current, err := json.Marshal(status.Current)
	if err != nil {
		return nil, fmt.Errorf("encode local team battle current points: %w", err)
	}
	maximum, err := json.Marshal(status.Max)
	if err != nil {
		return nil, fmt.Errorf("encode local team battle maximum points: %w", err)
	}
	top["0"] = current
	top["1"] = maximum
	medals, err := json.Marshal(medalCount)
	if err != nil {
		return nil, fmt.Errorf("encode local team battle medal count: %w", err)
	}
	top["8"] = medals
	roots, err := json.Marshal(alchemistGroups)
	if err != nil {
		return nil, err
	}
	top["alchemist_groups"] = roots
	updated, err := json.Marshal(top)
	if err != nil {
		return nil, fmt.Errorf("encode local team battle point response: %w", err)
	}
	return updated, nil
}

func battleResultRewardsWire(rewards []game.ReceivedReward) []any {
	result := make([]any, 0, len(rewards))
	for _, received := range rewards {
		result = append(result, map[string]any{
			"reward":           received.Reward,
			"uniqid":           append([]int64(nil), received.UniqueID...),
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		})
	}
	return result
}

func battleFameRewardsWire(awards []game.TeamBattleFameAward) []any {
	result := make([]any, 0, len(awards))
	for _, award := range awards {
		if len(award.Result.Rewards) != 1 || award.MemberType < 1 || award.MemberType > 4 ||
			(award.RewardKind != game.TeamBattleRewardFame && award.RewardKind != game.TeamBattleRewardFullFame) {
			continue
		}
		reward := battleResultRewardsWire(award.Result.Rewards)
		result = append(result, map[string]any{
			"result_reward":             reward[0],
			"member_type":               award.MemberType,
			"ex_teambattle_reward_type": award.RewardKind,
		})
	}
	return result
}

func battleHostRewardsWire(awards []game.TeamBattleFameAward) []any {
	result := make([]any, 0, len(awards))
	for _, award := range awards {
		if len(award.Result.Rewards) != 1 || award.MemberType < 1 || award.MemberType > 4 ||
			award.RewardKind != game.TeamBattleRewardHost {
			continue
		}
		reward := battleResultRewardsWire(award.Result.Rewards)
		result = append(result, map[string]any{
			"result_reward":             reward[0],
			"member_type":               award.MemberType,
			"ex_teambattle_reward_type": award.RewardKind,
		})
	}
	return result
}

func teamBattleReplayForBoss(
	replays []gamestate.TeamBattleReplay,
	bossID int,
) (gamestate.TeamBattleReplay, bool) {
	for _, replay := range replays {
		if replay.BossID == bossID {
			return replay, true
		}
	}
	return gamestate.TeamBattleReplay{}, false
}

func teamBattleReplayBattles(replay gamestate.TeamBattleReplay) ([]gamestate.TeamBattleReplayBattle, error) {
	if replay.EnemyPartyID <= 0 || replay.EnemyType < 0 || replay.EnemyType > 4 {
		return nil, errors.New("team battle replay legacy segment is invalid")
	}
	if len(replay.Battles) == 0 {
		return []gamestate.TeamBattleReplayBattle{{
			EnemyPartyID: replay.EnemyPartyID,
			EnemyType:    replay.EnemyType,
		}}, nil
	}
	if len(replay.Battles) > game.MaxTeamBattleSegments {
		return nil, errors.New("team battle replay has too many battle segments")
	}
	if replay.Battles[0].EnemyPartyID != replay.EnemyPartyID ||
		replay.Battles[0].EnemyType != replay.EnemyType {
		return nil, errors.New("team battle replay first segment does not mirror its legacy projection")
	}
	segments := append([]gamestate.TeamBattleReplayBattle(nil), replay.Battles...)
	for _, segment := range segments {
		if segment.EnemyPartyID <= 0 || segment.EnemyType < 0 || segment.EnemyType > 4 {
			return nil, errors.New("team battle replay segment is invalid")
		}
	}
	return segments, nil
}

func teamBattleEnemyTypes(segments []gamestate.TeamBattleReplayBattle) []int8 {
	enemyTypes := make([]int8, len(segments))
	for index, segment := range segments {
		enemyTypes[index] = segment.EnemyType
	}
	return enemyTypes
}

var teamBattlePartnerDeckWire = game.PartnerDeckWire

func selectCompletePartnerDeck(
	decks []game.DeckInfo,
	arthurType int8,
	cards map[int64]game.CardInfo,
) (game.DeckInfo, bool) {
	const battleDeckCardCount = 10
	complete := func(deck game.DeckInfo) bool {
		if deck.ArthurType != arthurType || len(deck.CardUniqueIDs) != battleDeckCardCount {
			return false
		}
		for _, uniqueID := range deck.CardUniqueIDs {
			if uniqueID <= 0 {
				return false
			}
			if _, found := cards[uniqueID]; !found {
				return false
			}
		}
		return true
	}
	for _, deck := range decks {
		if deck.IsActive != 0 && complete(deck) {
			return deck, true
		}
	}
	for _, deck := range decks {
		if complete(deck) {
			return deck, true
		}
	}
	return game.DeckInfo{}, false
}

func partnerCardWire(card game.CardInfo) map[string]any { return game.PartnerCardWire(card) }
