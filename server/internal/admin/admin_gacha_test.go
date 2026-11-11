package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestOperatorGachaCardEligibility(t *testing.T) {
	nonGacha := AdminCatalogEntry{Rarity: 2, GachaEligible: false}
	if !adminGachaCardEligible(gamestate.GachaProfile{BannerKey: "local_standard"}, nonGacha) {
		t.Fatal("operator pools must accept cards of any acquisition source")
	}
	if adminGachaCardEligible(gamestate.GachaProfile{UnownedOnly: true}, nonGacha) || !adminGachaCardEligible(gamestate.GachaProfile{UnownedOnly: true}, AdminCatalogEntry{Rarity: 6}) {
		t.Fatal("unowned six-star pool must keep its six-star identity")
	}
}

func TestAdminGachaDraftPublicationAndRestart(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	catalog.ItemDefinitions = append(catalog.ItemDefinitions, gamestate.ItemDefinition{ItemID: 4000, Name: "大硬币", MaxOwned: 9999})
	accounts.Database().SetCatalog(catalog)
	base := gamestate.GachaProfile{GachaID: 60200301, Name: "local", Price: 10, CardNum: 2, CardNumMax: 2, GroupID: 60200301, PublicationKey: "water_coin", CardIDs: []int{1, 2}, CardWeights: []int{1, 1}, GuaranteedCount: 1, GuaranteedRarityRank: 6, RemainderRarityRank: 5}
	operations, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{base, {GachaID: gamestate.OnboardingMultiGachaID}})
	if err != nil {
		t.Fatal(err)
	}
	admin := &API{operations: operations, catalogByKey: map[string]AdminCatalogEntry{"6:1": {GachaEligible: true, Rarity: 6, ResourceState: "ready", LoveMax: 10000}, "6:2": {GachaEligible: true, Rarity: 5, ResourceState: "ready"}, "6:3": {GachaEligible: true, Rarity: 6, ResourceState: "unavailable"}}}
	router := chi.NewRouter()
	router.Post("/{action}", admin.gachaEditorAction)
	call := func(action string, body any) *httptest.ResponseRecorder {
		content, _ := json.Marshal(body)
		request := httptest.NewRequest(http.MethodPost, "http://localhost/"+action, bytes.NewReader(content))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Kairisei-Admin-Action", "apply")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	config := AdminGachaConfigFromProfile(base)
	config.Price = 25
	config.Weights = []int{1, 9}
	config.CardLoves = map[int]int{1: 3501}
	config.OddsMessage = "整池概率说明\n<文字> & 不影响权重"
	config.PayType, config.PayTypeID = 4, 4000
	admin.catalogByKey["8:4000"] = AdminCatalogEntry{Kind: "item", Name: "大硬币", ResourceState: "ready"}
	tooLong := config
	tooLong.OddsMessage = strings.Repeat("界", 1001)
	if _, err := admin.validateGachaConfig(tooLong); err == nil {
		t.Fatal("oversized odds message accepted")
	}
	boundary := config
	boundary.OddsMessage = strings.Repeat("界", 1000)
	// The payment item is registered immediately above; exactly 1000 runes is valid.
	if _, err := admin.validateGachaConfig(boundary); err != nil {
		t.Fatal("valid odds message rejected", err)
	}
	invalidPayment := config
	invalidPayment.PayTypeID = 9999
	if _, err := admin.validateGachaConfig(invalidPayment); err == nil {
		t.Fatal("unknown payment item accepted")
	}
	for _, loves := range []map[int]int{{1: -1}, {1: 10001}, {2: 1}, {99: 0}} {
		invalid := config
		invalid.CardLoves = loves
		if response := call("draft", map[string]any{"config": invalid, "expected_revision": 0}); response.Code != 400 {
			t.Fatalf("invalid loyalty %v: %d %s", loves, response.Code, response.Body.String())
		}
	}
	result := call("draft", map[string]any{"config": config, "expected_revision": 0})
	if result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	var saved struct {
		Document accountstore.Document `json:"document"`
		Preview  struct {
			Base   gamestate.GachaProfile `json:"base"`
			Stages []struct {
				Odds []int `json:"odds_scaled"`
			} `json:"stages"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Preview.Stages) != 2 || saved.Preview.Stages[0].Odds[0] != 10000000 || saved.Preview.Stages[1].Odds[0] != 10000000 {
		t.Fatalf("guaranteed draws preview wrong distribution: %s", result.Body.String())
	}
	if saved.Preview.Base.OddsMessage != config.OddsMessage || saved.Preview.Base.PayType != 4 || saved.Preview.Base.PayTypeID != 4000 || saved.Preview.Base.Price != 25 || saved.Preview.Base.CardLoves[1] != 3501 {
		t.Fatal("preview lost configured payment or loyalty")
	}
	if operations.gachaRevision != 0 {
		t.Fatal("saving a draft changed the active pool")
	}
	publish := map[string]any{"config": map[string]int{"gacha_id": base.GachaID}, "expected_revision": 1, "expected_live_revision": 0, "sha256": saved.Document.SHA256}
	if response := call("publish", publish); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if response := call("publish", publish); response.Code != 409 {
		t.Fatalf("stale publication status=%d", response.Code)
	}
	cleared, err := operations.storage.ReadDocument("gacha-draft:" + strconv.Itoa(base.GachaID))
	if err != nil || gachaDraftExists(cleared) {
		t.Fatal("publication retained draft content", err)
	}
	restarted, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{base, {GachaID: gamestate.OnboardingMultiGachaID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.gachaBases) != 1 || len(restarted.gachaConfigurations) != 1 || restarted.gachaConfigurations[0].Profile.Price != 25 {
		t.Fatal("publication did not survive restart or exposed onboarding pool")
	}
	if p := restarted.gachaConfigurations[0].Profile; p.OddsMessage != config.OddsMessage || p.PayType != 4 || p.PayTypeID != 4000 || p.CardLoves[1] != 3501 {
		t.Fatal("payment item or loyalty was lost on restart")
	}
	if got := restarted.gachaConfigurations[0].Profile.BuyMessage; got != "消耗 25 大硬币 抽取 2 张卡牌吗？" {
		t.Fatalf("published confirmation should name the configured item, got %q", got)
	}
	config.CardIDs = []int{3, 2}
	config.CardLoves = nil
	result = call("draft", map[string]any{"config": config, "expected_revision": cleared.Revision})
	if result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	if err := json.Unmarshal(result.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	publish["expected_revision"], publish["expected_live_revision"], publish["sha256"] = saved.Document.Revision, 1, saved.Document.SHA256
	if response := call("publish", publish); response.Code != 400 {
		t.Fatalf("unavailable card published: %d", response.Code)
	}
	config.CardIDs = []int{1, 2}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- call("draft", map[string]any{"config": config, "expected_revision": saved.Document.Revision}).Code
		}()
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		}
		if code == 409 {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent editors: success=%d conflict=%d", success, conflict)
	}
	entry := admin.catalogByKey["6:1"]
	entry.GachaEligible, entry.FameMax = false, 90
	admin.catalogByKey["6:1"] = entry
	if _, err := admin.validateGachaConfig(config); err != nil {
		t.Fatalf("non-gacha acquisition card rejected: %v", err)
	}
	for fames, ok := range map[*map[int]int]bool{{1: 90}: true, {1: 91}: false, {99: 2}: false} {
		withFame := config
		withFame.CardFames = *fames
		if _, err := admin.validateGachaConfig(withFame); (err == nil) != ok {
			t.Fatalf("card fame %v accepted=%v: %v", *fames, err == nil, err)
		}
		if ok && operations.projectGacha(operations.gachaBases[config.GachaID], withFame).Profile.CardFame(1) != 90 {
			t.Fatal("configured fame not carried into the game profile")
		}
	}
	entry.GachaEligible = true
	admin.catalogByKey["6:1"] = entry
	unowned := gamestate.GachaProfile{GachaID: 60201102, UnownedOnly: true, CardNum: 1, CardNumMax: 1, Price: 1, CardIDs: []int{1}, CardWeights: []int{1}}
	operations.gachaBases[unowned.GachaID] = unowned
	unownedConfig := AdminGachaConfigFromProfile(unowned)
	unownedConfig.Name = "未入手六星"
	if _, err := admin.validateGachaConfig(unownedConfig); err != nil {
		t.Fatal(err)
	}
	unownedConfig.CardIDs = []int{2}
	if _, err := admin.validateGachaConfig(unownedConfig); err == nil {
		t.Fatal("unowned six-star guarantee accepted a five-star card")
	}
	initialRevision := 0
	future := time.Now().Add(time.Hour).Unix()
	if _, err := operations.setBattlePublication(teamBattlePublicationKey, TeamBattlePublication{Mode: "all", StartUnix: future, ExpectedRevision: &initialRevision}); err != nil {
		t.Fatal(err)
	}
	allowed, err := operations.TeamBattleGroupAllowlist()
	if err != nil || allowed == nil || len(allowed) != 0 {
		t.Fatalf("future event directory is visible: %v %v", allowed, err)
	}
}

func TestCardSourceTabsKeepMultiSourceCardsAndExcludeEvolvedGachaCandidates(t *testing.T) {
	catalog, _, err := BuildAdminCatalog(masterdata.CardRuntimeMaster{
		CardTemplates: []gamestate.Card{
			{CardID: 1, Name: "初始", RarityRank: 3, AcquisitionText: "普通副本，扭蛋"},
			{CardID: 2, Name: "进化", RarityRank: 4, AcquisitionText: "普通副本，进化，扭蛋"},
			{CardID: 3, Name: "奖章", RarityRank: 5, AcquisitionText: "BOSS币扭蛋"},
			{CardID: 4, Name: "点星", RarityRank: 6, AcquisitionText: "因子覚醒-1"},
			{CardID: 5, Name: "未核配方", RarityRank: 6, AcquisitionText: "进化，觉醒"},
			{CardID: 6, Name: "骑士", RarityRank: 7},
			{CardID: 7, Name: "因子突破", RarityRank: 7},
			{CardID: 8, Name: "多进化路线", RarityRank: 6}},
		EvolutionTransitions: []gamestate.EvolutionTransition{
			{FromCardID: 1, ToCardID: 2}, {FromCardID: 2, ToCardID: 4, Type: 3},
			{FromCardID: 4, ToCardID: 6, Type: 2}, {FromCardID: 6, ToCardID: 7, Type: 3},
			{FromCardID: 2, ToCardID: 8}, {FromCardID: 4, ToCardID: 8, Type: 1}},
		DeckRankPolicy: gamestate.DeckRankPolicy{Cards: map[int]gamestate.CardRankRule{
			1: {ArthurType: 1}, 2: {ArthurType: 4}, 3: {ArthurType: 4},
		}},
	}, masterdata.ItemRuntimeMaster{})
	if err != nil {
		t.Fatal(err)
	}
	for id, tags := range map[int][]string{4: {"other"}, 5: {"other"}, 6: {"other"}, 7: {"other"}, 8: {"evolution"}} {
		for _, card := range catalog {
			if card.Kind == "card" && card.RewardTypeID == id && !slices.Equal(card.SourceTags, tags) {
				t.Fatalf("card %d source tags %v, want actual recipe tags %v", id, card.SourceTags, tags)
			}
		}
	}
	admin := &API{catalog: catalog}
	for _, source := range []string{"gacha", "dungeon"} {
		response := httptest.NewRecorder()
		admin.catalogEntries(response, httptest.NewRequest("GET", "/api/catalog?kind=card&source="+source+"&limit=1&offset=1", nil))
		var page struct {
			Total   int
			Entries []AdminCatalogEntry
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != 2 || len(page.Entries) != 1 || page.Entries[0].RewardTypeID != 2 || page.Entries[0].GachaEligible {
			t.Fatalf("incorrect source page: %s", response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	admin.catalogEntries(response, httptest.NewRequest("GET", "/api/catalog?kind=card&source=gacha&arthur_type=4&limit=1", nil))
	var page struct {
		Total   int
		Entries []AdminCatalogEntry
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Entries) != 1 || page.Entries[0].RewardTypeID != 2 || page.Entries[0].ArthurType != 4 {
		t.Fatalf("profession filter did not intersect source before pagination: %s", response.Body.String())
	}
}

func TestUnifiedCardCatalogFiltersIntersectBeforePagination(t *testing.T) {
	master := masterdata.CardRuntimeMaster{
		CardTemplates: []gamestate.Card{
			{CardID: 10214045, Name: "絢爛型オルトリート", RarityRank: 6},
			{CardID: 10214046, Name: "蹴球型ドモヴォーイ", RarityRank: 6},
			{CardID: 10214048, Name: "奏楽型ダカーポ", RarityRank: 6},
			{CardID: 10214050, Name: "聖夜型ルー", RarityRank: 6},
			{CardID: 10214053, Name: "新春型モードレッド", RarityRank: 6},
			{CardID: 10214099, Name: "既存カード", RarityRank: 6},
			{CardID: 10214098, Name: "通用卡", RarityRank: 5},
		},
		DeckRankPolicy: gamestate.DeckRankPolicy{Cards: map[int]gamestate.CardRankRule{
			10214045: {ArthurType: 3}, 10214046: {ArthurType: 4}, 10214048: {ArthurType: 2},
			10214050: {ArthurType: 1}, 10214053: {ArthurType: 2}, 10214099: {ArthurType: 2},
		}},
	}
	catalog, _, err := BuildAdminCatalog(master, masterdata.ItemRuntimeMaster{})
	if err != nil {
		t.Fatal(err)
	}
	limits := map[int]int{10214045: 0, 10214046: 1, 10214048: 2, 10214050: 1, 10214053: 2, 10214098: 0}
	for i := range catalog {
		if limit, ok := limits[catalog[i].RewardTypeID]; ok {
			catalog[i].Combat = &multiplayer.OperationsCardInfo{LimitCount: limit}
		}
	}
	admin := &API{catalog: catalog}
	for _, tc := range []struct {
		query string
		total int
		first int
	}{
		{"arthur_type=0", 7, 10214045},
		{"arthur_type=1", 1, 10214050},
		{"arthur_type=2", 3, 10214048},
		{"arthur_type=3", 1, 10214045},
		{"arthur_type=4", 1, 10214046},
		{"arthur_type=2&offset=1", 3, 10214053},
		{"arthur_type=2&q=10214053", 1, 10214053},
		{"arthur_type=1&q=10214053", 0, 0},
		{"source=other&arthur_type=2", 3, 10214048},
		{"source=gacha&arthur_type=2", 0, 0},
		{"arthur_type=-1&rarity=5", 1, 10214098},
		{"arthur_type=-1&rarity=6", 0, 0},
		{"rarity=6&offset=4", 6, 10214053},
		{"rarity=5", 1, 10214098},
		{"rarity=6&limit_count=0", 1, 10214045},
		{"rarity=6&limit_count=1&offset=1", 2, 10214050},
		{"arthur_type=2&limit_count=2&offset=1", 2, 10214053},
		{"rarity=5&limit_count=2", 0, 0},
		{"limit_count=0", 2, 10214045},
		{"q=10214099&limit_count=0", 0, 0}, // Missing details are unknown, not unstarred.
	} {
		t.Run(tc.query, func(t *testing.T) {
			response := httptest.NewRecorder()
			admin.catalogEntries(response, httptest.NewRequest("GET", "/api/catalog?kind=card&limit=1&"+tc.query, nil))
			var page struct {
				Total   int
				Entries []AdminCatalogEntry
			}
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || page.Total != tc.total ||
				(tc.first == 0 && len(page.Entries) != 0) ||
				(tc.first != 0 && (len(page.Entries) != 1 || page.Entries[0].RewardTypeID != tc.first || page.Entries[0].GachaEligible)) {
				t.Fatalf("incorrect unified card page: %s", response.Body.String())
			}
		})
	}
	for _, value := range []string{"-1", "3", "1.5", "invalid"} {
		response := httptest.NewRecorder()
		admin.catalogEntries(response, httptest.NewRequest("GET", "/api/catalog?kind=card&limit_count="+value, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid point-star filter %q accepted: %s", value, response.Body.String())
		}
	}
}
