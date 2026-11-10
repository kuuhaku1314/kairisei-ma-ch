package cnbootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

// Replay the reported material identities and native deck/mail transitions in
// an isolated production account; never import or mutate a player's database.
func auditCompleteBusinessRecovery(t *testing.T, handler http.Handler, savePath, seedPath string, cards masterdata.CardRuntimeMaster) {
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
	testfixture.AttachProbeCardCatalog(t, storage, cards)
	accounts := testfixture.TestAccountRepository(t, storage)
	identity, err := accounts.ResolveInternalLogin("00000000-0000-4000-8482-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.Onboarding.Step = gamestate.OnboardingStepCount
	state.User.Gold = 1000000
	state.User.ArthurRank = 15
	for _, spec := range []struct {
		id          int
		unique      int64
		level, fame int
	}{
		{10150024, 90024, 1, 1}, {10150029, 90065, 80, 100}, {10000141, 93177, 1, 1},
	} {
		found := false
		for _, c := range cards.CardTemplates {
			if c.CardID != spec.id {
				continue
			}
			c.UniqueID, c.Level, c.Fame, c.IsLock = spec.unique, spec.level, spec.fame, 0
			state.Cards = append(state.Cards, c)
			found = true
			break
		}
		if !found {
			t.Fatalf("missing sample card %d", spec.id)
		}
	}
	stackIDs := []int{20000002, 20000001, 20000004, 20000025, 20000011, 20002025, 20000029}
	state.StackCards = nil
	for _, id := range stackIDs {
		for _, c := range cards.StackCardTemplates {
			if c.CardID == id {
				c.Num = 1
				state.StackCards = append(state.StackCards, c)
				break
			}
		}
	}
	var draft gamestate.Deck
	for i := range state.Decks {
		if state.Decks[i].ArthurType == 3 && state.Decks[i].Index == 0 {
			state.Decks[i].CardUniqueIDs[0] = 90065
			draft = state.Decks[i]
		}
	}
	state.Engagement.Presents = []gamestate.Present{{PresentID: 2171848891202068326, Title: "运营赠礼", Reward: gamestate.Reward{Type: 4, Num: 25}}, {PresentID: 2171848891202068327, Title: "运营赠礼", Reward: gamestate.Reward{Type: 4, Num: 25}}}
	state.Engagement.Histories = nil
	if err := accounts.PersistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	wantPresent := -1
	call := func(route string, payload any, want int) map[string]json.RawMessage {
		t.Helper()
		b, _ := json.Marshal(payload)
		if payload == nil {
			b = nil
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", route, strings.NewReader(identity.SessionKey+string(b))))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		var common struct {
			Code          int `json:"res_code"`
			Action        int `json:"res_err_action"`
			Delete        int `json:"res_is_del_savedata"`
			Notifications []struct {
				PresentNum int `json:"present_num"`
			} `json:"notification"`
		}
		var result map[string]json.RawMessage
		if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != want || json.Unmarshal([]byte(lines[1]), &result) != nil || (want != 0 && (common.Action != 2 || common.Delete != 0)) {
			t.Fatalf("%s want=%d HTTP=%d: %s", route, want, w.Code, w.Body.String())
		}
		if wantPresent >= 0 && (len(common.Notifications) != 1 || common.Notifications[0].PresentNum != wantPresent) {
			t.Fatalf("%s stale gift badge: %+v", route, common.Notifications)
		}
		return result
	}
	load := func() gamestate.State {
		t.Helper()
		s, e := accounts.LoadState(identity.UserID)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	// The full-input gate queries every normal card and stack material through
	// the production adapter. The small game test owns availability edge cases.
	cardIDs := make([]int, 0, len(cards.CardTemplates)+len(cards.StackCardTemplates))
	for _, card := range cards.CardTemplates {
		cardIDs = append(cardIDs, card.CardID)
	}
	for _, card := range cards.StackCardTemplates {
		cardIDs = append(cardIDs, card.CardID)
	}
	slices.Sort(cardIDs)
	cardIDs = slices.Compact(cardIDs)
	sourceCounts := map[int]int{}
	for start := 0; start < len(cardIDs); start += 64 {
		batch := cardIDs[start:min(start+64, len(cardIDs))]
		result := call("/HowToGetCardShow", map[string]any{"cardids": batch}, 0)
		var lists []struct {
			CardID  int `json:"cardid"`
			Sources []struct {
				Type int `json:"type"`
			} `json:"get_cards"`
		}
		if err := json.Unmarshal(result["how_to_list"], &lists); err != nil || len(lists) != len(batch) {
			t.Fatalf("acquisition batch %d: %v, rows=%d", start, err, len(lists))
		}
		for i, list := range lists {
			if list.CardID != batch[i] || len(list.Sources) == 0 {
				t.Fatalf("acquisition response lost card %d", batch[i])
			}
			for _, source := range list.Sources {
				sourceCounts[source.Type]++
			}
		}
	}
	t.Logf("acquisition full catalog: cards/materials=%d source rows by native type=%v", len(cardIDs), sourceCounts)
	fusion := map[string]any{"base_uniqid": 90024, "add_uniqids": []int{93177, 90065}, "add_cardids": stackIDs}
	call("/CardFusion2", fusion, -1)
	rejected := load()
	if rejected.User.Gold != 1000000 || len(rejected.Cards) != len(state.Cards) {
		t.Fatal("rejected equipped-card fusion changed inventory")
	}
	call("/CardDeckSet", map[string]any{"decks": []any{map[string]any{
		"arthur_type": draft.ArthurType, "idx": draft.Index, "job_type": draft.JobType, "leader_card_idx": 0,
		"card_uniqid": make([]int64, 10), "support_card_uniqid": make([]int64, 10),
		"sphr_uniqid": make([]int64, 3), "buddy_uniqid": make([]int64, 5), "name": "未完成卡组", "is_active": 0, "is_rental": 0,
	}}}, 0)
	reloaded := load()
	for _, d := range reloaded.Decks {
		if d.ArthurType == 3 && d.IsActive != 0 {
			t.Fatal("inactive draft was silently activated")
		}
	}
	call("/CardFusion2", fusion, 0)
	fused := load()
	if fused.User.Gold != 999100 || len(fused.Cards) != len(state.Cards)-2 {
		t.Fatal("mixed fusion cost or material consumption differs")
	}
	found := false
	for _, c := range fused.Cards {
		if c.UniqueID == 90024 {
			found = true
			if c.Fame != 100 || c.Experience <= 0 {
				t.Fatal("mixed fusion did not grant fame and experience")
			}
		}
	}
	if !found {
		t.Fatal("base card vanished")
	}
	call("/GachaPlay2", map[string]any{"0": gamestate.OnboardingMultiGachaID, "1": 3, "2": "isolated-recovery-probe", "4": 0}, -3100)
	first := state.Engagement.Presents[0].PresentID
	call("/PresentBoxDelete", map[string]int64{"presentid": first}, -1)
	wantPresent = 1
	call("/PresentBoxRecv", map[string]int64{"presentid": first}, 0)
	mail := load()
	if len(mail.Engagement.Presents) != 2 || mail.Engagement.Presents[0].State != 1 || len(mail.Engagement.Histories) != 0 {
		t.Fatal("receipt did not preserve native received row")
	}
	call("/PresentBoxRecv", map[string]int64{"presentid": first}, 0)
	if load().User.Gold != fused.User.Gold+25 {
		t.Fatal("mail retry duplicated gold")
	}
	call("/PresentBoxDelete", map[string]int64{"presentid": first}, 0)
	call("/PresentBoxDelete", map[string]int64{"presentid": first}, 0)
	mail = load()
	if len(mail.Engagement.Presents) != 1 || len(mail.Engagement.Histories) != 1 {
		t.Fatal("mail delete retry changed history")
	}
	wantPresent = 0
	call("/PresentBoxMultiRecv2", map[string]any{"is_coin_recv": 1, "receive_types": []int{2}}, 0)
	call("/PresentBoxMultiRecv2", map[string]any{"is_coin_recv": 1, "receive_types": []int{2}}, 0)
	mail = load()
	if mail.User.Gold != fused.User.Gold+50 || len(mail.Engagement.Presents) != 1 || mail.Engagement.Presents[0].State != 1 {
		t.Fatal("batch receipt/retry changed native inbox or duplicated reward")
	}
	call("/PresentBoxShow", nil, 0)
	info := call("/GetMobileServiceToken", map[string]string{"sprite": "q=可乖离卡牌"}, -1)
	if string(info["url"]) != `""` {
		t.Fatal("legacy compatibility issued a GM URL")
	}
	call("/PresentBoxShow", nil, 0)
	t.Log(fmt.Sprintf("reported acquisition IDs, equipped/mixed fame fusion, inactive draft, stale tutorial draw and received-mail delete/retry passed for isolated UID %d", identity.UserID))
}
