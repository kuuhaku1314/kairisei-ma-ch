package cnbootstrap

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/testfixture"
)

func TestMaintenanceBlocksLoginAndBusinessBeforeAccountWork(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	if _, err := accounts.Database().WriteDocument("server-maintenance", 0, map[string]any{"enabled": true}, "test"); err != nil {
		t.Fatal(err)
	}
	o, err := admin.NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	app := &application{operations: o.Runtime, editor: o}
	called := 0
	handler := app.maintenanceRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }))
	for _, path := range []string{"/local/client/login", "/local/account/login", "/GachaPlay2", "/TeamBattleSoloEnd"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}")))
		if called != 0 || !strings.Contains(w.Body.String(), "服务器维护中") {
			t.Fatalf("maintenance bypass: %s %d %s", path, w.Code, w.Body.String())
		}
		if businessCompressionRoute(httptest.NewRequest(http.MethodPost, path, nil)) && (!strings.Contains(w.Body.String(), `"res_err_action":1`) || w.Code != 200) {
			t.Fatal("native popup contract lost")
		}
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/local/server/default.list", nil))
	if called != 1 {
		t.Fatal("maintenance hid read-only server information")
	}
}
