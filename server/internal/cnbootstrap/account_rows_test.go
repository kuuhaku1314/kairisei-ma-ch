package cnbootstrap

import (
	"bytes"
	"fmt"
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

// Exercise the real repository transaction: sparse writes, inventory order,
// warehouse/gift moves, account isolation and rollback of a failed save.
func TestAccountRowsCommitOnlyChangesAndRollbackTogether(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	otherID := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x4670)
	userID := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x4671)
	load := func(id int) gamestate.State {
		t.Helper()
		state, err := accounts.LoadPersistentState(id)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	persist := func(state gamestate.State) {
		t.Helper()
		if err := accounts.PersistState(userID, state); err != nil {
			t.Fatal(err)
		}
	}
	fingerprint := func(state gamestate.State) [32]byte {
		t.Helper()
		value, err := fingerprintCNAccountState(state)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	otherBefore := fingerprint(load(otherID))
	state := load(userID)
	originalCards := len(state.Cards)
	for i := 0; i < 3; i++ {
		card := state.Cards[0]
		card.UniqueID = int64(9000000000 + i)
		state.Cards = append(state.Cards, card)
	}
	present := gamestate.Present{PresentID: 9000000000, IssuedAtUnix: 1788930000, Title: "扭蛋奖励", Reward: gamestate.Reward{Type: 1, Num: 1, RewardTypeID: state.Cards[0].CardID, CardSkillLevels: []int16{1}}}
	state.Engagement.Presents = append(state.Engagement.Presents, present)
	persist(state)
	db, err := accounts.Database().Open()
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string) {
		t.Helper()
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	var metadata []byte
	if err := db.QueryRow(`SELECT payload_json FROM cn_account_snapshot WHERE user_id=?`, userID).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"cards":`, `"items":`, `"stack_cards":`, `"presents":`, `"histories":`, `"coin":`, `"coin_free":`} {
		if bytes.Contains(metadata, []byte(key)) {
			t.Fatalf("relational data duplicated in metadata: %s", key)
		}
	}
	exec(`CREATE TABLE observed_writes(kind TEXT)`)
	for _, table := range []string{"cn_account_card", "cn_account_item", "cn_account_stack_card", "cn_account_present"} {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			exec(fmt.Sprintf(`CREATE TRIGGER watch_%s_%s AFTER %s ON %s BEGIN INSERT INTO observed_writes VALUES('%s'); END`, table, event, event, table, table))
		}
	}
	state = load(userID)
	state.User.CoinFree += 100
	persist(state)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM observed_writes`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("wallet-only save rewrote inventory: %d, %v", count, err)
	}
	state.Cards = append(state.Cards[:originalCards], state.Cards[originalCards+1:]...)
	state.Cards[originalCards].Fame++
	state.ContainerCards = append(state.ContainerCards, state.Cards[originalCards+1])
	state.Cards = state.Cards[:originalCards+1]
	state.Items[0].Num++
	state.Engagement.Presents = state.Engagement.Presents[:len(state.Engagement.Presents)-1]
	state.Engagement.Histories = append(state.Engagement.Histories, present)
	expected := fingerprint(state)
	persist(state)
	if fingerprint(load(userID)) != expected {
		t.Fatal("card, item, warehouse or gift state/order changed on reload")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM observed_writes WHERE kind='cn_account_card'`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("sale and two card changes wrote %d card rows: %v", count, err)
	}
	exec(`CREATE TRIGGER fail_save BEFORE UPDATE ON cn_account_snapshot BEGIN SELECT RAISE(ABORT,'forced final write failure'); END`)
	state.User.CoinFree -= 50
	state.Cards[0].Fame++
	state.Items[0].Num++
	state.Engagement.Histories[len(state.Engagement.Histories)-1].Title = "must roll back"
	if err := accounts.PersistState(userID, state); err == nil {
		t.Fatal("injected persistence failure was ignored")
	}
	if fingerprint(load(userID)) != expected {
		t.Fatal("failed save partially committed wallet/inventory/gift changes")
	}
	exec(`DROP TRIGGER fail_save`)
	expected = fingerprint(state)
	persist(state)
	if fingerprint(load(userID)) != expected {
		t.Fatal("component digests advanced past a failed transaction")
	}
	if fingerprint(load(otherID)) != otherBefore {
		t.Fatal("another account changed")
	}
}
