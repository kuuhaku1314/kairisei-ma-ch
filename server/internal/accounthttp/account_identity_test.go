package accounthttp

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"kairisei.local/server/internal/accountstore"
)

// Existing account-cache tests exercise a chosen primary account explicitly.
func primaryAccountRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Header.Set(AccountUserHeader, fmt.Sprint(accountstore.PrimaryUserID))
	return request
}

func TestAccountRouterRejectsMissingIdentityBeforeLoading(t *testing.T) {
	builds := 0
	router := New(Config{Build: func(id int) (http.Handler, error) {
		builds++
		if id != accountstore.PrimaryUserID {
			t.Fatalf("wrong account: %d", id)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), nil
	}})
	for _, identity := range []string{"", "garbage", "0"} {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		request.Header.Set(AccountUserHeader, identity)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || builds != 0 {
			t.Fatalf("identity %q: status=%d loaded=%d", identity, response.Code, builds)
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, primaryAccountRequest(http.MethodPost, "/", nil))
	if response.Code != 204 || builds != 1 {
		t.Fatalf("explicit primary account rejected: %d, %d", response.Code, builds)
	}
}
