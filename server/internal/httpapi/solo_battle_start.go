package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"

	"kairisei.local/server/internal/game"
)

type soloBattleStartRequest struct {
	BossID              int                           `json:"bossid"`
	DeckArthurType      int8                          `json:"deck_arthur_type"`
	DeckArthurTypeIndex int8                          `json:"deck_arthur_type_idx"`
	PartnerDeckSelects  []teamBattlePartnerDeckSelect `json:"partner_deck_selects"`
}

func decodeSoloBattleStart(request *http.Request) (soloBattleStartRequest, error) {
	var result soloBattleStartRequest
	expected := []string{"bossid", "deck_arthur_type", "deck_arthur_type_idx"}
	if err := decodeExact(request, expected, &result, "partner_deck_selects"); err != nil {
		return result, err
	}
	return result, nil
}

func (a *API) beginSoloBattleEntry(request soloBattleStartRequest, entry game.SoloBattleEntry) (game.TeamBattleContext, game.BattlePointStatus, bool, error) {
	identity, _ := json.Marshal(struct {
		BossID   int
		Arthur   int8
		Deck     int8
		Partners []teamBattlePartnerDeckSelect
	}{request.BossID, request.DeckArthurType, request.DeckArthurTypeIndex, request.PartnerDeckSelects})
	entry.RequestKey = fmt.Sprintf("%x", sha256.Sum256(identity))
	return a.account.BeginSoloBattle(entry)
}
