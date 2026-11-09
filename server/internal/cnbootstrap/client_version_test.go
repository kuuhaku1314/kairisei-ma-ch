package cnbootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCNClientVersionGateBeforeAccountCreation(t *testing.T) {
	for _, version := range []string{"", "1.9.99", "2.0", "2.0.00", "2.0.0", "2.0.0.0", "2.0.1", "6.0.2", "6.0.7", "6.0.10", "7.0.0", "garbage"} {
		body, _ := json.Marshal(localLoginPayload{UUID: "00000000-0000-4000-8000-00000000a501", ClientVersion: version, ResourcePlatform: "StandaloneWindows64", UnityVersion: "6000.3.26f1"})
		response := httptest.NewRecorder()
		// A nil account store proves rejected clients cannot mint a session.
		platformClientLogin(nil, "127.0.0.1", 26020, CDNConfig{}, "images", nil)(response, httptest.NewRequest(http.MethodPost, "/local/client/login", strings.NewReader(string(body))))
		var data map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil || data["res_code"] != float64(-1) || data["update_url"] != cnClientReleaseURL || data["sess_key"] != nil {
			t.Fatalf("version %q was not rejected with an update link: %s (%v)", version, response.Body.String(), err)
		}
	}
	if !cnClientVersionAllowed(cnMinimumClientVersion) {
		t.Fatalf("published client version %s rejected", cnMinimumClientVersion)
	}
}
