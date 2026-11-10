package admin

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
)

// Read-only complete-input audit. Accounts are local Go values, never DB rows.
func TestAlchemistInventoryCandidate(t *testing.T) {
	root := os.Getenv("KAIRI_ALCHEMIST_AUDIT_ROOT")
	if root == "" {
		t.Skip("complete alchemist candidate is not requested")
	}
	control := filepath.Join(root, "_local", "control", "server")
	path := filepath.Join(control, "cn602-card-runtime-master.json")
	master, err := masterdata.LoadCardRuntimeMaster(path)
	if err != nil {
		t.Fatal(err)
	}
	if master.AlchemistStarterDeck == nil {
		t.Fatal("starting policy is missing")
	}
	items, err := masterdata.LoadItemRuntimeMaster(filepath.Join(control, "cn602-item-runtime-master.json"))
	if err != nil {
		t.Fatal(err)
	}
	resources, err := loadAdminCardResources(master, path, filepath.Join(root, "asset-map.json"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, _, err := BuildAdminCatalog(master, items)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err = applyAdminCatalogAssetCoverage(catalog, filepath.Join(control, "cn602-admin-assets"), resources)
	if err != nil {
		t.Fatal(err)
	}
	details, err := multiplayer.LoadOperationsCardCatalog(filepath.Join(control, "cn602-card-master", "card.csv"), filepath.Join(control, "cn602-battle-master"), filepath.Join(control, "cn602-card-master", "card_alchemist.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := attachAdminCardDetails(catalog, master.CardTemplates, details); err != nil {
		t.Fatal(err)
	}
	alchemistCount := 0
	fixedLevelCount := 0
	for i := range catalog {
		if catalog[i].Kind != "card" {
			continue
		}
		detail, ok := details[catalog[i].RewardTypeID]
		if !ok {
			t.Fatalf("missing card details %d", catalog[i].RewardTypeID)
		}
		if catalog[i].ArthurType == 5 {
			alchemistCount++
			if detail.SummonID == 0 {
				fixedLevelCount++
				if catalog[i].FixedCardLevel != catalog[i].LevelMax {
					t.Fatalf("alchemist skill %d is not fixed at its maximum level", catalog[i].RewardTypeID)
				}
			} else if catalog[i].FixedCardLevel != 0 {
				t.Fatalf("alchemist summon %d has a fixed level", catalog[i].RewardTypeID)
			}
			if !resources[catalog[i].RewardTypeID].Ready {
				t.Fatal("alchemist picture source is missing")
			}
		}
	}
	if alchemistCount != 799 || fixedLevelCount != 44 {
		t.Fatalf("alchemist catalog = %d cards / %d fixed skills", alchemistCount, fixedLevelCount)
	}
	api := &API{catalog: catalog}
	response := httptest.NewRecorder()
	api.catalogEntries(response, httptest.NewRequest("GET", "/api/catalog?kind=card&arthur_type=5&rarity=3&limit_count=0&limit=1&offset=1", nil))
	var page struct {
		Total   int
		Entries []AdminCatalogEntry
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || page.Total == 0 || len(page.Entries) != 1 || page.Entries[0].ArthurType != 5 {
		t.Fatal(response.Body.String())
	}
	seed, err := os.ReadFile(filepath.Join("..", "..", "config", "cn602-save-template.json"))
	if err != nil {
		t.Fatal(err)
	}
	avatar, err := masterdata.LoadAvatarRuntimeMaster(filepath.Join(control, "cn602-avatar-runtime-master.json"))
	if err != nil {
		t.Fatal(err)
	}
	login, err := masterdata.LoadLoginBonusRuntimeMaster(filepath.Join("..", "..", "config", "cn602-login-bonus-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	newAccount := func(done bool, capacity int) (*game.Account, gamestate.State) {
		t.Helper()
		var state gamestate.State
		if err := json.Unmarshal(seed, &state); err != nil {
			t.Fatal(err)
		}
		if _, err := masterdata.ApplyCardRuntimeMaster(&state, master); err != nil {
			t.Fatal(err)
		}
		if _, err := masterdata.ApplyAvatarRuntimeMaster(&state, avatar); err != nil {
			t.Fatal(err)
		}
		if err := masterdata.ApplyItemRuntimeMaster(&state, items); err != nil {
			t.Fatal(err)
		}
		if _, err := masterdata.ApplyLoginBonusRuntimeMaster(&state, login); err != nil {
			t.Fatal(err)
		}
		state.Onboarding = gamestate.OnboardingState{ConfigVersion: masterdata.OnboardingConfigVersion}
		if done {
			state.Onboarding.Step = masterdata.OnboardingStepCount
		}
		state.User.CardMax = capacity
		definitions := &battlemodel.Definitions{Cards: make(map[int]battlemodel.CombatCardDefinition), Summons: make(map[int]battlemodel.CombatSummonDefinition)}
		for _, id := range append(master.AlchemistStarterDeck.SummonCardIDs[:], master.AlchemistStarterDeck.SkillCardIDs[:]...) {
			definitions.Cards[id] = battlemodel.CombatCardDefinition{ID: id, SummonID: details[id].SummonID}
			if summonID := details[id].SummonID; summonID != 0 {
				definitions.Summons[summonID] = battlemodel.CombatSummonDefinition{ID: summonID}
			}
		}
		account, err := game.New(state, game.WithCombatDefinitions(definitions))
		if err != nil {
			t.Fatal(err)
		}
		return account, state
	}
	account, state := newAccount(true, 10000)
	before := account.Snapshot(state)
	if changed, err := account.InitializeAlchemist(); err != nil || !changed {
		t.Fatalf("initialize: %v %v", changed, err)
	}
	after := account.Snapshot(state)
	if len(after.Cards) != len(before.Cards)+10 || !account.FeatureUnlocked(37) {
		t.Fatal("incomplete starter batch")
	}
	ids := append(master.AlchemistStarterDeck.SummonCardIDs[:], master.AlchemistStarterDeck.SkillCardIDs[:]...)
	for i, c := range after.Cards[len(before.Cards):] {
		if c.CardID != ids[i] || c.CreateTime <= 0 || c.Fame != 1 || i < 3 && c.Level != 1 || i >= 3 && c.Level != c.LevelMax {
			t.Fatalf("invalid starter %+v", c)
		}
	}
	created := after.Cards[len(before.Cards):]
	if created[0].Experience != 0 || created[0].AddExperience != 10 || created[3].Experience != 28526 || created[3].AddExperience != 10 {
		t.Fatalf("unexpected R457 starter progression: summon %+v; skill %+v", created[0], created[3])
	}
	t.Logf("starter samples: summon %d Lv%d exp%d add_exp%d; skill %d Lv%d exp%d add_exp%d",
		created[0].CardID, created[0].Level, created[0].Experience, created[0].AddExperience,
		created[3].CardID, created[3].Level, created[3].Experience, created[3].AddExperience)
	deck, ok := game.ExactDeck(game.DecksFromState(after.Decks), 5, 0)
	if !ok || len(deck.CardUniqueIDs) != 10 {
		t.Fatal("initial deck is missing")
	}
	if changed, err := account.InitializeAlchemist(); err != nil || changed {
		t.Fatal("repeat Home granted cards")
	}
	reloaded, err := game.New(after)
	if err != nil {
		t.Fatal("reload starter snapshot: ", err)
	}
	if changed, err := reloaded.InitializeAlchemist(); err != nil || changed {
		t.Fatal("snapshot reload regranted cards")
	}
	blocked, blockedBase := newAccount(false, 10000)
	if changed, err := blocked.InitializeAlchemist(); err != nil || changed || blocked.FeatureUnlocked(37) || len(blocked.Snapshot(blockedBase).Cards) != len(blockedBase.Cards) {
		t.Fatal("unfinished onboarding granted alchemist cards")
	}
	full, fullBase := newAccount(true, len(before.Cards))
	if changed, err := full.InitializeAlchemist(); err != nil || !changed {
		t.Fatalf("full inventory initialization: %v %v", changed, err)
	}
	overflow := full.Snapshot(fullBase)
	if len(overflow.Cards) != len(fullBase.Cards)+10 || len(overflow.Cards) <= overflow.User.CardMax || !full.FeatureUnlocked(37) {
		t.Fatal("full inventory did not receive the complete starter batch")
	}
	if _, err := game.New(overflow); err != nil {
		t.Fatalf("over-capacity account cannot reload: %v", err)
	}
	t.Logf("alchemist=%d; total templates=%d; initial 3+7, repeat/reload and over-capacity grant passed", alchemistCount, len(master.CardTemplates))
}
