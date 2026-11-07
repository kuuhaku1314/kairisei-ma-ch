package httpapi

import (
	"context"
	"net/http"

	"kairisei.local/server/internal/game"
)

type cardAcquisitionGroupsKey struct{}

// WithCardAcquisitionGroups uses the same current publication as the battle
// list adapter. nil means all activities; an empty map means none. Normal
// quests retain their account-scoped unlocks independently of operations.
func WithCardAcquisitionGroups(request *http.Request, groups map[int]struct{}) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), cardAcquisitionGroupsKey{}, groups))
}

func cardSourcePolicy(request *http.Request) game.CardSourcePolicy {
	groups, _ := request.Context().Value(cardAcquisitionGroupsKey{}).(map[int]struct{})
	publication, _ := request.Context().Value(gachaPublicationKey{}).(func(int) bool)
	return game.CardSourcePolicy{BattleGroups: groups, GachaPublished: publication}
}
