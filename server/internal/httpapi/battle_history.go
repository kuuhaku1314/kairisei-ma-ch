package httpapi

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (a *API) teamBattleClearDeckShow(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		BossID int `json:"bossid"`
	}
	if err := decodeExact(r, []string{"bossid"}, &payload); err != nil {
		a.writeStoreError(w, err)
		return
	}
	if !a.validBattleHistoryRequest(w, payload.BossID) {
		return
	}
	records, err := a.battleHistory.RecentBattleClears(payload.BossID, time.Now())
	if err != nil {
		a.writeStoreError(w, err)
		return
	}
	// Each multi participant may have a daily row for the same battle.
	// Sample distinct recent clears, keeping each actual party together.
	seen := map[string]bool{}
	unique := records[:0]
	for _, record := range records {
		if !seen[record.EventKey] && len(record.Decks) > 0 {
			seen[record.EventKey] = true
			unique = append(unique, record)
		}
	}
	rand.Shuffle(len(unique), func(i, j int) { unique[i], unique[j] = unique[j], unique[i] })
	decks := []gamestate.BattleClearDeck{}
	for _, record := range unique[:min(10, len(unique))] {
		decks = append(decks, record.Decks...)
	}
	if len(decks) == 0 {
		a.writeProtocolResult(w, map[string]any{}, -1, "暂无该副本的近期通关卡组，请在成功通关后再查看。")
		return
	}
	how, states, err := a.clearDeckMetadata(r, decks)
	if err != nil {
		a.writeStoreError(w, err)
		return
	}
	wire := make([]any, 0, len(decks))
	for _, deck := range decks {
		wire = append(wire, a.clearDeckParameterView(deck))
	}
	a.writeProtocol(w, map[string]any{"partner_deck": wire, "how_to_list": how, "friend_state_list": states})
}

func (a *API) dailyClearRankShow(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		BossID  int `json:"bossid"`
		IsMulti int `json:"is_multi"`
	}
	if err := decodeExact(r, []string{"bossid", "is_multi"}, &payload); err != nil {
		a.writeStoreError(w, err)
		return
	}
	if payload.IsMulti != 0 && payload.IsMulti != 1 {
		writeError(w, http.StatusBadRequest, "invalid daily-clear ranking mode")
		return
	}
	if !a.validBattleHistoryRequest(w, payload.BossID) {
		return
	}
	records, err := a.battleHistory.YesterdayBattleRanks(payload.BossID, payload.IsMulti, time.Now())
	if err != nil {
		a.writeStoreError(w, err)
		return
	}
	if len(records) == 0 {
		a.writeProtocolResult(w, map[string]any{}, -1, "昨日暂无该副本的通关排行，新记录将于次日显示。")
		return
	}
	buckets := [4][]any{}
	for i := range buckets {
		buckets[i] = []any{}
	}
	var positions, counts, ranks [4]int
	decks := []gamestate.BattleClearDeck{}
	for _, record := range records {
		bucket := 0
		if payload.IsMulti == 1 {
			bucket = record.ArthurType - 1
		}
		if bucket < 0 || bucket > 3 {
			continue
		}
		positions[bucket]++
		if counts[bucket] != record.Count {
			ranks[bucket] = positions[bucket]
			counts[bucket] = record.Count
		}
		for index, deck := range record.Decks {
			rowIndex := index
			if payload.IsMulti == 1 {
				if deck.UserID != record.UserID || deck.ArthurType != record.ArthurType {
					continue
				}
				rowIndex = bucket
			}
			name := deck.Name
			if payload.IsMulti == 0 {
				name = record.Decks[0].Name
			}
			buckets[rowIndex] = append(buckets[rowIndex], map[string]any{"userid": record.UserID, "name": name, "rank": ranks[bucket],
				"partner_deck": a.clearDeckParameterView(deck), "deck_honorids": deck.HonorIDs})
			decks = append(decks, deck)
		}
	}
	how, states, err := a.clearDeckMetadata(r, decks)
	if err != nil {
		a.writeStoreError(w, err)
		return
	}
	wire := make([]any, 4)
	for i, bucket := range buckets {
		wire[i] = map[string]any{"ranks": bucket}
	}
	a.writeProtocol(w, map[string]any{"deck": wire, "how_to_list": how, "friend_state_list": states})
}

func (a *API) validBattleHistoryRequest(w http.ResponseWriter, bossID int) bool {
	if _, found := teamBattleReplayForBoss(a.initialState.TeamBattleReplays, bossID); !found {
		writeError(w, http.StatusBadRequest, "unknown battle boss")
		return false
	}
	if a.battleHistory == nil {
		a.writeProtocolResult(w, map[string]any{}, -1, "暂无通关记录。")
		return false
	}
	return true
}

func (a *API) clearDeckMetadata(r *http.Request, decks []gamestate.BattleClearDeck) ([]any, []any, error) {
	users := map[int]bool{}
	cards := map[int]bool{}
	for _, deck := range decks {
		users[deck.UserID] = true
		var wire struct {
			Deck []struct {
				ID int `json:"cardid"`
			} `json:"deck"`
			Support []struct {
				ID int `json:"cardid"`
			} `json:"support_deck"`
		}
		if err := json.Unmarshal(deck.Deck, &wire); err != nil {
			return nil, nil, err
		}
		for _, card := range append(wire.Deck, wire.Support...) {
			if card.ID > 0 {
				cards[card.ID] = true
			}
		}
	}
	userIDs := make([]int, 0, len(users))
	for id := range users {
		userIDs = append(userIDs, id)
	}
	sort.Ints(userIDs)
	relations, err := a.friendPointAccountStates(userIDs)
	if err != nil {
		return nil, nil, err
	}
	states := make([]any, 0, len(userIDs))
	for _, id := range userIDs {
		state := relations[id]
		if id == a.initialState.User.UserID {
			state = 4
		}
		states = append(states, map[string]any{"userid": id, "friend_state": state})
	}
	cardIDs := make([]int, 0, len(cards))
	for id := range cards {
		cardIDs = append(cardIDs, id)
	}
	sort.Ints(cardIDs)
	policy := cardSourcePolicy(r)
	how := []any{}
	for start := 0; start < len(cardIDs); start += 64 {
		lists, err := a.account.HowToGetMissingCards(cardIDs[start:min(start+64, len(cardIDs))], a.initialState.TeamBattleRewards, policy)
		if err != nil {
			return nil, nil, err
		}
		for _, list := range lists {
			how = append(how, list)
		}
	}
	return how, states, nil
}
