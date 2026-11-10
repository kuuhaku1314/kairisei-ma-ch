package admin

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/testfixture"
)

func TestPopupAnnouncementsPublishHomeReadAndRestart(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{operations: o}
	mux := chi.NewRouter()
	mux.Post("/popups", a.popupAnnouncementAction)
	call := func(action string, id, revision int, content any, status int) int {
		t.Helper()
		raw := testfixture.CallContentAdmin(t, mux, "POST", "/popups", map[string]any{
			"action": action, "id": id, "expected_revision": revision, "announcement": content}, status)
		if status != 200 {
			return 0
		}
		var response struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		return response.ID
	}
	content := map[string]any{"title": "活动公告", "body": "测试正文", "button_text": "知道了"}
	draft := call("save", 0, 0, content, 200)
	if len(o.publishedPopupAnnouncements()) != 0 {
		t.Fatal("draft was published")
	}
	state := testfixture.RuntimeState(t)
	state.Onboarding.Step = 9
	state.Engagement.PopupReadIDs = nil
	durable := state
	newBusiness := func(input gamestate.State) http.Handler {
		t.Helper()
		h, err := httpapi.New(httpapi.Config{InitialState: input, BaseURL: "http://127.0.0.1:26040",
			Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
			PersistState: func(next gamestate.State) error { durable = next; return nil }})
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	business := newBusiness(state)
	request := func(handler http.Handler, path, body string) []int {
		t.Helper()
		o.Runtime.PrepareBusiness(handler)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		segments := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		if len(segments) != 3 {
			t.Fatalf("missing protocol popup segment: %s", w.Body.String())
		}
		var common struct {
			Code int `json:"res_code"`
		}
		if err := json.Unmarshal([]byte(segments[0]), &common); err != nil || common.Code != 0 {
			t.Fatalf("business failed: %s", w.Body.String())
		}
		var popups struct {
			Popup []struct {
				ID int `json:"popupid"`
			} `json:"popup"`
		}
		if err := json.Unmarshal([]byte(segments[2]), &popups); err != nil {
			t.Fatal(err)
		}
		ids := []int{}
		for _, p := range popups.Popup {
			ids = append(ids, p.ID)
		}
		return ids
	}
	if got := request(business, "/HomeShow", `{}`); len(got) != 0 {
		t.Fatal("draft delivered", got)
	}
	call("publish", draft, 1, content, 200)
	second := call("publish", 0, 2, content, 200)
	for i := 0; i < 2; i++ {
		if got := request(business, "/HomeShow", `{}`); !slices.Equal(got, []int{draft, second}) {
			t.Fatal("unread order or repeat delivery changed", got)
		}
	}
	if got := request(business, "/GachaShow", `{"show_type":0}`); len(got) != 0 {
		t.Fatal("announcement delivered outside Home", got)
	}
	request(business, "/PopupExec", `{"popupid":610000001,"is_system":1}`)
	if !slices.Contains(durable.Engagement.PopupReadIDs, draft) {
		t.Fatal("acknowledgement was not persisted")
	}
	if got := request(newBusiness(durable), "/HomeShow", `{}`); !slices.Equal(got, []int{second}) {
		t.Fatal("read state lost on account reload", got)
	}
	other := state
	other.User.UserID++
	if got := request(newBusiness(other), "/HomeShow", `{}`); len(got) != 2 {
		t.Fatal("another account inherited read state", got)
	}
	for _, step := range []int{0, 8} {
		beginner := state
		beginner.Onboarding.Step = step
		if got := request(newBusiness(beginner), "/HomeShow", `{}`); len(got) != 0 {
			t.Fatal("beginner received announcements", step, got)
		}
	}
	// Publishing is immutable. A stale page cannot replace or reissue it.
	call("save", draft, 3, content, 400)
	call("stop", second, 2, nil, 409)
	call("stop", second, 3, nil, 200)
	if got := request(business, "/HomeShow", `{}`); len(got) != 0 {
		t.Fatal("stopped announcement still delivered", got)
	}
	request(business, "/PopupExec", `{"popupid":610000002,"is_system":1}`)
	call("delete", second, 4, nil, 400)
	future := map[string]any{"title": "未开始", "body": "正文", "start_unix": time.Now().Add(time.Hour).Unix()}
	call("publish", 0, 4, future, 200)
	if got := request(business, "/HomeShow", `{}`); len(got) != 0 {
		t.Fatal("scheduled announcement arrived early", got)
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.popupAnnouncementsRevision != 5 || len(restarted.publishedPopupAnnouncements()) != 3 {
		t.Fatal("publication was not restored")
	}
	cleanup := map[string]any{"action": "cleanup", "expected_revision": 5, "ids": []int{second}}
	testfixture.CallContentAdmin(t, mux, "POST", "/popups", cleanup, 403)
	if _, err := o.SetMaintenance(true, 0, func(bool) error { return nil }); err != nil {
		t.Fatal(err)
	}
	cleanup["ids"] = []int{firstPopupAnnouncementID + 2} // Future publications cannot be removed.
	testfixture.CallContentAdmin(t, mux, "POST", "/popups", cleanup, 400)
	cleanup["ids"] = []int{second}
	testfixture.CallContentAdmin(t, mux, "POST", "/popups", cleanup, 200)
	if len(o.popupAnnouncements.Items) != 2 || slices.ContainsFunc(o.popupAnnouncements.Items, func(p popupAnnouncementEntry) bool { return p.ID == second }) {
		t.Fatal("cleanup retained publication")
	}
	reloaded, err := NewOperations(accounts.Database(), nil)
	if err != nil || reloaded.popupAnnouncements.NextID != firstPopupAnnouncementID+3 || len(reloaded.popupAnnouncements.Items) != 2 {
		t.Fatal("cleanup sequence lost on reload", err)
	}
	// New publication IDs are never recycled after cleanup.
	newID := call("save", 0, 6, content, 200)
	if newID <= firstPopupAnnouncementID+2 {
		t.Fatal("publication ID reused", newID)
	}
}
