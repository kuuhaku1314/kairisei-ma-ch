package httpapi

import (
	"reflect"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestSharedCardCatalogKeepsPlayerStateAndSnapshotIsolated(t *testing.T) {
	state := testfixture.RuntimeState(t)
	template := state.CardTemplates[0]
	state.Engagement.Presents = []gamestate.Present{{
		PresentID: 900001, Title: "shared catalog grant", Reward: gamestate.Reward{
			Type: 6, RewardTypeID: template.CardID, Num: 1, CardFame: 1, CardSkillLevels: []int16{1},
		},
	}}
	catalog, err := game.NewCardCatalog(state)
	if err != nil {
		t.Fatal(err)
	}
	// The catalog owns its inputs; a caller editing its fixture must not edit
	// definitions already shared by other handlers.
	state.CardTemplates[0].LevelMax++
	if len(state.CardTemplates[0].SkillLevels) > 0 {
		state.CardTemplates[0].SkillLevels[0]++
	}
	newAPI := func() *API {
		t.Helper()
		h, err := New(Config{InitialState: state, BaseURL: "http://127.0.0.1", CardCatalog: catalog})
		if err != nil {
			t.Fatal(err)
		}
		return h.(*accountBusinessHandler).api
	}
	a, b := newAPI(), newAPI()
	if len(a.initialState.Cards)+len(a.initialState.ContainerCards)+len(a.initialState.Decks)+
		len(a.initialState.Engagement.Presents) != 0 {
		t.Fatal("handler retained load-time private collections")
	}
	if &a.initialState.CardTemplates[0] != &b.initialState.CardTemplates[0] {
		t.Fatal("handlers did not reuse the shared master")
	}
	before := b.account.Snapshot(b.initialState)
	result, err := a.account.ReceivePresent(900001)
	if err != nil || len(result.Cards) != 1 || result.Cards[0].LevelMax != template.LevelMax {
		t.Fatalf("grant from immutable template: %+v, %v", result, err)
	}
	if err := a.account.SetCardLock(result.Cards[0].UniqueID, 0, true); err != nil {
		t.Fatal(err)
	}
	saved := a.account.Snapshot(a.initialState)
	// Compact context and the former full base must reconstruct identical saves.
	fullBase := state
	catalog.ApplyDefinitions(&fullBase)
	if !reflect.DeepEqual(saved, a.account.Snapshot(fullBase)) {
		t.Fatal("dropping load-time inventory changed the saved state")
	}
	if levels := saved.Cards[len(saved.Cards)-1].SkillLevels; len(levels) > 0 {
		levels[0]++
	}
	if !reflect.DeepEqual(before, b.account.Snapshot(b.initialState)) {
		t.Fatal("grant/lock/save in one account changed another")
	}
	other, err := b.account.ReceivePresent(900001)
	if err != nil || len(other.Cards) != 1 || other.Cards[0].IsLock != 0 ||
		!reflect.DeepEqual(other.Cards[0].SkillLevels, result.Cards[0].SkillLevels) {
		t.Fatalf("second account received a mutated template: %+v, %v", other, err)
	}
	if _, err := game.New(a.account.Snapshot(a.initialState), game.WithCardCatalog(catalog)); err != nil {
		t.Fatalf("reconstruct saved account using shared catalog: %v", err)
	}
}
