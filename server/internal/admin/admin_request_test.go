package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestAdminStampCatalogFiltersBeforePagination(t *testing.T) {
	admin := &API{catalog: []AdminCatalogEntry{
		{Kind: "stamp", StampKind: "dialog", RewardType: 16, RewardTypeID: 1, Name: "Hello"},
		{Kind: "stamp", StampKind: "image", RewardType: 16, RewardTypeID: 2, Name: "Hello"},
		{Kind: "stamp", StampKind: "voice", RewardType: 16, RewardTypeID: 3, Name: "Hello"},
		{Kind: "stamp", StampKind: "voice", RewardType: 16, RewardTypeID: 4, Name: "Goodbye"},
	}}
	for _, test := range []struct {
		query string
		total int
		id    int
	}{
		{"kind=stamp&stamp_kind=dialog", 1, 1},
		{"kind=stamp&stamp_kind=image", 1, 2},
		{"kind=stamp&stamp_kind=voice&q=hello", 1, 3},
		{"kind=stamp&stamp_kind=voice&offset=1&limit=1", 2, 4},
		{"kind=stamp", 4, 1},
	} {
		response := httptest.NewRecorder()
		admin.catalogEntries(response, httptest.NewRequest(http.MethodGet, "/api/catalog?"+test.query, nil))
		var page struct {
			Total   int                 `json:"total"`
			Entries []AdminCatalogEntry `json:"entries"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || response.Code != 200 || page.Total != test.total || len(page.Entries) == 0 || page.Entries[0].RewardTypeID != test.id {
			t.Fatalf("%s: status=%d body=%s err=%v", test.query, response.Code, response.Body.String(), err)
		}
	}
	for _, query := range []string{"kind=item&stamp_kind=voice", "kind=stamp&stamp_kind=unknown"} {
		response := httptest.NewRecorder()
		admin.catalogEntries(response, httptest.NewRequest(http.MethodGet, "/api/catalog?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid stamp filter accepted: %s", query)
		}
	}
}

func TestAdminJSONRejectsBodyBeyondLimit(t *testing.T) {
	const limit = 1024 * 1024
	prefix := `{}` + strings.Repeat(" ", limit-2)
	for _, body := range []string{prefix + " ", prefix + `{}`} {
		request := httptest.NewRequest(http.MethodPost, "http://localhost/api/accounts", strings.NewReader(body))
		var payload struct{}
		if err := decodeAdminJSON(request, &payload); err == nil {
			t.Fatal("oversized body accepted, hiding bytes after the JSON boundary")
		}
	}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/accounts", strings.NewReader(prefix))
	var payload struct{}
	if err := decodeAdminJSON(request, &payload); err != nil {
		t.Fatalf("valid body at the limit rejected: %v", err)
	}
}

func TestAdminRejectsReboundHostBeforeServingAccounts(t *testing.T) {
	for _, test := range []struct {
		host    string
		allowed bool
	}{
		{"127.0.0.1:18002", true}, {"localhost:18002", true}, {"[::1]:18002", true},
		{"attacker.example:18002", false}, {"127.0.0.1.attacker.example:18002", false}, {"192.168.1.2:18002", false},
	} {
		t.Run(test.host, func(t *testing.T) {
			reached := false
			handler := adminSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			}))
			request := httptest.NewRequest(http.MethodGet, "http://"+test.host+"/api/accounts", nil)
			request.RemoteAddr = "127.0.0.1:12345"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if reached != test.allowed || (!test.allowed && response.Code != http.StatusForbidden) {
				t.Fatalf("host boundary reached=%v status=%d", reached, response.Code)
			}
		})
	}
}

func TestAdminMailIdempotencyComparesStoredContent(t *testing.T) {
	mail := AdminMailRequest{Title: "cards", Message: "reward"}
	reward := gamestate.Reward{Type: 6, Num: 1, RewardTypeID: 1001, CardLevel: 1, CardFame: 1, CardSkillLevels: []int16{1}}
	existing := gamestate.Present{Title: mail.Title, Comment: mail.Message, Reward: reward}
	if !adminMailPayloadMatches(existing, mail, reward) {
		t.Fatal("identical retry rejected")
	}
	changed := reward
	changed.Num = 100
	if adminMailPayloadMatches(existing, mail, changed) {
		t.Fatal("reused key silently accepted a different quantity")
	}
	mail.Message = "different"
	if adminMailPayloadMatches(existing, mail, reward) {
		t.Fatal("reused key silently accepted different mail content")
	}
}
