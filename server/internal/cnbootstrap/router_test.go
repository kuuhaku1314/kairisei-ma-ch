package cnbootstrap

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/protocol"
	"kairisei.local/server/internal/testfixture"
)

func TestGachaBannerVersionTracksImageBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "banner.png")
	if err := os.WriteFile(path, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{"local_friend": path}
	before, err := gachaBannerVersions(path, paths)
	if err != nil {
		t.Fatal(err)
	}
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if before[""] != want || before["local_friend"] != want {
		t.Fatalf("image content version = %v", before)
	}
	if err := os.WriteFile(path, []byte("updated artwork"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := gachaBannerVersions(path, paths)
	if err != nil || after["local_friend"] == before["local_friend"] {
		t.Fatalf("same-path image update must change client cache key: %v, %v", after, err)
	}
}

func newTestHandler(t *testing.T, root string, logPath string) http.Handler {
	t.Helper()
	savePath := testfixture.WriteTestSave(t, root)
	// These transport fixtures model an established account. Seed SQLite
	// explicitly; JSON is configuration, no longer an account-import path.
	storage, err := accountstore.OpenDatabase(savePath, savePath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := storage.LoadOrImport(); err != nil {
		t.Fatal(err)
	}
	fixture, err := accountstore.LoadSaveState(savePath)
	if err != nil {
		t.Fatal(err)
	}
	fixture.User.UnlockedFeatureIDs = append(fixture.User.UnlockedFeatureIDs, 15, 30)
	if err := storage.Persist(fixture); err != nil {
		t.Fatal(err)
	}
	cpkRoot := filepath.Join(root, "CPK")
	if err := os.Mkdir(cpkRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	testfixture.WriteCPKVersions(t, cpkRoot)
	if err := os.WriteFile(filepath.Join(cpkRoot, "alpha.cpk"), []byte("cpk!"), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := writePlatformFixture(t, root, "StandaloneWindows64", []byte("menu"))
	assetMapPath := writeTestAssetMap(t, root, []byte("Admin metadata fixture"))
	for _, fileName := range cn602GachaBannerFiles {
		if err := os.WriteFile(filepath.Join(root, fileName), []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cardMasterPath := testfixture.WriteTestCardMaster(t, root)
	handler, err := newProtocolTestHandler(t, Config{
		Persistence: PersistenceConfig{
			RequestLog: logPath,
			SavePath:   savePath,
			SeedPath:   savePath,
		},
		Resources: ResourcesConfig{
			AssetMap:           assetMapPath,
			Banners:            map[string]string{"": assetMapPath, "five_star_ticket": assetMapPath, "home": assetMapPath},
			CPKRoot:            cpkRoot,
			ImageRoot:          writeTestCardImages(t, root),
			CPKAliases:         writeTestCPKAliases(t, root),
			PlatformDeliveries: []string{profile},
		},
		Masters: MastersConfig{
			Cards:             cardMasterPath,
			Explore:           testfixture.WriteTestExploreMaster(t, root),
			Story:             testfixture.WriteTestStoryMaster(t, root),
			Battle:            "",
			Navi:              testfixture.WriteTestNaviMaster(t, root),
			Items:             testfixture.WriteTestItemMaster(t, root),
			Avatar:            testfixture.WriteTestAvatarMaster(t, root),
			Stamps:            testfixture.WriteTestStampMaster(t, root),
			Honors:            testfixture.WriteTestHonorMaster(t, root),
			PlayerProgression: filepath.Join("..", "..", "config", "cn602-player-progression-runtime.json"),
			LoginBonus:        filepath.Join("..", "..", "config", "cn602-login-bonus-runtime.json"),
		},
		Network: NetworkConfig{
			AdvertiseHost: "10.0.2.2",
			HTTPPort:      18081,
			BattleSV:      multiplayer.Endpoint{Host: "10.0.2.2", Port: uint16(18081 + 1)},
		},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Multiplayer: multiplayer.NewHub(),
	})
	if err != nil {
		t.Fatal(err)
	}
	loginRequest := httptest.NewRequest(
		http.MethodPost,
		"http://local/local/client/login",
		strings.NewReader(`{"uuid":"00000000-0000-0000-0000-000000000001","clver":"`+cnMinimumClientVersion+`","resource_platform":"StandaloneWindows64","unity_version":"6000.3.26f1"}`),
	)
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("initialize test session: status=%d body=%q", loginResponse.Code, loginResponse.Body.String())
	}
	var loginPayload struct {
		SessionKey string `json:"sess_key"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&loginPayload); err != nil || loginPayload.SessionKey == "" {
		t.Fatalf("decode initialized test session: payload=%+v error=%v", loginPayload, err)
	}
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatalf("reset request capture after test login: %v", err)
	}
	return testSessionHandler{Handler: handler, sessionKey: loginPayload.SessionKey}
}

type testSessionHandler struct {
	http.Handler
	sessionKey string
}

func (handler testSessionHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Body != nil {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		legacy := []byte(cn602LocalSession)
		if bytes.HasPrefix(body, legacy) {
			body = append([]byte(handler.sessionKey), body[len(legacy):]...)
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
	}
	handler.Handler.ServeHTTP(writer, request)
}

func writeTestAssetMap(t *testing.T, root string, version []byte, catalogAssets ...map[string]any) string {
	t.Helper()
	if len(catalogAssets) == 0 {
		catalogAssets = []map[string]any{
			testAsset("scenes", "Menu", ".prefab", cn602MenuBundle),
		}
	}
	bundleNames := make([]string, 0)
	seenBundles := make(map[string]struct{})
	for _, asset := range catalogAssets {
		bundleName := asset["bundle"].(string)
		if _, exists := seenBundles[bundleName]; exists {
			continue
		}
		seenBundles[bundleName] = struct{}{}
		bundleNames = append(bundleNames, bundleName)
	}
	bundles := make([]map[string]any, len(bundleNames))
	for index, bundleName := range bundleNames {
		bundles[index] = map[string]any{
			"bundle":       bundleName,
			"cab_name":     fmt.Sprintf("cab-test-%d", index),
			"scrambled":    true,
			"dependencies": []string{},
		}
	}
	digest := sha256.Sum256(version)
	manifest := map[string]any{
		"schema_version": 2,
		"client_profile": "cn602-bootstrap",
		"source": map[string]any{
			"catalog_version":                    cn602CatalogVersion,
			"version_dat_sha256":                 fmt.Sprintf("%x", digest),
			"parsed_unity_bundle_count":          len(bundles),
			"non_unity_bundle_count":             0,
			"bundle_dependency_edge_count":       0,
			"unresolved_bundle_dependency_count": 0,
			"scrambled_bundle_count":             len(bundles),
			"plain_bundle_count":                 0,
		},
		"bundles":        bundles,
		"catalog_assets": catalogAssets,
	}
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	assetMapPath := filepath.Join(root, "cn602-asset-map.json")
	if err := os.WriteFile(assetMapPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return assetMapPath
}

func testAsset(directory string, name string, extension string, bundle string) map[string]any {
	return map[string]any{
		"directory": directory,
		"name":      name,
		"extension": extension,
		"bundle":    bundle,
	}
}

func writeTestCPKAliases(t *testing.T, root string) string {
	t.Helper()
	content := []byte(`{"schema_version":2,"client_profile":"cn602-bootstrap","aliases":[]}`)
	manifestPath := filepath.Join(root, "cn602-cpk-aliases.json")
	if err := os.WriteFile(manifestPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return manifestPath
}

func TestCaptureUnknownCNRequest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	request := httptest.NewRequest(
		http.MethodPost,
		"http://local/unimplemented.php?probe=1",
		strings.NewReader(`{"uid":"local"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d", response.Code)
	}

	file, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("capture log is empty")
	}
	var entry capture
	if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Path != "/unimplemented.php" || entry.Query != "" ||
		entry.Body != "<body-omitted>" || !entry.BodyOmitted || entry.BodyBytes != len(`{"uid":"local"}`) || entry.BodySHA == "" {
		t.Fatalf("unexpected capture: %+v", entry)
	}
	if scanner.Scan() {
		t.Fatal("capture log has unexpected extra entry")
	}
}

func TestCN602ServerList(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	request := httptest.NewRequest(http.MethodGet, "http://local/local/server/default.list?time=1", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	want := "ALL,0,本地服务器,10.0.2.2,18081,0,," + cnMinimumClientVersion + ",,,\nALL,0,请更新客户端,10.0.2.2,18081,0,,ALL," + cnClientReleaseURL + "," + cnClientUpdateTips() + ",\n"
	if response.Body.String() != want {
		t.Fatalf("body = %q, want %q", response.Body.String(), want)
	}
	if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
}

func TestCN602BootstrapNoUpdateAndLogAcknowledgement(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	for _, path := range []string{"/xxxx/g0/apk.config", "/xxxx/g0/dex.config"} {
		request := httptest.NewRequest(http.MethodGet, "http://local"+path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != "{}" {
			t.Fatalf("%s: status=%d body=%q", path, response.Code, response.Body.String())
		}
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"http://local//log.php",
		strings.NewReader(`{"log_cat":"Activation"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"log_cat\":\"Activation\"}\n" {
		t.Fatalf("log acknowledgement: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestCN602ModuleSwitchBootstrap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	request := httptest.NewRequest(
		http.MethodPost,
		"http://local//mods_switch.php",
		strings.NewReader(`{"clver":"6.0.2","market":2}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload struct {
		ResponseCode int   `json:"res_code"`
		ModuleState  int64 `json:"mods_state"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.ResponseCode != 0 || payload.ModuleState != int64(protocol.DefaultModuleSwitches()) {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.ModuleState&(int64(1)<<7) == 0 {
		t.Fatalf("Gift code module switch is closed: %d", payload.ModuleState)
	}
	if payload.ModuleState&(int64(1)<<31) == 0 {
		t.Fatalf("Card development module switch is closed: %d", payload.ModuleState)
	}
	if payload.ModuleState&(int64(1)<<29) == 0 {
		t.Fatalf("Battle failure advice module switch is closed: %d", payload.ModuleState)
	}
	if payload.ModuleState&(int64(1)<<34) == 0 {
		t.Fatalf("Avatar shop module switch is closed: %d", payload.ModuleState)
	}
}

func TestModernLocalLoginContract(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	for _, retired := range []struct{ method, path string }{
		{http.MethodPost, "/loginSDK.php"},
		{http.MethodGet, "/local/version/default/Android/patch/catalog.dat"},
		{http.MethodGet, "/local/resources/patch/Android/patch/main_c/menu.dat.v00000000"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(retired.method, retired.path, strings.NewReader(`{}`)))
		if response.Code != http.StatusNotImplemented {
			t.Fatalf("retired resource route %s remains active: %d", retired.path, response.Code)
		}
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"http://local//local/client/login",
		strings.NewReader(`{"uuid":"00000000-0000-0000-0000-000000000001","clver":"`+cnMinimumClientVersion+`","resource_platform":"StandaloneWindows64","unity_version":"6000.3.26f1"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"res_code", "res_str", "sess_key", "api_url", "res_patch_url",
		"realname_status", "version_url", "res_img_url", "res_cpk_url",
		"web_url", "charge_url", "products_url", "update_url", "gid",
		"userid", "uid", "session", "room_config", "comment_url",
		"display_pictures", "sp_resource_flag",
	} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("required client field %q is absent", key)
		}
	}
	if payload["api_url"] != "http://10.0.2.2:18081/" ||
		payload["uid"] != "local-cn-user" || payload["session"] != "local-cn-session" {
		t.Fatalf("unexpected local login identity or API boundary: %+v", payload)
	}
	sessionKey, ok := payload["sess_key"].(string)
	if !ok || !strings.HasPrefix(sessionKey, "local-cn-") || sessionKey == cn602LocalSession {
		t.Fatalf("login returned a predictable or malformed account session: %q", sessionKey)
	}
	if pictures, ok := payload["display_pictures"].(map[string]any); !ok || len(pictures) != 0 {
		t.Fatalf("display_pictures must be an empty object: %#v", payload["display_pictures"])
	}
}

func TestCN602BusinessRoutesRequirePersistedRandomSession(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	testHandler, ok := handler.(testSessionHandler)
	if !ok || testHandler.sessionKey == "" {
		t.Fatal("test handler has no authenticated session")
	}

	valid := httptest.NewRecorder()
	testHandler.Handler.ServeHTTP(valid, httptest.NewRequest(
		http.MethodPost,
		"http://local/Connect",
		strings.NewReader(testHandler.sessionKey+"{}"),
	))
	if valid.Code != http.StatusOK {
		t.Fatalf("valid session status=%d body=%q", valid.Code, valid.Body.String())
	}

	forged := httptest.NewRecorder()
	forgedRequest := httptest.NewRequest(
		http.MethodPost,
		"http://local/Connect",
		strings.NewReader("local-cn-0000000000000000000000000000000000000000000000000000000000000000{}"),
	)
	forgedRequest.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(accountstore.PrimaryUserID))
	testHandler.Handler.ServeHTTP(forged, forgedRequest)
	var rejected map[string]any
	if forged.Code != http.StatusOK || json.Unmarshal(bytes.Split(forged.Body.Bytes(), []byte{'\n'})[0], &rejected) != nil ||
		rejected["res_code"] != float64(-3208) || rejected["res_err_action"] != float64(1) || rejected["res_is_del_savedata"] != float64(0) {
		t.Fatalf("forged session status=%d body=%q", forged.Code, forged.Body.String())
	}
}

func TestCNBossScoreRankingRouteRequiresSessionAndDispatches(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	const payload = `{"bossid":1,"mode":0,"offset":0,"limit":50}`
	for _, test := range []struct {
		name, body, message string
		status              int
	}{
		{"personal", cn602LocalSession + payload, "invalid boss score ranking request", http.StatusBadRequest},
		{"team", cn602LocalSession + strings.Replace(payload, `"mode":0`, `"mode":1`, 1), "invalid boss score ranking request", http.StatusBadRequest},
		{"missing mode", cn602LocalSession + `{"bossid":1,"offset":0,"limit":50}`, "invalid CN BossScoreRankingShow payload", http.StatusBadRequest},
		{"no session", payload, "invalid CN BossScoreRankingShow session", http.StatusUnauthorized},
		{"forged session", "unregistered-session-key" + payload, `"res_code":-3208`, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/BossScoreRankingShow", strings.NewReader(test.body))
			request.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(accountstore.PrimaryUserID))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("ranking route status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
}

func TestCN602PingAndConnectProtocolEnvelope(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	for _, test := range []struct {
		path        string
		payloadKeys []string
	}{
		{path: "/Ping", payloadKeys: []string{"stamp"}},
		{path: "/Connect", payloadKeys: []string{
			"is_user", "push_option", "game_option", "revision",
			"navi_unlock_flag", "cl_behavior_flag",
		}},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(
			http.MethodPost,
			"http://local"+test.path,
			strings.NewReader("local-cn-session-key{}"),
		))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%q", test.path, response.Code, response.Body.String())
		}
		lines := strings.Split(strings.TrimSuffix(response.Body.String(), "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("%s: protocol lines=%d body=%q", test.path, len(lines), response.Body.String())
		}
		var common map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
			t.Fatalf("%s common: %v", test.path, err)
		}
		for _, key := range []string{
			"res_code", "res_str", "notification", "revision", "is_appupdate",
			"res_err_action", "res_is_del_savedata",
		} {
			if _, ok := common[key]; !ok {
				t.Fatalf("%s: common field %q is absent", test.path, key)
			}
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(lines[1]), &payload); err != nil {
			t.Fatalf("%s payload: %v", test.path, err)
		}
		for _, key := range test.payloadKeys {
			if _, ok := payload[key]; !ok {
				t.Fatalf("%s: payload field %q is absent", test.path, key)
			}
		}
	}
}

func TestCN602BusinessAdapterBootstrapReadChain(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	for _, test := range []struct {
		path        string
		payloadKeys []string
	}{
		{path: "/HonorDeckShow", payloadKeys: []string{"deck_honorids"}},
		{path: "/CardCollectionShow", payloadKeys: []string{"0", "1", "2"}},
		{path: "/HomeShow", payloadKeys: []string{
			"user", "login_bonus_daily", "login_bonus_beginner",
			"login_bonus_total", "login_bonus_event", "quests", "banners",
			"fp_reward", "function_flag", "function_show_state", "server_time",
			"background", "max_rank", "tutorial_flag", "firstpay",
		}},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(
			http.MethodPost,
			"http://local"+test.path,
			strings.NewReader("local-cn-session-key"),
		))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status=%d body=%q", test.path, response.Code, response.Body.String())
		}
		lines := strings.Split(response.Body.String(), "\n")
		if len(lines) != 3 {
			t.Fatalf("%s: protocol lines=%d body=%q", test.path, len(lines), response.Body.String())
		}
		var common map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
			t.Fatalf("%s common: %v", test.path, err)
		}
		if common["revision"] != float64(790) || common["res_code"] != float64(0) {
			t.Fatalf("%s: unexpected common response %#v", test.path, common)
		}
		notifications, ok := common["notification"].([]any)
		if !ok || len(notifications) != 1 {
			t.Fatalf("%s: notification contract %#v", test.path, common["notification"])
		}
		notification, ok := notifications[0].(map[string]any)
		if !ok || int64(notification["unlock_feature_flag"].(float64))&(int64(1)<<15) == 0 {
			t.Fatalf("%s: CARD_DECK feature is not unlocked: %#v", test.path, notifications[0])
		}
		if int64(notification["unlock_feature_flag"].(float64))&(int64(1)<<30) == 0 {
			t.Fatalf("%s: AVATAR_PARTS_SHOP feature is not unlocked: %#v", test.path, notifications[0])
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(lines[1]), &payload); err != nil {
			t.Fatalf("%s payload: %v", test.path, err)
		}
		for _, key := range test.payloadKeys {
			if _, ok := payload[key]; !ok {
				t.Fatalf("%s: payload field %q is absent", test.path, key)
			}
		}
		if test.path == "/HomeShow" && payload["firstpay"] != float64(2147483647) {
			t.Fatalf("%s: local no-payment firstpay flag=%#v", test.path, payload["firstpay"])
		}
	}
}

func TestCN602TutorialFlagRouteAndDTOAdapter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"http://local/TutorialFlag",
		strings.NewReader(cn602LocalSession+`{"flag":134217727,"step":26}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	lines := strings.Split(response.Body.String(), "\n")
	if len(lines) != 3 {
		t.Fatalf("protocol lines=%d body=%q", len(lines), response.Body.String())
	}
	var common map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
		t.Fatal(err)
	}
	if common["res_code"] != float64(0) {
		t.Fatalf("unexpected common response %#v", common)
	}
}

func TestCN602ClickLogIsAcknowledgedWithoutAnalytics(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"http://local/ClickLog",
		strings.NewReader(cn602LocalSession+`{"info":[{"click_id":0,"click_num":0}],"click_time":1785692073}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	lines := strings.Split(response.Body.String(), "\n")
	if len(lines) != 3 {
		t.Fatalf("protocol lines=%d body=%q", len(lines), response.Body.String())
	}
	var common map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &common); err != nil {
		t.Fatal(err)
	}
	if common["res_code"] != float64(0) {
		t.Fatalf("unexpected common response %#v", common)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 0 {
		t.Fatalf("ClickLog ACK must not return analytics payload: %#v", payload)
	}
	var popup map[string][]any
	if err := json.Unmarshal([]byte(lines[2]), &popup); err != nil {
		t.Fatal(err)
	}
	if entries, ok := popup["popup"]; !ok || len(entries) != 0 {
		t.Fatalf("unexpected popup response %#v", popup)
	}
}

func TestCN602ItemShowSessionAdapter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodPost,
		"http://local/ItemShow",
		strings.NewReader(cn602LocalSession+`{"item_type":0}`),
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
	lines := strings.Split(response.Body.String(), "\n")
	if len(lines) != 3 {
		t.Fatalf("protocol lines=%d body=%q", len(lines), response.Body.String())
	}
	var payload map[string][]any
	if err := json.Unmarshal([]byte(lines[1]), &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"items", "gachas"} {
		entries, ok := payload[key]
		if !ok || len(entries) != 0 {
			t.Fatalf("unexpected %s payload %#v", key, payload)
		}
	}
}

func TestCN602NoPaymentProductsAndLocalBanners(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	products := httptest.NewRecorder()
	handler.ServeHTTP(products, httptest.NewRequest(
		http.MethodGet, "http://local/disabled/products", nil,
	))
	var productCatalog struct {
		Code     int `json:"code"`
		Products []struct {
			ID      string `json:"bid"`
			Price   string `json:"price"`
			Crystal int    `json:"gold"`
		} `json:"product_list"`
	}
	if products.Code != http.StatusOK || json.Unmarshal(products.Body.Bytes(), &productCatalog) != nil || productCatalog.Code != 200 || len(productCatalog.Products) != 8 {
		t.Fatalf("products: status=%d body=%q", products.Code, products.Body.String())
	}

	for _, product := range productCatalog.Products {
		if product.Price != "0" || product.Crystal <= 0 {
			t.Fatalf("invalid free local product: %#v", product)
		}
	}

	schedule := httptest.NewRecorder()
	handler.ServeHTTP(schedule, httptest.NewRequest(
		http.MethodGet,
		"http://local/disabled/web/information/2015/7/kechengbiao?userid=1000001&type_id=1",
		nil,
	))
	if schedule.Code != http.StatusOK ||
		!strings.Contains(schedule.Header().Get("Content-Type"), "text/html") ||
		!strings.Contains(schedule.Body.String(), "副本日程表") ||
		!strings.Contains(schedule.Body.String(), "全天开放") {
		t.Fatalf("schedule: status=%d content-type=%q body=%q", schedule.Code, schedule.Header().Get("Content-Type"), schedule.Body.String())
	}

	intro := httptest.NewRecorder()
	handler.ServeHTTP(intro, httptest.NewRequest(
		http.MethodGet,
		"http://local/disabled/web/netease/fourplusone/20160626/Intro_4.png",
		nil,
	))
	if intro.Code != http.StatusOK || intro.Header().Get("Content-Type") != "image/png" ||
		intro.Header().Get("X-Kairisei-Source-State") != "" ||
		!bytes.HasPrefix(intro.Body.Bytes(), []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("intro placeholder: status=%d headers=%v body=%x", intro.Code, intro.Header(), intro.Body.Bytes())
	}

}

func TestCN602PushNoOpAndPreloadedCPKList(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "requests.jsonl")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, root, logPath)

	push := httptest.NewRecorder()
	handler.ServeHTTP(push, httptest.NewRequest(
		http.MethodPost,
		"http://local//subcribe_push.php",
		strings.NewReader(`{"userid":1000001,"flag":1}`),
	))
	if push.Code != http.StatusOK || push.Body.String() != "{}" {
		t.Fatalf("push no-op: status=%d body=%q", push.Code, push.Body.String())
	}

	cpk := httptest.NewRecorder()
	handler.ServeHTTP(cpk, httptest.NewRequest(
		http.MethodGet,
		"http://local/local/content/StandaloneWindows64/6000.3.26f1/current/CPK/cpk_file.csv?v=790",
		nil,
	))
	const want = "# cpk_name,type,id,file_size_mb,download_type,size_bytes\nalpha.cpk,NONE,0,0.000004,ALL,4\n"
	if cpk.Code != http.StatusOK || cpk.Body.String() != want {
		t.Fatalf("CPK list: status=%d body=%q", cpk.Code, cpk.Body.String())
	}

	patchState := httptest.NewRecorder()
	handler.ServeHTTP(patchState, httptest.NewRequest(
		http.MethodGet,
		"http://local/local/content/StandaloneWindows64/6000.3.26f1/current/CPK/cpk_patch.txt?v=790",
		nil,
	))
	if patchState.Code != http.StatusOK || patchState.Body.String() != "alpha.cpk,1\n" {
		t.Fatalf("CPK patch state: status=%d body=%q", patchState.Code, patchState.Body.String())
	}
}

func newProtocolTestHandler(t *testing.T, config Config) (http.Handler, error) {
	t.Helper()
	app, err := assembleApplication(config)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		if err := app.accounts.Database().Close(); err != nil {
			t.Error(err)
		}
	})
	return app.router(), nil
}

func writeTestCardImages(t *testing.T, root string) string {
	t.Helper()
	root = filepath.Join(root, "image")
	if err := os.MkdirAll(filepath.Join(root, "chr51"), 0700); err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	file := cardImage{Path: "chr51/chr51_10000010.png", Bytes: int64(buffer.Len()), SHA256: fmt.Sprintf("%x", sha256.Sum256(buffer.Bytes()))}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(file.Path)), buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(map[string]any{"schema_version": 1, "files": []cardImage{file}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	return root
}
