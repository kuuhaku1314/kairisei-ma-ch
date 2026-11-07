package cnbootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"
	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/testfixture"
)

func TestTeamBattleSoloShowProfessionRequest(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := adminapi.NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for arthur := 0; arthur <= 6; arthur++ {
		t.Run(fmt.Sprint(arthur), func(t *testing.T) {
			forwarded := false
			business := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwarded = true
				var query struct {
					ArthurType int `json:"active_arthur_type"`
				}
				if err := json.NewDecoder(r.Body).Decode(&query); err != nil || query.ArthurType != arthur || r.URL.Path != "/TeamBattleSoloShow" {
					t.Fatalf("wrong catalog query: %+v / %v / %s", query, err, r.URL.Path)
				}
				_, _ = w.Write([]byte("{\"0\":0}\n{\"9\":[],\"10\":[],\"11\":[],\"12\":[],\"alchemist_groups\":[{\"boss_groupid\":7,\"stage_type\":19}]}\n{}"))
			})
			request := httptest.NewRequest(http.MethodPost, "/TeamBattleSoloShow", strings.NewReader(fmt.Sprintf(`test-session-00000000{"0":%d}`, arthur)))
			request = request.WithContext(context.WithValue(request.Context(), cnAuthenticatedUserKey{}, accountstore.PrimaryUserID))
			response := httptest.NewRecorder()
			cnBootstrapTeamBattleSoloShow(business, ops.Runtime)(response, request)
			if arthur >= 1 && arthur <= 5 {
				if !forwarded || response.Code != http.StatusOK || len(strings.Split(response.Body.String(), "\n")) != 3 || !strings.Contains(response.Body.String(), `"alchemist_groups":[{"boss_groupid":7,"stage_type":19}]`) {
					t.Fatalf("profession catalog rejected: %s", response.Body.String())
				}
			} else if forwarded || response.Code != http.StatusBadRequest {
				t.Fatalf("invalid profession forwarded: %s", response.Body.String())
			}
		})
	}
}

// The Home-selected profession is only context for this directory read. Actual
// room admission owns its separate ordinary-profession rules.
func TestTeamBattleMultiShowCurrentProfession(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[],"11":[],"12":[]}`)
	business, err := httpapi.New(httpapi.Config{InitialState: state, BaseURL: "http://127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	handler := cnBootstrapExactBusiness(business, "TeamBattleMultiShow", "/TeamBattleMultiShow", "active_arthur_type")
	var ordinary string
	for _, profession := range []int{1, 5, 0, 6} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/TeamBattleMultiShow", strings.NewReader(cn602LocalSession+fmt.Sprintf(`{"active_arthur_type":%d}`, profession)))
		request = request.WithContext(context.WithValue(request.Context(), cnAuthenticatedUserKey{}, state.User.UserID))
		handler.ServeHTTP(response, request)
		if profession < 1 || profession > 5 {
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid profession %d: %d %s", profession, response.Code, response.Body.String())
			}
			continue
		}
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		if response.Code != http.StatusOK || len(lines) != 3 {
			t.Fatalf("profession %d: %d %s", profession, response.Code, response.Body.String())
		}
		var groups map[string]json.RawMessage
		if err := json.Unmarshal([]byte(lines[1]), &groups); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"normal_groups", "special_groups", "key_groups", "event_groups"} {
			var rows []json.RawMessage
			if err := json.Unmarshal(groups[key], &rows); err != nil || rows == nil {
				t.Fatalf("profession %d lacks complete %s: %s", profession, key, lines[1])
			}
		}
		if _, exists := groups["event_bg_pictid"]; !exists {
			t.Fatal("directory background field missing")
		}
		if profession == 1 {
			ordinary = lines[1]
		} else if lines[1] != ordinary {
			t.Fatal("current profession changed the shared multiplayer directory")
		}
	}
}
