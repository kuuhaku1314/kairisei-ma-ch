package masterdata_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func cardRefreshJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCardMasterRefreshPreservesOwnedProgress(t *testing.T) {
	state := testfixture.RuntimeState(t)
	master, err := masterdata.LoadCardRuntimeMaster(testfixture.WriteTestCardMaster(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	card := &state.Cards[0]
	card.Level, card.Experience = 3, 25
	card.Love, card.Fame, card.IsLock, card.CreateTime = 800, 7, 1, 12345
	card.HP = 0 // Derived values must refresh from the current definitions.
	before := *card
	decks := cardRefreshJSON(t, state.Decks)
	support := cardRefreshJSON(t, state.SupportDeck)
	state.SupportDeck.CardCollectionIDs = append(state.SupportDeck.CardCollectionIDs, card.CardID)
	if changed, err := masterdata.ApplyCardRuntimeMaster(&state, master); err != nil || !changed {
		t.Fatalf("refresh: changed=%v, err=%v", changed, err)
	}
	after := state.Cards[0]
	if after.UniqueID != before.UniqueID || after.CardID != before.CardID ||
		after.Level != 3 || after.Experience != 25 || after.NowLevelExperience != 5 || after.NextLevelExperience != 5 ||
		after.Love != before.Love || after.Fame != before.Fame || after.IsLock != before.IsLock ||
		after.CreateTime != before.CreateTime || !reflect.DeepEqual(after.SkillLevels, before.SkillLevels) || after.HP <= 0 {
		t.Fatalf("owned progress changed or derived state not refreshed: %+v", after)
	}
	if string(decks) != string(cardRefreshJSON(t, state.Decks)) || string(support) != string(cardRefreshJSON(t, state.SupportDeck)) {
		t.Fatal("definition refresh changed deck selections, unlocks or collection history")
	}
	if changed, err := masterdata.ApplyCardRuntimeMaster(&state, master); err != nil || changed {
		t.Fatalf("repeat refresh: changed=%v, err=%v", changed, err)
	}
	state.Cards[0].Experience = -1
	if _, err := masterdata.ApplyCardRuntimeMaster(&state, master); err == nil {
		t.Fatal("negative owned experience accepted")
	}
}
