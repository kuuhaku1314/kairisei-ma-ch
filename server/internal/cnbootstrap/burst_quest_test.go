package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

// Extends the existing optional production-data gate; no synthetic resource
// catalog, player database, device or network listener is involved.
func probeCompleteRuntimeBurst(t *testing.T, handler http.Handler, savePath, seedPath string, cards masterdata.CardRuntimeMaster, output string) {
	t.Helper()
	if !filepath.IsAbs(output) {
		t.Fatal("absolute probe output required")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	storage, err := accountstore.OpenDatabase(savePath, seedPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	accounts := testfixture.TestAccountRepository(t, storage)
	testfixture.AttachProbeCardCatalog(t, storage, cards)
	if _, err := accounts.ResolveInternalLogin("00000000-0000-0000-0002-000000000000"); err != nil {
		t.Fatal(err)
	}
	identity, err := accounts.ResolveInternalLogin("00000000-0000-0000-0002-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Onboarding.Step = gamestate.OnboardingStepCount
	state.User.UnlockedFeatureIDs = append(state.User.UnlockedFeatureIDs, 0, 1, 2, 3)
	state.User.Gold = 2000000
	state.Items = append(state.Items, gamestate.Item{ItemID: 1305, Num: 20})
	if err := accounts.PersistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	call := func(route, payload, name string, expected int) map[string]json.RawMessage {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey+payload)))
		if err := os.WriteFile(filepath.Join(output, name+".jsonl"), response.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		var common struct {
			Code int `json:"res_code"`
		}
		var method map[string]json.RawMessage
		if response.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || json.Unmarshal([]byte(lines[1]), &method) != nil || common.Code != expected {
			t.Fatalf("%s: HTTP %d %s", name, response.Code, response.Body.String())
		}
		return method
	}
	collection := call("/CardCollectionShow", "", "collection", 0)
	auto, err := accounts.LoadState(identity.UserID)
	if err != nil || len(auto.Buddies) != len(state.Buddies)+4 || auto.BurstProgress != [4]uint8{} {
		t.Fatal("completed training did not grant the four initial Buddies independently of learning progress")
	}
	for _, quest := range gamestate.BurstQuests() {
		if gamestate.ArthurBurstUnlocked(auto.User.UnlockedFeatureIDs, quest.ArthurType) != 1 {
			t.Fatal("training did not unlock a sword")
		}
		var id int64
		for _, buddy := range auto.Buddies {
			if buddy.BuddyID == quest.BuddyID {
				id = buddy.UniqueID
			}
		}
		for _, deck := range auto.Decks {
			if deck.ArthurType == quest.ArthurType && (len(deck.BuddyUniqueIDs) == 0 || deck.BuddyUniqueIDs[0] != id) {
				t.Fatal("initial sword was not bound to its profession deck")
			}
		}
	}
	var pages []struct {
		Cards []struct {
			ID    int `json:"0"`
			State int `json:"1"`
		} `json:"0"`
	}
	if json.Unmarshal(collection["0"], &pages) != nil || len(pages) != len(cards.CardCollectionPages) {
		t.Fatal("original collection pages missing")
	}
	total, acquired := 0, 0
	for _, page := range pages {
		if len(page.Cards) != 10 {
			t.Fatal("original client reads ten collection slots")
		}
		for _, card := range page.Cards {
			if card.ID != 0 {
				total++
			}
			if card.State&1 != 0 {
				acquired++
			}
		}
	}
	expectedTotal := 0
	for _, page := range cards.CardCollectionPages {
		for _, id := range page {
			if id != 0 {
				expectedTotal++
			}
		}
	}
	if total != expectedTotal || string(collection["2"]) != fmt.Sprint(total) || acquired == 0 || string(collection["1"]) != fmt.Sprint(acquired) || acquired == total {
		t.Fatal("collection progress differs from its catalog")
	}
	love := call("/CardLoveUp", fmt.Sprintf(`{"base_uniqid":%d,"use_items":[{"itemid":1305,"num":20}]}`, state.Cards[0].UniqueID), "love-max", 0)
	var grown struct {
		Love int `json:"love"`
	}
	if json.Unmarshal(love["base_card"], &grown) != nil || grown.Love != 10000 {
		t.Fatalf("original calcAddLove value*quantity: %s", love["base_card"])
	}
	call("/StoryTeamBattleEnd", "", "no-active-story", -6800)
	for _, quest := range gamestate.BurstQuests() {
		label := fmt.Sprint(quest.ArthurType)
		shown := call("/StorySubShow", `{"cn_story":0}`, label+"-show", 0)
		var characters []gamestate.StorySubCharacter
		if json.Unmarshal(shown["characters"], &characters) != nil {
			t.Fatal("invalid character story catalog")
		}
		found := false
		for _, character := range characters {
			if len(character.Sections) == 0 {
				t.Fatal("empty character row would crash the native unread counter")
			}
			for _, section := range character.Sections {
				for _, story := range section.Stories {
					if story.StorySubID == quest.StoryIDs[0] {
						found = story.StateFlag&1 != 0
					}
				}
			}
		}
		if !found {
			t.Fatal("learning story missing from its character")
		}

		for step, id := range quest.StoryIDs {
			prefix := fmt.Sprintf("%s-%d", label, step)
			call("/StorySubStart", fmt.Sprintf(`{"story_subid":%d}`, id), prefix+"-start", 0)
			// Fixed battles still run locally. Character stories now use their
			// normal start/end protocol and the existing learning progress.
			end := call("/StorySubEnd", `{"is_clear":1}`, prefix+"-end", 0)
			retry := call("/StorySubEnd", `{"is_clear":1}`, prefix+"-retry", 0)
			var repeated []json.RawMessage
			if json.Unmarshal(retry["clear_rewards"], &repeated) != nil || len(repeated) != 0 || bytes.Equal(end["clear_rewards"], retry["clear_rewards"]) {
				t.Fatal("learning repeat granted first-clear rewards")
			}
		}

		call("/StorySubStart", fmt.Sprintf(`{"story_subid":%d}`, quest.StoryIDs[2]), label+"-rewatch-start", 0)
		call("/StorySubEnd", `{"is_clear":1}`, label+"-rewatch-end", 0)
	}
	reading := call("/MainQuestShow", "", "reading-directory", 0)
	var sections []struct {
		Groups []struct {
			Stories []struct {
				ID int `json:"story_teambattleid"`
			} `json:"stories"`
		} `json:"boss_groups"`
	}
	if json.Unmarshal(reading["sections"], &sections) != nil {
		t.Fatal("invalid shared reading directory")
	}
	readIDs := []int{}
	for _, section := range sections {
		if len(section.Groups) == 0 || len(section.Groups[0].Stories) == 0 {
			continue
		}
		id := section.Groups[0].Stories[0].ID
		prefix := fmt.Sprintf("reading-%d", id)
		call("/StoryTeamBattleStart", fmt.Sprintf(`{"story_teambattleid":%d}`, id), prefix+"-start", 0)
		end := call("/StoryTeamBattleEnd", "", prefix+"-end", 0)
		retry := call("/StoryTeamBattleEnd", "", prefix+"-retry", 0)
		if !bytes.Equal(end["clear_rewards"], retry["clear_rewards"]) || string(end["burst_unlock"]) != "[]" {
			t.Fatal("reading retry did not preserve its original response")
		}
		call("/StoryTeamBattleStart", fmt.Sprintf(`{"story_teambattleid":%d}`, id), prefix+"-rewatch-start", 0)
		rewatch := call("/StoryTeamBattleEnd", "", prefix+"-rewatch-end", 0)
		if string(rewatch["clear_rewards"]) != "[]" {
			t.Fatal("reading rewatch granted duplicate rewards")
		}
		readIDs = append(readIDs, id)
	}
	if len(readIDs) == 0 {
		t.Fatal("the registered reading directory was not published")
	}
	// Inspect committed state through an independent repository lifetime.
	final, err := accounts.LoadPersistentState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if final.BurstProgress != [4]uint8{3, 3, 3, 3} || len(final.Buddies) != len(state.Buddies)+4 || final.StoryTeamBattleSession.StoryID != 0 || final.ActiveTeamBattle != nil {
		t.Fatal("persistent reward/progress or runtime boundary differs")
	}
	if len(final.Story.QuestClearedIDs) != len(readIDs) {
		t.Fatal("reading progress was not retained in normalized catalog progress")
	}
	// Published local policy: twelve first clears, fifty crystals each.
	expectedCoins := auto.User.CoinFree + 600 + 50*len(readIDs)
	if final.User.CoinFree != expectedCoins {
		t.Fatalf("first-clear crystals=%d, want %d without repeat rewards", final.User.CoinFree, expectedCoins)
	}
	if len(final.SupportDeck.CardCollectionLoveMaxIDs) != 1 || final.SupportDeck.CardCollectionLoveMaxIDs[0] != state.Cards[0].CardID || final.Cards[0].Love != 10000 || final.User.Gold != 1000000 {
		t.Fatal("loyalty value, item cost or full-love collection history was not committed")
	}
	for _, quest := range gamestate.BurstQuests() {
		if gamestate.ArthurBurstUnlocked(final.User.UnlockedFeatureIDs, quest.ArthurType) != 1 {
			t.Fatal("feature not committed")
		}
	}
	progress, err := json.Marshal(map[string]any{"progress": final.BurstProgress, "buddies": len(final.Buddies)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "progress.json"), progress, 0600); err != nil {
		t.Fatal(err)
	}
	t.Log("training auto-unlock, four character learning chains without StoryStart, duplicate End and independent SQLite reload passed")
}
