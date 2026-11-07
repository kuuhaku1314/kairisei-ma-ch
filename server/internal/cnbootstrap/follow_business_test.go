package cnbootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func auditCompleteFollowBusiness(t *testing.T, handler http.Handler, savePath, seedPath string, cards masterdata.CardRuntimeMaster) {
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
	// Leave the preconstructed primary handler untouched; use fresh players.
	if _, err := accounts.ResolveInternalLogin("00000000-0000-4000-8478-000000000000"); err != nil {
		t.Fatal(err)
	}
	user := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x4781)
	other := testfixture.CreateNamedFriendCapacityAccount(t, accounts, 0x4782)
	stored, err := accounts.LoadState(user)
	if err != nil {
		t.Fatal(err)
	}
	stored.User.FollowMax = 37
	stored.User.CardMax = 6000
	if err := accounts.PersistState(user, stored); err != nil {
		t.Fatal(err)
	}
	identity, err := accounts.ResolveInternalLogin(fmt.Sprintf("00000000-0000-4000-8000-%012x", 0x4781))
	if err != nil {
		t.Fatal(err)
	}
	call := func(route, payload string, code int) map[string]json.RawMessage {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey+payload)))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		var common struct {
			Code int `json:"res_code"`
		}
		var method map[string]json.RawMessage
		if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != code || json.Unmarshal([]byte(lines[1]), &method) != nil {
			t.Fatalf("%s: expected native result %d, got HTTP %d %s", route, code, w.Code, w.Body.String())
		}
		return method
	}
	for _, target := range []int{other, accountstore.SystemPartnerUserID(2)} {
		payload := fmt.Sprintf(`{"userids":[%d,0,0,0,0,0,0,0,0,0]}`, target)
		call("/FollowAdd", payload, 0)
		call("/FollowAdd", payload, -3411)
	}
	call("/FollowAdd", `{"userids":[0,0,0,0,0,0,0,0,0,0]}`, 0)
	call("/FollowAdd", fmt.Sprintf(`{"userids":[%d]}`, user), -600)
	call("/FollowAdd", `{"userids":[123456789]}`, -601)
	call("/FollowAdd", fmt.Sprintf(`{"userids":[%d,%d]}`, accountstore.SystemPartnerUserID(3), accountstore.SystemPartnerUserID(3)), 0)
	for range 2 {
		call("/FollowUnFollow", fmt.Sprintf(`{"userid":%d}`, other), 0)
	}
	if response := call("/FollowoFollowShow", "", 0); response["follow_max"] != nil {
		t.Fatal("follow list repeated the user-info capacity")
	}
	stored, err = accounts.LoadState(user)
	if err != nil || stored.User.FollowMax != 37 || stored.User.CardMax != 10000 {
		t.Fatalf("normal HTTP reload changed stored follow capacity or failed to raise the bag: follow=%d card=%d err=%v", stored.User.FollowMax, stored.User.CardMax, err)
	}
	t.Log("native FollowAdd: real/system accounts, padded/empty/duplicate selections, business errors, repeat unfollow and subsequent list passed")
}
