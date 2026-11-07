package cnbootstrap

import (
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
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

// Exercise the native repeated-ID request through the production adapter,
// account projection and SQLite, including depletion and a subsequent refresh.
func probeCompleteRuntimeFusion(t *testing.T, handler http.Handler, savePath, seedPath string, cards masterdata.CardRuntimeMaster, output string) {
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
	testfixture.AttachProbeCardCatalog(t, storage, cards)
	accounts := testfixture.TestAccountRepository(t, storage)
	if _, err := accounts.ResolveInternalLogin("00000000-0000-0000-0475-000000000000"); err != nil {
		t.Fatal(err)
	}
	for caseID, scenario := range []struct {
		name            string
		stock, used     []int
		gold            int
		ids             []int
		grouped, reject bool
	}{
		{"last", []int{1}, []int{1}, 50000000, nil, false, false},
		{"deplete", []int{3}, []int{3}, 50000000, nil, false, false},
		{"mixed", []int{1, 2, 3}, []int{1, 2, 3}, 50000000, nil, false, false},
		{"mixed remainder", []int{2, 4, 3}, []int{1, 2, 3}, 50000000, nil, false, false},
		{"gold", []int{1, 2, 3}, []int{1, 2, 3}, 0, nil, false, false},
		{"MR nine", []int{9}, []int{9}, 50000000, []int{20000004}, false, false},
		{"MR hundred", []int{100}, []int{100}, 50000000, []int{20000004}, false, false},
		{"MR mixed", []int{9, 9}, []int{9, 9}, 50000000, []int{20000004, 20000031}, false, false},
		{"JP grouped 101", []int{101}, []int{101}, 50000000, nil, true, false},
		{"JP grouped 9999", []int{9999}, []int{9999}, 50000000, nil, true, false},
		{"JP grouped over total", []int{10000}, []int{10000}, 50000000, nil, true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			identity, err := accounts.ResolveInternalLogin(fmt.Sprintf("00000000-0000-0000-0475-%012d", caseID+1))
			if err != nil {
				t.Fatal(err)
			}
			state, err := accounts.LoadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			base := state.Cards[0]
			for _, candidate := range cards.CardTemplates {
				if candidate.RarityRank == 5 && candidate.LevelMax >= 50 {
					base = candidate
					break
				}
			}
			base.UniqueID, base.Level, base.Experience = 900000, 1, 0
			state.Cards = append(state.Cards, base)
			state.User.Gold = scenario.gold
			state.Onboarding.Step = masterdata.OnboardingStepCount
			state.StackCards = nil
			selected := []int{}
			materialIDs := append([]int(nil), scenario.ids...)
			if len(materialIDs) == 0 {
				for i := range scenario.stock {
					materialIDs = append(materialIDs, 20000001+i)
				}
			}
			for i, count := range scenario.stock {
				id := materialIDs[i]
				for _, template := range cards.StackCardTemplates {
					if template.CardID == id {
						template.Num = count
						state.StackCards = append(state.StackCards, template)
						break
					}
				}
				for range scenario.used[i] {
					selected = append(selected, id)
				}
			}
			if len(state.StackCards) != len(scenario.stock) {
				t.Fatal("EXP templates missing")
			}
			if err := accounts.PersistState(identity.UserID, state); err != nil {
				t.Fatal(err)
			}
			request := map[string]any{"base_uniqid": base.UniqueID, "add_uniqids": []int{}, "add_container_uniqids": []int{}}
			if scenario.grouped {
				uses := make([]map[string]int, 0, len(materialIDs))
				for i, id := range materialIDs {
					uses = append(uses, map[string]int{"cardid": id, "num": scenario.used[i]})
				}
				request["add_stackcards"] = uses
			} else {
				request["add_cardids"] = selected
			}
			payload, _ := json.Marshal(request)
			call := func(route, payload, name string) (int, map[string]json.RawMessage) {
				t.Helper()
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey+payload)))
				if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("%d-%s.jsonl", caseID, name)), response.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				if response.Code != 200 {
					if scenario.reject && response.Code == http.StatusBadRequest {
						return -http.StatusBadRequest, nil
					}
					t.Fatalf("%s HTTP %d: %s", route, response.Code, response.Body.String())
				}
				lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
				var common struct {
					Code   int `json:"res_code"`
					Action int `json:"res_err_action"`
					Delete int `json:"res_is_del_savedata"`
				}
				var method map[string]json.RawMessage
				if len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || json.Unmarshal([]byte(lines[1]), &method) != nil {
					t.Fatal("invalid protocol envelope")
				}
				if common.Code < 0 && (common.Action != 2 || common.Delete != 0) {
					t.Fatal("business failure does not preserve login")
				}
				return common.Code, method
			}
			code, method := call("/CardFusion2", string(payload), "fusion")
			persisted, err := accounts.LoadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.gold == 0 || scenario.reject {
				if code >= 0 || scenario.gold == 0 && code != -1030 || persisted.User.Gold != scenario.gold {
					t.Fatalf("balance failure code=%d gold=%d", code, persisted.User.Gold)
				}
				for i, stack := range persisted.StackCards {
					if stack.Num != scenario.stock[i] {
						t.Fatal("failure consumed materials")
					}
				}
				for _, card := range persisted.Cards {
					if card.UniqueID == base.UniqueID && card.Experience != 0 {
						t.Fatal("failure changed experience")
					}
				}
				return
			}
			if code != 0 {
				t.Fatalf("fusion business code=%d", code)
			}
			var result struct {
				Card struct {
					Experience int `json:"exp"`
				} `json:"card"`
				OldCard struct {
					BaseAddPrice int `json:"base_add_price"`
				} `json:"old_card"`
			}
			if json.Unmarshal(method["result_card"], &result) != nil {
				t.Fatal("missing result")
			}
			if result.OldCard.BaseAddPrice <= 0 || persisted.User.Gold != scenario.gold-len(selected)*result.OldCard.BaseAddPrice {
				t.Fatal("incorrect gold deduction")
			}
			if result.Card.Experience <= 0 {
				t.Fatal("fusion did not grant experience")
			}
			for _, card := range persisted.Cards {
				if card.UniqueID == base.UniqueID && card.Experience != result.Card.Experience {
					t.Fatal("persisted experience differs from result")
				}
			}
			code, shown := call("/CardShow2", "", "refresh")
			if code != 0 {
				t.Fatalf("refresh code=%d", code)
			}
			var stacks []struct {
				CardID int `json:"0"`
				Num    int `json:"1"`
			}
			if err := json.Unmarshal(shown["2"], &stacks); err != nil {
				t.Fatal(err)
			}
			for i, stock := range scenario.stock {
				remaining, listed := 0, false
				for _, stack := range stacks {
					if stack.CardID == materialIDs[i] {
						remaining, listed = stack.Num, true
					}
				}
				if remaining != stock-scenario.used[i] || listed && remaining == 0 {
					t.Errorf("refreshed material %d: num=%d listed=%v, want %d", materialIDs[i], remaining, listed, stock-scenario.used[i])
				}
			}
			code, _ = call("/CardFusion2", string(payload), "stale-selection")
			if code != -1200 {
				t.Fatalf("depleted materials should be a business rejection: %d", code)
			}
			after, err := accounts.LoadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			beforeJSON, _ := json.Marshal(persisted.Cards)
			afterJSON, _ := json.Marshal(after.Cards)
			beforeStack, _ := json.Marshal(persisted.StackCards)
			afterStack, _ := json.Marshal(after.StackCards)
			if after.User.Gold != persisted.User.Gold || string(beforeJSON) != string(afterJSON) || string(beforeStack) != string(afterStack) {
				t.Fatal("rejected stale selection changed account")
			}
			if caseID == 0 {
				for start := 0; start < len(cards.StackCardTemplates); start += 32 {
					ids := []int{}
					for _, template := range cards.StackCardTemplates[start:min(start+32, len(cards.StackCardTemplates))] {
						ids = append(ids, template.CardID)
					}
					payload, _ := json.Marshal(map[string]any{"cardids": ids})
					code, method := call("/HowToGetCardShow", string(payload), fmt.Sprintf("sources-%d", start))
					var sources []struct {
						CardID  int               `json:"cardid"`
						Entries []json.RawMessage `json:"get_cards"`
					}
					if code != 0 || json.Unmarshal(method["how_to_list"], &sources) != nil || len(sources) != len(ids) {
						t.Fatal("material source query failed")
					}
					for i, source := range sources {
						if source.CardID != ids[i] || len(source.Entries) == 0 {
							t.Fatal("source response does not match requested material")
						}
					}
				}
				t.Logf("acquisition queries passed for all %d stack material templates, including unowned and depleted materials", len(cards.StackCardTemplates))
			}
		})
	}
}
