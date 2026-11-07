package testfixture

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"kairisei.local/server/internal/accountstore"
)

func NewFriendCapacityTestAccounts(t *testing.T) *accountstore.Accounts {
	t.Helper()
	seedPath := filepath.Join("..", "..", "config", "cn602-save-template.json")
	seed, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	jsonPath := filepath.Join(t.TempDir(), "save.json")
	if err := os.WriteFile(jsonPath, seed, 0o600); err != nil {
		t.Fatalf("write temporary primary seed: %v", err)
	}
	storage, err := accountstore.OpenDatabase(
		jsonPath,
		seedPath,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := storage.LoadOrImport(); err != nil {
		t.Fatalf("import primary account: %v", err)
	}
	accounts, err := accountstore.NewAccounts(storage)
	if err != nil {
		t.Fatalf("initialize account store: %v", err)
	}
	return accounts
}

func CreateNamedFriendCapacityAccount(t *testing.T, accounts *accountstore.Accounts, number int) int {
	t.Helper()
	identity, err := accounts.ResolveInternalLogin(fmt.Sprintf("00000000-0000-4000-8000-%012x", number))
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.User.Name = fmt.Sprintf("FriendEdge%02d", number)
	if err := accounts.PersistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	return identity.UserID
}

func TestAccountRepository(t *testing.T, storage *accountstore.Database) *accountstore.Accounts {
	t.Helper()
	accounts, err := accountstore.NewAccounts(storage)
	if err != nil {
		t.Fatal(err)
	}
	return accounts
}
