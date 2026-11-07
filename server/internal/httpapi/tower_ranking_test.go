package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

type towerRankingFunc func(int, int, int, int, int) (game.TowerRankingPage, error)

func (f towerRankingFunc) TowerRanking(tower, floors, user, offset, limit int) (game.TowerRankingPage, error) {
	return f(tower, floors, user, offset, limit)
}

func TestTowerRankingNamedConsumerContractAndEmptyBoard(t *testing.T) {
	profile := gamestate.TowerQuestProfile{TowerID: 7, Name: "Tower", LoseCountMax: 4,
		Ranks:  []gamestate.TowerQuestRankProfile{{Rank: 1, RankName: "序章"}, {Rank: 2, RankName: "挑战"}},
		Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Rank: 1}, {Floor: 2, Rank: 2}, {Floor: 3, Rank: 2}, {Floor: 4, Rank: 2}}}
	account := testAccount(t, func(s *gamestate.State) {
		s.TowerQuestProfiles = []gamestate.TowerQuestProfile{profile}
		s.TowerQuestProgress = []gamestate.TowerQuestProgress{{TowerID: 7, Floor: 1}}
	})
	const selfID = 1000001
	a := &API{account: account, initialState: gamestate.State{User: gamestate.User{UserID: selfID}}}
	page := game.TowerRankingPage{Entries: []game.TowerRankRecord{}}
	calls := 0
	a.towerRankings = towerRankingFunc(func(tower, floors, user, offset, limit int) (game.TowerRankingPage, error) {
		calls++
		if tower != 7 || floors != 4 || user != selfID || offset != 0 || limit != 10 {
			t.Fatal("request boundary changed")
		}
		return page, nil
	})
	call := func() (map[string]json.RawMessage, []byte) {
		t.Helper()
		w := httptest.NewRecorder()
		a.towerRankingShow(w, httptest.NewRequest(http.MethodPost, "/TowerRankingShow", strings.NewReader(`{"towerid":7}`)))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		if w.Code != 200 || len(lines) != 3 {
			t.Fatal(w.Code, w.Body.String())
		}
		var method map[string]json.RawMessage
		if err := json.Unmarshal([]byte(lines[1]), &method); err != nil {
			t.Fatal(err)
		}
		return method, []byte(lines[1])
	}
	method, _ := call()
	if len(method) != 1 || string(method["ranking"]) != "[]" {
		t.Fatal("empty board is not an empty named array", method)
	}
	for i := 1; i <= 10; i++ {
		page.Entries = append(page.Entries, game.TowerRankRecord{RankNumber: i, UserID: selfID + i, Level: 12,
			Name: "Other player", Floor: 3, Score: 5500, LeaderCardID: 10000010, LeaderCardLV: 50, LeaderCardLove: 100, LeaderCardFame: 90})
	}
	self := page.Entries[0]
	self.UserID, self.RankNumber, self.Name, self.Floor = selfID, 12, "Self", 2
	page.Self = &self
	method, body := call()
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(method["ranking"], &rows); err != nil || len(rows) != 11 {
		t.Fatal("top page or self missing", err)
	}
	keys := []string{"rank_number", "userid", "lv", "name", "rank", "floor", "score", "rank_name", "leader_cardid", "leader_card_lv", "leader_card_love", "leader_card_fame"}
	for _, row := range rows {
		if len(row) != len(keys) {
			t.Fatal("unexpected consumer fields", row)
		}
		for _, key := range keys {
			if row[key] == nil {
				t.Fatal("missing generated receiver field", key)
			}
		}
	}
	var decoded struct {
		Ranking []towerRankWire `json:"ranking"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil || decoded.Ranking[10].RankNumber != 12 ||
		decoded.Ranking[10].UserID != selfID || decoded.Ranking[10].Floor != 2 || decoded.Ranking[10].Rank != 2 || decoded.Ranking[10].RankName != "挑战" {
		t.Fatal("consumer row values changed", err)
	}
	if output := os.Getenv("TOWER_RANK_WIRE_OUTPUT"); output != "" {
		if err := os.WriteFile(output, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	account.ApplyTowerAdmission(game.TowerAdmission{TowerIDs: map[int]struct{}{}})
	w := httptest.NewRecorder()
	a.towerRankingShow(w, httptest.NewRequest(http.MethodPost, "/TowerRankingShow", strings.NewReader(`{"towerid":7}`)))
	if calls != 2 || !strings.Contains(w.Body.String(), "炼金副本暂未开放") {
		t.Fatal("closed tower reached ranking repository", w.Body.String())
	}
}
