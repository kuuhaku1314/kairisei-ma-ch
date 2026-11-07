package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/accountstore"
	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/netstats"
	"kairisei.local/server/internal/testfixture"
)

// Optional full-input gate: constructs the production handlers without opening
// any sockets. Its database and logs belong to the test, never to a player.
func TestCompleteRuntimeSetConstruction(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("set CN602_RUNTIME_SET to the complete resource directory")
	}
	if !filepath.IsAbs(root) {
		t.Fatal("absolute resource root required")
	}
	content, err := os.ReadFile(filepath.Join(root, "resource-set.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Entrypoints map[string]string `json:"entrypoints"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	p := func(key string) string {
		t.Helper()
		value, ok := manifest.Entrypoints[key]
		if !ok {
			t.Fatalf("missing entrypoint %s", key)
		}
		return filepath.Join(root, filepath.FromSlash(value))
	}
	dir := t.TempDir()
	// Optional offline compatibility fixture. Supply an SQLite backup, never a
	// live database with a WAL; the test only opens its own disposable copy.
	legacyFixture := os.Getenv("CN602_DATABASE_FIXTURE")
	if legacyFixture != "" {
		if !filepath.IsAbs(legacyFixture) {
			t.Fatal("absolute offline database fixture required")
		}
		src, err := os.Open(legacyFixture)
		if err != nil {
			t.Fatal(err)
		}
		dst, err := os.OpenFile(filepath.Join(dir, "save-state.sqlite3"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			src.Close()
			t.Fatal(err)
		}
		_, copyErr := io.Copy(dst, src)
		src.Close()
		closeErr := dst.Close()
		if copyErr != nil || closeErr != nil {
			t.Fatalf("copy offline fixture: %v / %v", copyErr, closeErr)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "requests.jsonl"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	resources := ResourcesConfig{
		AssetMap:    p("cn-asset-map"),
		GachaBanner: p("cn-gacha-banner"), FiveStarGachaBanner: p("cn-five-star-gacha-banner"), HomeBanner: p("cn-home-banner"),
		CPKRoot: p("cn-cpk-root"), ImageRoot: p("cn-image-root"), CPKAliases: p("cn-cpk-aliases"),
	}
	profiles := os.Getenv("CN602_PLATFORM_CONTENT")
	if profiles == "" {
		t.Fatal("CN602_PLATFORM_CONTENT must select built target profiles")
	}
	resources.PlatformDeliveries = filepath.SplitList(profiles)
	primaryDelivery, err := loadPlatformDelivery(resources.PlatformDeliveries[0])
	if err != nil {
		t.Fatal(err)
	}
	combat, err := multiplayer.LoadCombatCatalog(p("cn-combat-card-master"), p("cn-combat-master-root"))
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	t.Cleanup(func() {
		if t.Failed() {
			for _, line := range strings.Split(diagnostics.String(), "\n") {
				if strings.Contains(line, "level=ERROR") {
					t.Log(line)
				}
			}
		}
	})
	handler, err := New(Config{
		NetworkMetrics:    &netstats.Counters{},
		CombatDefinitions: (*battlemodel.Definitions)(combat),
		Persistence: PersistenceConfig{
			RequestLog: filepath.Join(dir, "requests.jsonl"),
			SavePath:   filepath.Join(dir, "save.json"),
			SeedPath:   p("cn-save-seed"),
		},
		Resources: resources,
		Masters: MastersConfig{
			Cards:             p("cn-card-master"),
			Explore:           p("cn-explore-master"),
			Story:             p("cn-story-master"),
			Battle:            p("cn-battle-master"),
			Navi:              p("cn-navi-master"),
			Items:             p("cn-item-master"),
			Avatar:            p("cn-avatar-master"),
			Stamps:            p("cn-stamp-master"),
			Honors:            p("cn-honor-master"),
			PVP:               p("cn-pvp-master"),
			PlayerProgression: p("cn-player-progression"),
			LoginBonus:        p("cn-login-bonus"),
		},
		Network: NetworkConfig{
			AdvertiseHost: "127.0.0.1",
			HTTPPort:      26020,
			BattleSV:      multiplayer.Endpoint{Host: "127.0.0.1", Port: 26021},
		},
		Multiplayer: multiplayer.NewHub(),
		CDN:         CDNConfig{},
		Logger:      slog.New(slog.NewTextHandler(&diagnostics, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handler.Close(); err != nil {
			t.Error(err)
		}
	})
	if len(resources.PlatformDeliveries) != 0 {
		// Same production New()/router/account path, temporary DB and no sockets.
		for _, profile := range resources.PlatformDeliveries {
			delivery, err := loadPlatformDelivery(profile)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(localLoginPayload{UUID: "f1000000-0000-4000-8000-000000000001", ClientVersion: cnMinimumClientVersion, ResourcePlatform: delivery.platform, UnityVersion: delivery.unityVersion})
			for _, loginPath := range []string{"/local/client/login"} {
				reply := httptest.NewRecorder()
				handler.ServeHTTP(reply, httptest.NewRequest(http.MethodPost, loginPath, strings.NewReader(string(body))))
				var login struct {
					Session string `json:"sess_key"`
					Version string `json:"version_url"`
				}
				if reply.Code != 200 || json.Unmarshal(reply.Body.Bytes(), &login) != nil || login.Session == "" || !strings.Contains(login.Version, "/local/content/"+delivery.platform+"/") {
					t.Fatalf("modern login %s: %d %s", loginPath, reply.Code, reply.Body.String())
				}
				catalog := httptest.NewRecorder()
				handler.ServeHTTP(catalog, httptest.NewRequest(http.MethodGet, login.Version+"patch/catalog.dat", nil))
				if catalog.Code != 200 || string(catalog.Body.Bytes()) != string(delivery.objects["patch/catalog.dat"].data) {
					t.Fatal("modern login/version chain did not serve its pinned platform catalog")
				}
			}
			retired := httptest.NewRecorder()
			handler.ServeHTTP(retired, httptest.NewRequest(http.MethodPost, "/loginSDK.php", strings.NewReader(string(body))))
			if retired.Code != http.StatusNotImplemented {
				t.Fatal("modern-only mode still exposes an unused legacy login alias")
			}
		}
		manifest, err := BuildCDNManifest(filepath.Join(root, "resource-set.json"), resources.PlatformDeliveries...)
		if err != nil || manifest.SchemaVersion != 2 {
			t.Fatalf("complete modern CDN export: %v", err)
		}
		t.Logf("Complete modern startup/login/catalog/CDN: %d profiles, %d public objects; no legacy patch roots/carrier loading, listeners or player DB", len(resources.PlatformDeliveries), len(manifest.Files))
	}
	if legacyFixture != "" {
		admin := handler.AdminHandler()
		for _, path := range []string{"/api/status", "/api/gacha-editor", "/api/settings", "/api/evolution-policy", "/api/activity-rewards", "/api/player-policy", "/api/maintenance"} {
			testfixture.CallContentAdmin(t, admin, "GET", path, nil, 200)
		}
		var page struct {
			Accounts []struct {
				ID int `json:"user_id"`
			} `json:"accounts"`
		}
		if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/accounts?limit=200&include_system=1", nil, 200), &page); err != nil {
			t.Fatal(err)
		}
		for _, account := range page.Accounts {
			testfixture.CallContentAdmin(t, admin, "GET", fmt.Sprintf("/api/accounts/%d", account.ID), nil, 200)
		}
		// Admin reads alone do not apply live pool profiles to account snapshots.
		// Exercise the real login/Home persistence boundary on this disposable DB.
		db, err := handler.database.OpenRead()
		if err != nil {
			t.Fatal(err)
		}
		rows, err := db.Query(`SELECT login_uuid FROM cn_local_account WHERE user_id < ? ORDER BY user_id LIMIT 200`, accountstore.SystemPartnerUserIDBase)
		if err != nil {
			t.Fatal(err)
		}
		var uuids []string
		for rows.Next() {
			var uuid string
			if err := rows.Scan(&uuid); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			uuids = append(uuids, uuid)
		}
		readErr := rows.Err()
		rows.Close()
		if readErr != nil || len(uuids) == 0 {
			t.Fatalf("read offline fixture logins: %v", readErr)
		}
		for _, uuid := range uuids {
			body, _ := json.Marshal(localLoginPayload{UUID: uuid, ClientVersion: cnMinimumClientVersion, ResourcePlatform: primaryDelivery.platform, UnityVersion: primaryDelivery.unityVersion})
			login := httptest.NewRecorder()
			handler.ServeHTTP(login, httptest.NewRequest("POST", "/local/client/login", strings.NewReader(string(body))))
			var identity struct {
				Session string `json:"sess_key"`
			}
			if login.Code != 200 || json.Unmarshal(login.Body.Bytes(), &identity) != nil || identity.Session == "" {
				t.Fatal("offline fixture login failed")
			}
			for attempt := 0; attempt < 2; attempt++ {
				for _, request := range []struct{ path, payload string }{{"/HomeShow", ""}, {"/TeamBattleSoloShow", `{"0":1}`}} {
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, httptest.NewRequest("POST", request.path, strings.NewReader(identity.Session+request.payload)))
					if response.Code != 200 {
						t.Fatalf("offline fixture %s %d: status %d: %s", request.path, attempt, response.Code, response.Body.String())
					}
				}
			}
		}
		t.Logf("existing database: all configuration pages, %d account snapshots, %d player logins with repeated Home/dungeon requests; disposable copy, no listeners", len(page.Accounts), len(uuids))
		return
	}
	if !t.Run("AlchemistSoloHTTP", func(t *testing.T) {
		auditAlchemistSoloHTTP(t, handler, p("cn-battle-master"))
	}) {
		return
	}
	cards, err := masterdata.LoadCardRuntimeMaster(p("cn-card-master"))
	if err != nil {
		t.Fatal(err)
	}
	auditCompleteAdminContent(t, handler)
	avatar, err := masterdata.LoadAvatarRuntimeMaster(p("cn-avatar-master"))
	if err != nil {
		t.Fatal(err)
	}
	var costumes struct {
		Entries []adminapi.AdminCatalogEntry `json:"entries"`
	}
	admin := handler.AdminHandler()
	var metrics struct {
		Database *accountstore.DatabaseStatistics `json:"database"`
		Network  *netstats.Snapshot               `json:"network"`
		Runtime  struct {
			CPUSeconds *float64 `json:"process_cpu_seconds_total"`
			Started    int64    `json:"process_started_unix_ms"`
		} `json:"go_runtime"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/status", nil, 200), &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.Database == nil || metrics.Database.Writer.MaxOpenConnections != 1 || metrics.Network == nil || metrics.Runtime.Started <= 0 {
		t.Fatal("Admin status did not expose the wired database/process/socket metrics")
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/catalog?kind=costume&limit=200", nil, 200), &costumes); err != nil {
		t.Fatal(err)
	}
	if len(costumes.Entries) != len(avatar.CostumeRewards) {
		t.Fatalf("Admin costume count %d differs from resource catalog %d", len(costumes.Entries), len(avatar.CostumeRewards))
	}
	// Exercise the actual catalog and image allowlist, including text-only chat
	// and older resource sets that have no decorative collection thumbnails.
	for _, kind := range []string{"costume", "stamp"} {
		icons, textOnly := 0, 0
		for offset := 0; ; offset += 200 {
			var page struct {
				Entries []adminapi.AdminCatalogEntry `json:"entries"`
			}
			body := testfixture.CallContentAdmin(t, admin, "GET", fmt.Sprintf("/api/catalog?kind=%s&limit=200&offset=%d", kind, offset), nil, 200)
			if err := json.Unmarshal(body, &page); err != nil {
				t.Fatal(err)
			}
			for _, entry := range page.Entries {
				url := fmt.Sprintf("/assets/%s/%d.webp", kind, entry.RewardTypeID)
				_, err := os.Stat(filepath.Join(filepath.Dir(p("cn-card-master")), "cn602-admin-assets", kind, fmt.Sprintf("%d.webp", entry.RewardTypeID)))
				if os.IsNotExist(err) {
					if entry.ImageURL != "" {
						t.Fatalf("%s points to a missing image", url)
					}
					textOnly++
					continue
				}
				if err != nil || entry.ImageURL != url {
					t.Fatalf("collection image mapping %s: %q, %v", url, entry.ImageURL, err)
				}
				reply := httptest.NewRecorder()
				request := httptest.NewRequest("GET", "http://localhost"+url, nil)
				request.RemoteAddr = "127.0.0.1:50000"
				admin.ServeHTTP(reply, request)
				if reply.Code != 200 || reply.Header().Get("Content-Type") != "image/webp" || reply.Body.Len() == 0 {
					t.Fatalf("collection image not served: %s, %d", url, reply.Code)
				}
				icons++
			}
			if len(page.Entries) < 200 {
				break
			}
		}
		t.Logf("Admin %s: %d original icons, %d text fallbacks", kind, icons, textOnly)
	}
	requests := make([]adminapi.AdminMailRequest, 0, len(avatar.CostumeRewards))
	for _, row := range avatar.CostumeRewards {
		requests = append(requests, adminapi.AdminMailRequest{RewardType: row.Type, RewardTypeID: row.ID, Quantity: 1})
	}
	testfixture.CallContentAdmin(t, admin, "POST", "/api/catalog/resolve", map[string]any{"rewards": requests}, 200)
	auditCompletePastAdmin(t, handler)
	auditCompletePlayerPolicy(t, handler)
	auditCompleteFollowBusiness(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards)
	auditCompleteSpheres(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards)
	auditCompleteBusinessRecovery(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards)
	auditCompleteCollectionRewards(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), p("cn-stamp-master"), p("cn-honor-master"), cards)
	if profile := os.Getenv("CN602_ACCOUNT_HEAP_PROFILE"); profile != "" {
		profileCompleteRuntimeAccounts(t, handler, profile, primaryDelivery)
	}
	if output := os.Getenv("CN602_GACHA_PROBE"); output != "" {
		probeCompleteRuntimeGacha(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	if output := os.Getenv("CN602_BURST_PROBE"); output != "" {
		probeCompleteRuntimeBurst(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	if output := os.Getenv("CN602_FUSION_PROBE"); output != "" {
		probeCompleteRuntimeFusion(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	if output := os.Getenv("CN602_PURCHASE_PROBE"); output != "" {
		probeCompleteRuntimePurchase(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	if output := os.Getenv("CN602_PAGE_PROBE"); output != "" {
		probeCompleteRuntimePages(t, handler, filepath.Join(dir, "save.json"), p("cn-save-seed"), cards, output)
	}
	items, err := masterdata.LoadItemRuntimeMaster(p("cn-item-master"))
	if err != nil {
		t.Fatal(err)
	}
	_, catalog, err := adminapi.BuildAdminCatalog(cards, items)
	if err != nil {
		t.Fatal(err)
	}
	state, err := accountstore.LoadSaveState(p("cn-save-seed"))
	if err != nil {
		t.Fatal(err)
	}
	auditCompleteRuntimeDropCatalog(t, state, cards, p("cn-battle-master"))
	if err := accountstore.InstallOnboardingGacha(&state); err != nil {
		t.Fatal(err)
	}
	for _, pool := range state.Gachas {
		if pool.GachaID != masterdata.OnboardingMultiGachaID {
			continue
		}
		stars := map[int]int{}
		for _, id := range pool.CardIDs {
			entry, ok := catalog[fmt.Sprintf("6:%d", id)]
			if !ok || entry.Rarity < 4 || entry.Rarity > 5 {
				t.Fatalf("tutorial card %d missing or not basic 4/5 star: %+v", id, entry)
			}
			stars[entry.Rarity]++
		}
		if stars[5] != 4 || stars[4] != 8 {
			t.Fatalf("tutorial lineup: %v", stars)
		}
	}
	count := 0
	for _, base := range state.Gachas {
		if len(base.RewardPool) == 0 {
			continue
		}
		count++
		config := adminapi.AdminGachaConfigFromProfile(base)
		if _, err := adminapi.ValidateMixedGachaConfig(catalog, base, config); err != nil {
			t.Fatalf("preset %d: %v", base.GachaID, err)
		}
		config.RewardPool[0].Reward.Num++
		if _, err := adminapi.ValidateMixedGachaConfig(catalog, base, config); err == nil {
			t.Fatal("operator could replace locked reward quantity")
		}
	}
	if count != 4 {
		t.Fatalf("mixed presets=%d", count)
	}
	t.Logf("production construction, Admin assets and %d mixed editor presets passed; no listeners started", count)
}

func probeCompleteRuntimeGacha(t *testing.T, handler http.Handler, savePath, seedPath string, cards masterdata.CardRuntimeMaster, output string) {
	t.Helper()
	if !filepath.IsAbs(output) {
		t.Fatal("absolute probe output required")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
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
	// Reserve the prebuilt primary handler; cases use fresh uncached accounts.
	if _, err := accounts.ResolveInternalLogin("00000000-0000-0000-0001-000000000000"); err != nil {
		t.Fatal(err)
	}
	for caseID, scenario := range []struct{ paid, free, cards int }{
		{0, 1000, 0}, {0, 10000000, 0}, {10000000, 0, 0},
		{0, 10000000, 700}, {0, 10000000, 701}, {0, 10000000, 2000},
		{0, 10000000, 5995}, {0, 10000000, 6000},
	} {
		identity, err := accounts.ResolveInternalLogin(fmt.Sprintf("00000000-0000-0000-0001-%012d", caseID+1))
		if err != nil {
			t.Fatal(err)
		}
		state, err := accounts.LoadState(identity.UserID)
		if err != nil {
			t.Fatal(err)
		}
		state.User.Coin, state.User.CoinFree = scenario.paid, scenario.free
		if state.User.CardMax != 6000 {
			t.Fatalf("initial card capacity=%d, want 6000", state.User.CardMax)
		}
		for len(state.Cards) < scenario.cards {
			card := state.Cards[0]
			card.UniqueID = int64(len(state.Cards) + 100000)
			state.Cards = append(state.Cards, card)
		}
		state.Onboarding.Step = masterdata.OnboardingStepCount
		if err := accounts.PersistState(identity.UserID, state); err != nil {
			t.Fatal(err)
		}
		call := func(route, payload, name string) map[string]json.RawMessage {
			t.Helper()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(identity.SessionKey+payload)))
			if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("%d-%s.jsonl", caseID, name)), response.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK {
				t.Fatalf("case %d %s HTTP %d: %s", caseID, route, response.Code, response.Body.String())
			}
			lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
			var method map[string]json.RawMessage
			var common struct {
				Code int `json:"res_code"`
			}
			if len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != 0 || json.Unmarshal([]byte(lines[1]), &method) != nil {
				t.Fatal("invalid protocol envelope")
			}
			return method
		}
		shown := call("/GachaShow", `{"0":0}`, "show")
		var pools []map[string]json.RawMessage
		if err := json.Unmarshal(shown["1"], &pools); err != nil {
			t.Fatal(err)
		}
		count := 0
		seenCounts := make(map[int]bool)
		for _, pool := range pools {
			var id, payType, cards int
			if json.Unmarshal(pool["0"], &id) != nil || json.Unmarshal(pool["9"], &payType) != nil || json.Unmarshal(pool["12"], &cards) != nil {
				t.Fatal("invalid gacha identity")
			}
			if payType != 3 || (cards != 1 && cards != 10) || seenCounts[cards] {
				continue
			}
			seenCounts[cards] = true
			before, err := accounts.LoadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			result := call("/GachaPlay2", fmt.Sprintf(`{"0":%d,"1":%d,"2":"isolated-probe","3":[],"4":0}`, id, payType), fmt.Sprintf("draw-%d", id))
			var user map[string]json.RawMessage
			if err := json.Unmarshal(result["5"], &user); err != nil {
				t.Fatal(err)
			}
			after, err := accounts.LoadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			var destinations []int
			if json.Unmarshal(result["11"], &destinations) != nil || len(destinations) != cards {
				t.Fatal("draw delivery flags missing")
			}
			mailed := 0
			for _, destination := range destinations {
				if destination == 1 {
					mailed++
				} else if destination != 0 {
					t.Fatal("invalid native draw delivery flag")
				}
			}
			if len(after.Cards) != min(6000, len(before.Cards)+cards) || mailed != max(0, len(before.Cards)+cards-6000) || len(after.Engagement.Presents)-len(before.Engagement.Presents) != mailed {
				t.Fatal("draw lost or duplicated cards across inventory and presents")
			}
			t.Logf("gacha case=%d inventory=%d capacity=%d id=%d draw=%d mailed=%d paid=%s free=%s", caseID, len(before.Cards), after.User.CardMax, id, cards, mailed, user["35"], user["36"])
			count++
			if count >= 2 {
				break
			}
		}
		if count != 2 {
			t.Fatalf("expected both ordinary draw options, got %d", count)
		}
		if scenario.cards == 6000 {
			before, err := accounts.LoadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			present := before.Engagement.Presents[len(before.Engagement.Presents)-1]
			// Sell a probe-owned, unused card, then claim the mailed draw through
			// the real adapters and reload the independent SQLite repository.
			call("/CardSell", fmt.Sprintf(`{"uniqids":[%d],"cardids":[]}`, before.Cards[len(before.Cards)-1].UniqueID), "sell-for-mail")
			call("/PresentBoxRecv", fmt.Sprintf(`{"presentid":%d}`, present.PresentID), "claim-mailed-draw")
			after, err := accounts.LoadState(identity.UserID)
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Cards) != 6000 || len(after.Engagement.Presents) != len(before.Engagement.Presents) || len(after.Engagement.Histories) != len(before.Engagement.Histories) || after.Cards[len(after.Cards)-1].CardID != present.Reward.RewardTypeID {
				t.Fatal("mailed draw was lost or duplicated when claiming after freeing capacity")
			}
		}
		db, err := storage.Open()
		if err != nil {
			t.Fatal(err)
		}
		var metadataBytes, cardRows int
		err = db.QueryRow(`SELECT length(payload_json),(SELECT COUNT(*) FROM cn_account_card WHERE user_id=?) FROM cn_account_snapshot WHERE user_id=?`, identity.UserID, identity.UserID).Scan(&metadataBytes, &cardRows)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("SQLite case=%d card_rows=%d metadata_bytes=%d", caseID, cardRows, metadataBytes)
	}
}

// Opt-in profiling uses only this test's disposable accounts; no listener or player DB.
func profileCompleteRuntimeAccounts(t *testing.T, handler http.Handler, profile string, delivery *platformDelivery) {
	t.Helper()
	// Cross the 32-entry idle-cache boundary so the profile also exercises
	// eviction, rather than only measuring its expected initial growth.
	for count := 0; count <= 48; count++ {
		if count > 0 {
			response := httptest.NewRecorder()
			body, _ := json.Marshal(localLoginPayload{UUID: fmt.Sprintf("00000000-0000-0000-0000-%012d", count), ClientVersion: cnMinimumClientVersion, ResourcePlatform: delivery.platform, UnityVersion: delivery.unityVersion})
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/local/client/login", strings.NewReader(string(body))))
			var login struct {
				Session string `json:"sess_key"`
			}
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &login) != nil || login.Session == "" {
				t.Fatalf("profile login: %s", response.Body.String())
			}
			response = httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/HomeShow", strings.NewReader(login.Session)))
			if response.Code != http.StatusOK {
				t.Fatalf("profile HomeShow: %s", response.Body.String())
			}
		}
		if count%4 == 0 {
			runtime.GC()
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			t.Logf("accounts=%d heap_live_bytes=%d heap_inuse_bytes=%d total_alloc_bytes=%d", count, mem.HeapAlloc, mem.HeapInuse, mem.TotalAlloc)
		}
	}
	file, err := os.OpenFile(profile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := pprof.WriteHeapProfile(file); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(handler)
}
