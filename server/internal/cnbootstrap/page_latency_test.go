package cnbootstrap

import (
	"bytes"
	"compress/gzip"
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
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

// Isolated production save path with a small/large inventory. This is not an
// HTTP or battle load test, and never opens the operator's real database.
func BenchmarkAccountPersist(b *testing.B) {
	for _, count := range []int{10, 2000} {
		b.Run(fmt.Sprintf("cards_%d", count), func(b *testing.B) {
			storage, err := accountstore.OpenDatabase(filepath.Join(b.TempDir(), "save.json"),
				filepath.Join("..", "..", "config", "cn602-save-template.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() {
				if err := storage.Close(); err != nil {
					b.Error(err)
				}
			})
			if _, err := storage.LoadOrImport(); err != nil {
				b.Fatal(err)
			}
			accounts, err := accountstore.NewAccounts(storage)
			if err != nil {
				b.Fatal(err)
			}
			identity, err := accounts.ResolveInternalLogin("00000000-0000-4000-8000-000000000001")
			if err != nil {
				b.Fatal(err)
			}
			state, err := accounts.LoadState(identity.UserID)
			if err != nil {
				b.Fatal(err)
			}
			var lastID int64
			for _, card := range state.Cards {
				if card.UniqueID > lastID {
					lastID = card.UniqueID
				}
			}
			for len(state.Cards) < count {
				card := state.Cards[0]
				lastID++
				card.UniqueID = lastID
				state.Cards = append(state.Cards, card)
			}
			if err := accounts.PersistState(identity.UserID, state); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for b.Loop() {
				state.User.Gold++
				if err := accounts.PersistState(identity.UserID, state); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			loaded, err := accounts.LoadState(identity.UserID)
			if err != nil || loaded.User.Gold != state.User.Gold || len(loaded.Cards) != count {
				b.Fatalf("saved state differs: %v", err)
			}
		})
	}
}

// An opt-in diagnostic against the complete runtime, not a timing assertion in
// CI. It includes session resolution, account construction, adapters and saves.
func probeCompleteRuntimePages(t *testing.T, handler http.Handler, savePath, seedPath string, cards masterdata.CardRuntimeMaster, output string) {
	t.Helper()
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
	if _, err := accounts.ResolveInternalLogin("00000000-0000-0000-0003-000000000000"); err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{}
	for index, count := range []int{10, 2000} {
		identity, err := accounts.ResolveInternalLogin(fmt.Sprintf("00000000-0000-0000-0003-%012d", index+1))
		if err != nil {
			t.Fatal(err)
		}
		state, err := accounts.LoadState(identity.UserID)
		if err != nil {
			t.Fatal(err)
		}
		seen := make(map[int]bool)
		for _, card := range state.Cards {
			seen[card.CardID] = true
		}
		for _, card := range cards.CardTemplates {
			if len(state.Cards) >= count {
				break
			}
			if seen[card.CardID] {
				continue
			}
			card.UniqueID = int64(len(state.Cards) + 1)
			state.Cards = append(state.Cards, card)
			state.SupportDeck.CardCollectionIDs = append(state.SupportDeck.CardCollectionIDs, card.CardID)
		}
		state.Onboarding.Step = masterdata.OnboardingStepCount
		if err := accounts.PersistState(identity.UserID, state); err != nil {
			t.Fatal(err)
		}
		for pass := 0; pass < 3; pass++ {
			for _, route := range []string{"/HomeShow", "/CardShow2", "/CardCategoryGet", "/DeckLimitShow", "/CardCollectionShow"} {
				response := httptest.NewRecorder()
				start := time.Now()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey)))
				ms := float64(time.Since(start).Microseconds()) / 1000
				var common struct {
					Code int `json:"res_code"`
				}
				if response.Code != 200 || json.Unmarshal([]byte(strings.Split(response.Body.String(), "\n")[0]), &common) != nil || common.Code != 0 {
					t.Fatalf("%s: %d %.500s", route, response.Code, response.Body.String())
				}
				compression := map[string]int{}
				for _, level := range []int{1, 6} {
					var packed bytes.Buffer
					zipper, err := gzip.NewWriterLevel(&packed, level)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := zipper.Write(response.Body.Bytes()); err != nil {
						t.Fatal(err)
					}
					if err := zipper.Close(); err != nil {
						t.Fatal(err)
					}
					compression[fmt.Sprintf("gzip%d_bytes", level)] = packed.Len()
				}
				rows = append(rows, map[string]any{"cards": count, "pass": pass, "route": route, "elapsed_ms": ms, "response_bytes": response.Body.Len(), "compression": compression})
				if pass == 0 {
					path := fmt.Sprintf("%s.%d.%s.txt", output, count, strings.TrimPrefix(route, "/"))
					file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
					if err != nil {
						t.Fatal(err)
					}
					_, err = file.Write(response.Body.Bytes())
					closeErr := file.Close()
					if err != nil || closeErr != nil {
						t.Fatalf("write response sample: %v / %v", err, closeErr)
					}
				}
			}
		}
	}
	content, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
}
