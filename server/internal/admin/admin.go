package admin

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/netstats"
	"kairisei.local/server/internal/objectstore"
)

//go:embed web/admin_ui.html
var adminHTML []byte

//go:embed web/admin_operations.js
var adminOperationsJS []byte

//go:embed web/admin_kit.js
var adminKitJS []byte

//go:embed web/admin_missions.js
var adminMissionsJS []byte

//go:embed web/admin_gift_codes.js
var adminGiftCodesJS []byte

//go:embed web/admin_pool_rules.js
var adminPoolRulesJS []byte

//go:embed web/admin_maintenance_history.js
var adminMaintenanceHistoryJS []byte

//go:embed web/admin_editors.css
var adminEditorsCSS []byte

//go:embed web/admin_ui.js
var adminJS []byte

//go:embed web/admin_ui.css
var adminCSS []byte

//go:embed web/admin_accounts.js
var adminAccountsJS []byte

//go:embed web/admin_mail.js
var adminMailJS []byte

//go:embed web/admin_settings.js
var adminSettingsJS []byte

//go:embed web/admin_evolution.js
var adminEvolutionJS []byte

//go:embed web/admin_content.js
var adminContentJS []byte

//go:embed web/admin_tower_drops.js
var adminTowerDropsJS []byte

//go:embed web/admin_player_policy.js
var adminPlayerPolicyJS []byte

//go:embed web/admin_home_banners.js
var adminHomeBannersJS []byte

//go:embed web/admin_insights.js
var adminInsightsJS []byte

// The hash-locked CN client DECK_RANK enum ends at SSSS (17). Admin setup may
// only advance this persisted high-water mark; normal gameplay remains the
// owner of calculated deck rank and no card or deck data is rewritten here.
const adminMaximumArthurRank = 17

type AdminBattleGroup struct {
	Bosses        []DropBoss `json:"bosses,omitempty"`
	PastName      string     `json:"past_name,omitempty"`
	GroupID       int        `json:"group_id"`
	Name          string     `json:"name"`
	PictureID     int        `json:"picture_id"`
	BossCount     int        `json:"boss_count"`
	BossIDs       []int      `json:"boss_ids"`
	Category      string     `json:"category"`
	Difficulties  []string   `json:"difficulties"`
	SegmentCounts []int      `json:"segment_counts"`
	MaxSegments   int        `json:"max_segments"`
	ImageURL      string     `json:"image_url"`
}

type AdminCatalogEntry struct {
	Combat         *multiplayer.OperationsCardInfo `json:"combat,omitempty"`
	Parameters     *gamestate.CardParameter        `json:"parameters,omitempty"`
	ResourceState  string                          `json:"resource_state,omitempty"`
	Kind           string                          `json:"kind"`
	RewardType     int                             `json:"reward_type"`
	RewardTypeID   int                             `json:"reward_type_id"`
	Name           string                          `json:"name"`
	Detail         string                          `json:"detail"`
	ImageURL       string                          `json:"image_url,omitempty"`
	StampKind      string                          `json:"stamp_kind,omitempty"`
	Rarity         int                             `json:"rarity,omitempty"`
	ArthurType     int8                            `json:"arthur_type,omitempty"`
	LevelMax       int                             `json:"level_max,omitempty"`
	FixedCardLevel int                             `json:"fixed_card_level,omitempty"`
	FameMax        int                             `json:"fame_max,omitempty"`
	LoveMax        int                             `json:"love_max,omitempty"`
	PictID         int                             `json:"pict_id,omitempty"`
	SourceTags     []string                        `json:"source_tags,omitempty"`
	GachaEligible  bool                            `json:"gacha_eligible"`
}

type adminCatalogImageCoverage struct {
	SourceGapIDs []int `json:"source_gap_ids"`
}

type adminAssetManifest struct {
	CatalogImageCoverage map[string]adminCatalogImageCoverage `json:"catalog_image_coverage"`
}

type API struct {
	accounts         *accountstore.Accounts
	business         AccountRuntime
	operations       *Operations
	groups           []AdminBattleGroup
	catalog          []AdminCatalogEntry
	catalogByKey     map[string]AdminCatalogEntry
	assetURLs        map[string]struct{}
	assetsRoot       string
	knownGroups      map[int]struct{}
	pastGroups       []AdminBattleGroup
	bossCount        int
	multiplayerHub   *multiplayer.Hub
	advertiseHost    string
	gamePort         int
	logger           *slog.Logger
	networkMetrics   *netstats.Counters
	runtimeLog       *runtimeLogReader
	progression      gamestate.PlayerProgressionPolicy
	pvpRanks         []game.PVPRankConfig
	gachaPresets     []AdminGachaPreset
	gachaCoverDir    string
	homeBannerDir    string
	gachaBannerPaths map[string]string
	imageStorage     objectstore.Setting
	imageSync        *imageSyncState
}

type AdminGachaPreset struct {
	PublicationKey string `json:"publication_key"`
	GroupID        int    `json:"group_id"`
	Name           string `json:"name"`
	BannerKey      string `json:"banner_key"`
	ImageURL       string `json:"image_url"`
	PaymentItemID  int    `json:"payment_item_id"`
	PaymentItem    string `json:"payment_item"`
	Price          int    `json:"price"`
	DrawCount      int    `json:"draw_count"`
	CardCount      int    `json:"card_count"`
	GachaIDs       []int  `json:"gacha_ids"`
	// The game-list entry players see: the published category, or the pool's own preset.
	CategoryNum int    `json:"category_num"`
	Category    string `json:"category,omitempty"`
}

type adminAccount struct {
	LoginBlock          accountstore.LoginBlock `json:"login_block"`
	Username            string                  `json:"username"`
	ContainerCards      int                     `json:"container_cards"`
	ContainerMax        int                     `json:"container_max"`
	Spheres             int                     `json:"spheres"`
	Buddies             int                     `json:"buddies"`
	Decks               int                     `json:"decks"`
	Items               int                     `json:"item_kinds"`
	PendingMail         int                     `json:"pending_mail"`
	Experience          int                     `json:"experience"`
	NextLevelExperience int                     `json:"next_level_experience"`
	NaviID              int8                    `json:"navi_id"`
	UnlockedNavis       int                     `json:"unlocked_navis"`
	ActiveBossID        int                     `json:"active_boss_id"`
	ActiveRoomID        int64                   `json:"active_room_id"`
	RecentActivity      string                  `json:"recent_activity,omitempty"`
	TrainingStep        int                     `json:"training_step"`
	ArthurRank          int                     `json:"arthur_rank"`
	CardMax             int                     `json:"card_max"`
	FeatureIDs          []uint                  `json:"unlocked_features"`
	UserID              int                     `json:"user_id"`
	LoginUUID           string                  `json:"login_uuid"`
	Name                string                  `json:"name"`
	CreatedUTC          string                  `json:"created_utc"`
	LastLoginUTC        string                  `json:"last_login_utc"`
	Revision            int                     `json:"revision"`
	UpdatedUTC          string                  `json:"updated_utc"`
	Level               int                     `json:"level"`
	Gold                int                     `json:"gold"`
	FriendPoint         int                     `json:"friend_point"`
	PaidCrystal         int                     `json:"paid_crystal"`
	FreeCrystal         int                     `json:"free_crystal"`
	AP                  int                     `json:"ap"`
	APMax               int                     `json:"ap_max"`
	BP                  int                     `json:"bp"`
	BPMax               int                     `json:"bp_max"`
	CardCount           int                     `json:"card_count"`
	StackCardKinds      int                     `json:"stack_card_kinds"`
	PVPPoint            int                     `json:"pvp_point"`
	ActiveArthurType    int                     `json:"active_arthur_type"`
}

type adminPublicationState struct {
	StartUnix  int64  `json:"start_unix"`
	EndUnix    int64  `json:"end_unix"`
	Mode       string `json:"mode"`
	GroupIDs   []int  `json:"group_ids"`
	Revision   int    `json:"revision"`
	UpdatedUTC string `json:"updated_utc,omitempty"`
}

type adminGachaPublicationState struct {
	GroupIDs   []int  `json:"group_ids"`
	Revision   int    `json:"revision"`
	UpdatedUTC string `json:"updated_utc,omitempty"`
}

func adminCatalogKey(rewardType int, rewardTypeID int) string {
	return strconv.Itoa(rewardType) + ":" + strconv.Itoa(rewardTypeID)
}

func requireAdminAsset(assetsRoot string, urlPath string) error {
	if !strings.HasPrefix(urlPath, "/assets/") {
		return fmt.Errorf("invalid asset URL %q", urlPath)
	}
	relativeURL := strings.TrimPrefix(urlPath, "/assets/")
	path := filepath.Join(assetsRoot, filepath.FromSlash(relativeURL))
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(assetsRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("asset URL escapes root: %q", urlPath)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return fmt.Errorf("asset %q is absent: %w", urlPath, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return fmt.Errorf("asset %q is not a non-empty regular file", urlPath)
	}
	return nil
}

func adminOptionalImageURL(assetsRoot, urlPath string) (string, error) {
	err := requireAdminAsset(assetsRoot, urlPath)
	if errors.Is(err, os.ErrNotExist) {
		// An operator thumbnail is decorative. Packaging checks completeness;
		// Older resource sets and text-only chat entries keep their text fallback.
		return "", nil
	}
	return urlPath, err
}

func applyAdminCatalogAssetCoverage(catalog []AdminCatalogEntry, assetsRoot string, resources map[int]adminCardResource) ([]AdminCatalogEntry, error) {
	content, err := os.ReadFile(filepath.Join(assetsRoot, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest adminAssetManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	expectedKinds := map[string]struct{}{
		"card": {}, "item": {}, "material": {}, "sphere": {}, "buddy": {},
	}
	gapIDs := make(map[string]map[int]struct{}, len(expectedKinds))
	for kind := range expectedKinds {
		coverage := manifest.CatalogImageCoverage[kind]
		ids := make(map[int]struct{}, len(coverage.SourceGapIDs))
		for _, identity := range coverage.SourceGapIDs {
			if identity <= 0 {
				return nil, fmt.Errorf("manifest %s source gap contains invalid ID %d", kind, identity)
			}
			if _, duplicate := ids[identity]; duplicate {
				return nil, fmt.Errorf("manifest %s source gap contains duplicate ID %d", kind, identity)
			}
			ids[identity] = struct{}{}
		}
		gapIDs[kind] = ids
	}
	publishable := make([]AdminCatalogEntry, 0, len(catalog))
	for index := range catalog {
		entry := &catalog[index]
		if entry.Kind == "currency" {
			entry.ImageURL, err = adminOptionalImageURL(assetsRoot, fmt.Sprintf("/assets/currency/%d.webp", entry.RewardType))
			if err != nil {
				return nil, fmt.Errorf("currency reward %d image: %w", entry.RewardType, err)
			}
			publishable = append(publishable, *entry)
			continue
		}
		if _, exists := expectedKinds[entry.Kind]; !exists || entry.RewardTypeID <= 0 || entry.ImageURL == "" {
			return nil, fmt.Errorf("catalog reward %s/%d has an invalid image contract", entry.Kind, entry.RewardTypeID)
		}
		if _, sourceGap := gapIDs[entry.Kind][entry.RewardTypeID]; sourceGap {
			if entry.Kind == "card" {
				copy := *entry
				copy.ImageURL = ""
				publishable = append(publishable, copy)
			}
			continue
		}
		if entry.Kind == "card" || entry.Kind == "material" {
			resource, exists := resources[entry.RewardTypeID]
			if !exists || resource.PictID < 10000000 {
				return nil, fmt.Errorf("catalog reward %s/%d has no picture identity", entry.Kind, entry.RewardTypeID)
			}
			entry.ImageURL = fmt.Sprintf("/assets/card-picture/%d.webp", resource.PictID)
		}
		if err := requireAdminAsset(assetsRoot, entry.ImageURL); err != nil {
			return nil, fmt.Errorf("catalog reward %s/%d: %w", entry.Kind, entry.RewardTypeID, err)
		}
		publishable = append(publishable, *entry)
	}
	filtered := publishable[:0]
	for _, entry := range publishable {
		if entry.Kind == "material" {
			if name := resources[entry.RewardTypeID].Name; name != "" {
				entry.Name = name
			}
		}
		if entry.Kind == "card" {
			resource := resources[entry.RewardTypeID]
			if resource.PictID < 10000000 {
				continue
			}
			entry.PictID = resource.PictID
			entry.ResourceState = "unavailable"
			if resource.Ready {
				entry.ResourceState = "ready"
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}

func attachAdminCardDetails(entries []AdminCatalogEntry, templates []gamestate.Card, details map[int]multiplayer.OperationsCardInfo) error {
	alchemistIDs := make(map[int]struct{})
	for _, card := range templates {
		if card.RarityPlus == 4 {
			alchemistIDs[card.CardID] = struct{}{}
		}
	}
	for i := range entries {
		if entries[i].Kind != "card" {
			continue
		}
		detail, exists := details[entries[i].RewardTypeID]
		_, alchemist := alchemistIDs[entries[i].RewardTypeID]
		if alchemist && !exists {
			return fmt.Errorf("alchemist card %d has no combat definition", entries[i].RewardTypeID)
		}
		if exists {
			entries[i].Combat = &detail
			if alchemist && detail.SummonID == 0 {
				entries[i].FixedCardLevel = entries[i].LevelMax
			}
		}
	}
	return nil
}

func BuildAdminCatalog(cardMaster masterdata.CardRuntimeMaster, itemMaster masterdata.ItemRuntimeMaster) ([]AdminCatalogEntry, map[string]AdminCatalogEntry, error) {
	evolved := make(map[int]bool)
	evolutionSources := make(map[int][]int)
	for _, transition := range cardMaster.EvolutionTransitions {
		evolved[transition.ToCardID] = true
		evolutionSources[transition.ToCardID] = append(evolutionSources[transition.ToCardID], transition.Type)
	}
	entries := []AdminCatalogEntry{
		{Kind: "currency", RewardType: 0, Name: "亚瑟经验", Detail: "玩家等级经验"},
		{Kind: "currency", RewardType: 4, Name: "金币", Detail: "通用强化与商店货币"},
		{Kind: "currency", RewardType: 9, Name: "友情点", Detail: "友情扭蛋货币"},
		{Kind: "currency", RewardType: 10, Name: "免费水晶", Detail: "非付费水晶余额"},
		{Kind: "currency", RewardType: 12, Name: "体力", Detail: "领取时补充 BP，上限封顶"},
	}
	for _, card := range cardMaster.CardTemplates {
		sourceTags := cardSourceTags(card.AcquisitionText, evolutionSources[card.CardID])
		entries = append(entries, AdminCatalogEntry{
			Parameters: &card.ParameterMaximum,
			Kind:       "card", RewardType: 6, RewardTypeID: card.CardID,
			Name: card.Name, Detail: card.AcquisitionText,
			SourceTags:    sourceTags,
			GachaEligible: cardCrystalGachaSource(card.AcquisitionText) && !evolved[card.CardID] && card.RarityRank >= 3,
			ImageURL:      fmt.Sprintf("/assets/card/%d.webp", card.CardID),
			Rarity:        card.RarityRank, LevelMax: card.LevelMax,
			ArthurType: cardMaster.DeckRankPolicy.Cards[card.CardID].ArthurType,
			FameMax:    card.FameMax, LoveMax: card.LoveMax,
		})
	}
	for _, item := range itemMaster.Items {
		entries = append(entries, AdminCatalogEntry{
			Kind: "item", RewardType: 8, RewardTypeID: item.ItemID,
			Name: item.Name, Detail: item.Description, PictID: item.PictID,
			ImageURL: fmt.Sprintf("/assets/item/%d.webp", item.ItemID),
		})
	}
	for _, stack := range cardMaster.StackCardTemplates {
		entries = append(entries, AdminCatalogEntry{
			Kind: "material", RewardType: 13, RewardTypeID: stack.CardID,
			Name:     fmt.Sprintf("素材卡 %d", stack.CardID),
			Detail:   fmt.Sprintf("素材类型 %d · 强化经验 %d", stack.MaterialType, stack.AddExperience),
			ImageURL: fmt.Sprintf("/assets/card/%d.webp", stack.CardID),
		})
	}
	for _, sphere := range cardMaster.SphereDefinitions {
		entries = append(entries, AdminCatalogEntry{
			Kind: "sphere", RewardType: 15, RewardTypeID: sphere.SphereID,
			Name: sphere.Name, Detail: sphere.Text, PictID: sphere.PictID,
			ImageURL: fmt.Sprintf("/assets/sphere/%d.webp", sphere.SphereID),
			LevelMax: sphere.MaxLevel,
		})
	}
	for _, buddy := range cardMaster.BuddyDefinitions {
		entries = append(entries, AdminCatalogEntry{
			Kind: "buddy", RewardType: 19, RewardTypeID: buddy.BuddyID,
			Name: buddy.Name, Detail: buddy.Rarity, PictID: buddy.PictID,
			ImageURL: fmt.Sprintf("/assets/buddy/%d.webp", buddy.BuddyID),
			LevelMax: buddy.MaxLevel,
		})
	}
	kindOrder := map[string]int{"currency": 0, "card": 1, "item": 2, "material": 3, "sphere": 4, "buddy": 5}
	sort.Slice(entries, func(left int, right int) bool {
		leftOrder, rightOrder := kindOrder[entries[left].Kind], kindOrder[entries[right].Kind]
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if entries[left].RewardTypeID != entries[right].RewardTypeID {
			return entries[left].RewardTypeID < entries[right].RewardTypeID
		}
		return entries[left].Name < entries[right].Name
	})
	byKey := make(map[string]AdminCatalogEntry, len(entries))
	for _, entry := range entries {
		if entry.Name == "" || entry.RewardType < 0 || entry.RewardTypeID < 0 ||
			(entry.RewardTypeID == 0 && entry.Kind != "currency") {
			return nil, nil, errors.New("CN admin catalog contains an invalid entry")
		}
		key := adminCatalogKey(entry.RewardType, entry.RewardTypeID)
		if _, duplicate := byKey[key]; duplicate {
			return nil, nil, fmt.Errorf("CN admin catalog contains duplicate reward %s", key)
		}
		byKey[key] = entry
	}
	return entries, byKey, nil
}

func adminSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Cache-Control", "no-store")
		// Binding the listener to loopback alone does not prevent browser DNS
		// rebinding. Refuse non-loopback authorities even for read-only routes.
		host := request.Host
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = parsed
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			WriteAdminError(writer, http.StatusForbidden, "admin requires a localhost or loopback Host")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func requireAdminMutation(request *http.Request) error {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("admin mutations require a loopback client")
	}
	if request.Header.Get("X-Kairisei-Admin-Action") != "apply" {
		return errors.New("admin action confirmation header is missing")
	}
	if !strings.HasPrefix(strings.ToLower(request.Header.Get("Content-Type")), "application/json") {
		return errors.New("admin mutations require application/json")
	}
	if site := request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return errors.New("cross-site admin mutation rejected")
	}
	return nil
}

func decodeAdminJSON(request *http.Request, target any) error {
	return DecodeAdminJSONLimit(request, target, 1024*1024)
}

func DecodeAdminJSONLimit(request *http.Request, target any, maxBodyBytes int) error {
	body, err := io.ReadAll(io.LimitReader(request.Body, int64(maxBodyBytes)+1))
	if err != nil {
		return err
	}
	if len(body) > maxBodyBytes {
		return fmt.Errorf("admin request exceeds %d KiB", maxBodyBytes/1024)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("admin request must contain exactly one JSON value")
	}
	return nil
}

func WriteAdminJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

func WriteAdminError(writer http.ResponseWriter, status int, message string) {
	WriteAdminJSON(writer, status, map[string]any{"state": "FAIL", "error": message})
}

func (admin *API) index(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = writer.Write(adminHTML)
}

func (admin *API) health(writer http.ResponseWriter, _ *http.Request) {
	WriteAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "schema_version": accountstore.SaveDatabaseSchemaVersion,
	})
}

func (admin *API) status(writer http.ResponseWriter, request *http.Request) {
	accountSummary, err := admin.accounts.AccountSummary()
	if err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	policy, err := admin.publicationState()
	if err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	// The shell shows a global banner while maintenance pauses game requests.
	maintenance := map[string]any{"enabled": false, "busy": false, "revision": 0}
	if admin.operations != nil {
		state := admin.operations.MaintenanceState()
		maintenance["enabled"], maintenance["busy"] = state.Enabled, state.Busy
		maintenance["revision"] = state.Revision
	}
	response := map[string]any{
		"state": "PASS", "client_profile": "cn602-bootstrap", "maintenance": maintenance,
		"game_endpoint": fmt.Sprintf("http://%s:%d", admin.advertiseHost, admin.gamePort),
		"battle_port":   admin.gamePort + 1, "active_rooms": admin.multiplayerHub.RoomCount(),
		"max_player_level": admin.progression.MaxLevel, "account_count": accountSummary.Players, "boss_group_count": len(admin.groups),
		"accounts": accountSummary, "activity": admin.activity(),
		"account_creation_rejections": admin.accounts.CreationRejections(),
		"go_runtime":                  readGoRuntime(time.Now()),
		"database":                    admin.accounts.Database().Statistics(),
		"boss_count":                  admin.bossCount, "publication": policy,
		"sqlite_schema_version": accountstore.SaveDatabaseSchemaVersion,
		"server_time_utc":       time.Now().UTC().Format(time.RFC3339),
	}
	if admin.networkMetrics != nil {
		response["network"] = admin.networkMetrics.Snapshot()
	}
	if request.URL.Query().Get("room_diagnostics") == "1" {
		response["room_synchronization"] = admin.multiplayerHub.RoomSynchronizationSnapshot()
	}
	WriteAdminJSON(writer, http.StatusOK, response)
}

// loadAccountState binds the current progression rules and persists only
// actual changes to level-derived experience, BP caps or job parameters.
func (admin *API) loadAccountState(userID int) (gamestate.State, error) {
	state, err := admin.accounts.LoadState(userID)
	if err != nil {
		return gamestate.State{}, err
	}
	changed, err := masterdata.ApplyPlayerProgressionRuntimeMaster(&state, admin.progression)
	if err != nil {
		return gamestate.State{}, err
	}
	if !changed {
		return state, nil
	}
	if err := admin.accounts.PersistState(userID, state); err != nil {
		return gamestate.State{}, fmt.Errorf("persist CN admin player progression: %w", err)
	}
	admin.business.Invalidate(userID)
	return state, nil
}

func (admin *API) accountDetail(writer http.ResponseWriter, request *http.Request) {
	userID, err := strconv.Atoi(chi.URLParam(request, "userID"))
	if err != nil || userID < accountstore.PrimaryUserID {
		WriteAdminError(writer, http.StatusBadRequest, "invalid user ID")
		return
	}
	lock := admin.business.AccountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	state, err := admin.loadAccountState(userID)
	if err != nil {
		WriteAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	account := adminAccount{
		ContainerCards: len(state.ContainerCards), ContainerMax: state.User.CardContainerMax,
		Spheres: len(state.Spheres), Buddies: len(state.Buddies), Decks: len(state.Decks), Items: len(state.Items),
		Experience: state.User.Experience, NextLevelExperience: state.User.NextLevelExperience,
		NaviID: state.User.NaviID, UnlockedNavis: len(state.User.SelectableNaviIDs),
		UserID: userID, Name: state.User.Name, Level: state.User.Level,
		Gold: state.User.Gold, FriendPoint: state.User.FriendPoint,
		PaidCrystal: state.User.Coin, FreeCrystal: state.User.CoinFree,
		AP: state.User.AP, APMax: state.User.APMax, BP: state.User.BP, BPMax: state.User.BPMax,
		CardCount: len(state.Cards), StackCardKinds: len(state.StackCards),
		PVPPoint: state.User.PVPPoint, ActiveArthurType: state.User.ActiveArthurType,
		TrainingStep: state.Onboarding.Step, ArthurRank: state.User.ArthurRank, CardMax: state.User.CardMax, FeatureIDs: state.User.UnlockedFeatureIDs,
	}
	for _, present := range state.Engagement.Presents {
		if present.State == 0 {
			account.PendingMail++
		}
	}
	if state.ActiveTeamBattle != nil {
		account.ActiveBossID = state.ActiveTeamBattle.BossID
		account.ActiveRoomID = state.ActiveTeamBattle.PrepaidRoomID
	}
	for _, player := range admin.activity().HTTP.Players {
		if player.UserID == userID {
			account.RecentActivity = player.LastRequest.UTC().Format(time.RFC3339)
			break
		}
	}
	rows, _, err := admin.accounts.QueryAccounts("", true, 1, 0, userID)
	if err != nil {
		WriteAdminError(writer, 500, err.Error())
		return
	}
	if len(rows) > 0 {
		account.Username = rows[0].Username
		account.LoginBlock = rows[0].LoginBlock
	}
	metadata, err := admin.accounts.LoginMetadata(userID)
	if err != nil {
		WriteAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	account.LoginUUID, account.CreatedUTC, account.LastLoginUTC = metadata.LoginUUID, metadata.CreatedUTC, metadata.LastLoginUTC
	account.Revision, account.UpdatedUTC, err = admin.accounts.SnapshotMetadata(userID)
	if err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	WriteAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "account": account})
}

func (admin *API) catalogEntries(writer http.ResponseWriter, request *http.Request) {
	attribute := request.URL.Query().Get("attribute")
	if attribute != "" && !slices.Contains([]string{"FIRE", "ICE", "WIND", "LIGHT", "DARK"}, attribute) {
		WriteAdminError(writer, 400, "未知属性")
		return
	}
	cost := -1
	if raw := request.URL.Query().Get("cost"); raw != "" {
		var err error
		cost, err = strconv.Atoi(raw)
		if err != nil || cost < 0 || cost > 99 {
			WriteAdminError(writer, 400, "费用须为0–99")
			return
		}
	}
	resource := request.URL.Query().Get("resource")
	if resource != "" && resource != "available" && resource != "unavailable" {
		WriteAdminError(writer, 400, "未知资源状态")
		return
	}
	kind := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("kind")))
	source := request.URL.Query().Get("source")
	arthurType := 0
	if raw := request.URL.Query().Get("arthur_type"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < -1 || parsed > 5 {
			WriteAdminError(writer, http.StatusBadRequest, "card arthur_type must be -1 (common), 0 (all), or 1 through 5")
			return
		}
		arthurType = parsed
	}
	rarity := 0
	if raw := request.URL.Query().Get("rarity"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 8 {
			WriteAdminError(writer, http.StatusBadRequest, "catalog rarity must be 0 (all) through 8")
			return
		}
		rarity = parsed
	}
	limitCount := -1
	if raw := request.URL.Query().Get("limit_count"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > 2 {
			WriteAdminError(writer, http.StatusBadRequest, "catalog limit_count must be 0, 1, or 2")
			return
		}
		limitCount = parsed
	}
	query := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("q")))
	limit := 80
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			WriteAdminError(writer, http.StatusBadRequest, "catalog limit must be 1 through 200")
			return
		}
		limit = parsed
	}
	offset := 0
	if raw := request.URL.Query().Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			WriteAdminError(writer, http.StatusBadRequest, "catalog offset must be non-negative")
			return
		}
		offset = parsed
	}
	if kind != "" {
		valid := map[string]bool{"currency": true, "card": true, "item": true, "material": true, "sphere": true, "buddy": true, "costume": true, "stamp": true, "honor": true}
		if !valid[kind] {
			WriteAdminError(writer, http.StatusBadRequest, "unknown catalog kind")
			return
		}
	}
	filtered := make([]AdminCatalogEntry, 0, limit)
	stampKind := request.URL.Query().Get("stamp_kind")
	if stampKind != "" && (kind != "stamp" || stampKind != "dialog" && stampKind != "image" && stampKind != "voice") {
		WriteAdminError(writer, http.StatusBadRequest, "stamp_kind requires kind=stamp and dialog, image or voice")
		return
	}
	total := 0
	for _, entry := range admin.catalog {
		if stampKind != "" && entry.StampKind != stampKind {
			continue
		}
		if attribute != "" && (entry.Combat == nil || !slices.Contains(strings.Split(entry.Combat.Attribute, "_"), attribute)) {
			continue
		}
		if cost >= 0 && (entry.Combat == nil || entry.Combat.Cost != cost) {
			continue
		}
		if resource == "available" && entry.ResourceState == "unavailable" || resource == "unavailable" && entry.ResourceState != "unavailable" {
			continue
		}
		if kind != "" && entry.Kind != kind {
			continue
		}
		if source != "" && !slices.Contains(entry.SourceTags, source) {
			continue
		}
		if arthurType != 0 && (entry.Kind != "card" || int(entry.ArthurType) != max(0, arthurType)) {
			continue
		}
		if rarity != 0 && entry.Rarity != rarity {
			continue
		}
		if limitCount >= 0 && (entry.Kind != "card" || entry.Combat == nil || entry.Combat.LimitCount != limitCount) {
			continue
		}
		text := fmt.Sprintf("%s %s %d %d", entry.Name, entry.Detail, entry.RewardTypeID, entry.PictID)
		if entry.Combat != nil {
			text += " " + entry.Combat.NormalSkill + " " + entry.Combat.ArthurSkill
		}
		if !matchesAdminSearch(text, query) {
			continue
		}
		if total >= offset && len(filtered) < limit {
			filtered = append(filtered, entry)
		}
		total++
	}
	WriteAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "entries": filtered, "total": total,
		"offset": offset, "limit": limit,
	})
}

func matchesAdminSearch(text, query string) bool {
	text = strings.ToLower(text)
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

func (admin *API) catalogAsset(writer http.ResponseWriter, request *http.Request) {
	kind := chi.URLParam(request, "kind")
	file := chi.URLParam(request, "file")
	if kind == "" || file == "" || filepath.Base(file) != file || filepath.Ext(file) != ".webp" {
		WriteAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	urlPath := "/assets/" + kind + "/" + file
	if _, allowed := admin.assetURLs[urlPath]; !allowed {
		WriteAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	path := filepath.Join(admin.assetsRoot, kind, file)
	absolute, err := filepath.Abs(path)
	if err != nil {
		WriteAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	relative, err := filepath.Rel(admin.assetsRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		WriteAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		WriteAdminError(writer, http.StatusNotFound, "admin asset not found")
		return
	}
	writer.Header().Set("Content-Type", "image/webp")
	http.ServeFile(writer, request, absolute)
}

type AdminMailRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	Title          string `json:"title"`
	Message        string `json:"message"`
	Discardable    bool   `json:"discardable"`
	RewardType     int    `json:"reward_type"`
	RewardTypeID   int    `json:"reward_type_id"`
	Quantity       int    `json:"quantity"`
	CardLevel      int    `json:"card_level"`
	CardFame       int    `json:"card_fame"`
	CardLove       int    `json:"card_love"`
}

const (
	adminMaximumInstanceRewardQuantity = 100
	adminMaximumStackedRewardQuantity  = 10000000
)

func adminMailPresentID(userID int, idempotencyKey string) int64 {
	digest := sha256.Sum256([]byte(fmt.Sprintf("cn602-admin-mail\x00%d\x00%s", userID, idempotencyKey)))
	value := binary.BigEndian.Uint64(digest[:8]) & uint64(math.MaxInt64)
	if value == 0 {
		value = 1
	}
	return int64(value)
}

func (admin *API) mailReward(request AdminMailRequest) (gamestate.Reward, AdminCatalogEntry, error) {
	entry, exists := admin.catalogByKey[adminCatalogKey(request.RewardType, request.RewardTypeID)]
	if !exists {
		return gamestate.Reward{}, AdminCatalogEntry{}, errors.New("reward is not present in the active CN catalog")
	}
	if entry.ResourceState == "unavailable" {
		return gamestate.Reward{}, AdminCatalogEntry{}, errors.New("该卡牌尚未进入当前运行资源清单")
	}
	reward, err := adminCatalogReward(request, entry)
	return reward, entry, err
}

// adminCatalogReward validates values against a resolved catalog entry.
// Callers decide whether unavailable assets block a draft or only publication.
func adminCatalogReward(request AdminMailRequest, entry AdminCatalogEntry) (gamestate.Reward, error) {
	maximum := adminMaximumStackedRewardQuantity
	if gamestate.IsCollectionReward(request.RewardType) {
		maximum = 1
	}
	if request.RewardType == 6 || request.RewardType == 15 || request.RewardType == 19 {
		maximum = adminMaximumInstanceRewardQuantity
	}
	if request.Quantity < 1 || request.Quantity > maximum {
		return gamestate.Reward{}, fmt.Errorf("reward quantity must be 1 through %d", maximum)
	}
	reward := gamestate.Reward{
		Type: request.RewardType, Num: request.Quantity,
		RewardTypeID: request.RewardTypeID, CardSkillLevels: []int16{},
	}
	if request.RewardType == 19 {
		if request.CardLevel < 0 || request.CardLevel > entry.LevelMax || request.CardLevel > math.MaxInt16 ||
			request.CardFame != 0 || request.CardLove != 0 {
			return gamestate.Reward{}, errors.New("传承卡只支持按当前形态等级上限配置等级")
		}
		reward.CardLevel = int16(request.CardLevel)
		return reward, nil
	}
	if request.RewardType != 6 {
		if request.CardLevel != 0 || request.CardFame != 0 || request.CardLove != 0 {
			return gamestate.Reward{}, errors.New("card progression fields require a card reward")
		}
		return reward, nil
	}
	if entry.FixedCardLevel > 0 {
		if request.CardLevel != 0 && request.CardLevel != entry.FixedCardLevel {
			return gamestate.Reward{}, errors.New("炼金技能卡获得时固定为满级")
		}
		request.CardLevel = entry.FixedCardLevel
	} else if request.CardLevel == 0 {
		request.CardLevel = 1
	}
	if request.CardFame == 0 {
		request.CardFame = 1
	}
	if request.CardLevel < 1 || request.CardLevel > entry.LevelMax ||
		request.CardFame < 1 || request.CardFame > entry.FameMax ||
		request.CardLove < 0 || request.CardLove > entry.LoveMax ||
		request.CardLevel > math.MaxInt16 || request.CardFame > math.MaxInt16 {
		return gamestate.Reward{}, errors.New("card progression values exceed the official card definition")
	}
	reward.CardLevel = int16(request.CardLevel)
	reward.CardFame = int16(request.CardFame)
	reward.CardLove = request.CardLove
	reward.CardSkillLevels = []int16{1}
	return reward, nil
}

func (admin *API) sendAccountMail(writer http.ResponseWriter, request *http.Request) {
	if err := requireAdminMutation(request); err != nil {
		WriteAdminError(writer, http.StatusForbidden, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(request, "userID"))
	if err != nil || userID < accountstore.PrimaryUserID {
		WriteAdminError(writer, http.StatusBadRequest, "invalid user ID")
		return
	}
	var mail AdminMailRequest
	if err := decodeAdminJSON(request, &mail); err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	mail.IdempotencyKey = strings.TrimSpace(mail.IdempotencyKey)
	mail.Title = strings.TrimSpace(mail.Title)
	mail.Message = strings.TrimSpace(mail.Message)
	if len(mail.IdempotencyKey) < 8 || len(mail.IdempotencyKey) > 128 ||
		len(mail.Title) < 1 || len([]rune(mail.Title)) > 40 ||
		len(mail.Message) < 1 || len([]rune(mail.Message)) > 200 {
		WriteAdminError(writer, http.StatusBadRequest, "mail key, title, or message length is invalid")
		return
	}
	reward, entry, err := admin.mailReward(mail)
	if err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	presentID := adminMailPresentID(userID, mail.IdempotencyKey)
	lock := admin.business.AccountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	state, err := admin.loadAccountState(userID)
	if err != nil {
		WriteAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	for _, group := range [][]gamestate.Present{state.Engagement.Presents, state.Engagement.Histories} {
		for _, existing := range group {
			if existing.AdminIdempotencyKey == mail.IdempotencyKey {
				if existing.PresentID != presentID {
					WriteAdminError(writer, http.StatusConflict, "mail idempotency metadata is inconsistent")
					return
				}
				// A key identifies one immutable mail, not any subsequent request
				// with that key. Receipt changes State in-place; it is not part
				// of the immutable mail content, even before explicit deletion.
				if !adminMailPayloadMatches(existing, mail, reward) {
					WriteAdminError(writer, http.StatusConflict, "mail idempotency key was already used with different content")
					return
				}
				WriteAdminJSON(writer, http.StatusOK, map[string]any{
					"state": "PASS", "user_id": userID, "present_id": presentID,
					"duplicate": true, "discardable": existing.State == 1,
					"reward": existing.Reward, "catalog": entry,
				})
				return
			}
			if existing.PresentID == presentID {
				WriteAdminError(writer, http.StatusConflict, "mail present ID collision")
				return
			}
		}
	}
	emptyReward := gamestate.Reward{CardSkillLevels: []int16{}}
	presentState := int8(0)
	if mail.Discardable {
		presentState = 1
	}
	present := gamestate.Present{
		PresentID: presentID, IssuedAtUnix: time.Now().Unix(), Reward: reward,
		Reward0: emptyReward, Reward1: emptyReward, Reward2: emptyReward,
		Comment: mail.Message, Reason: 0, State: presentState, Title: mail.Title,
		URL: "", AdminIdempotencyKey: mail.IdempotencyKey,
	}
	state.Engagement.Presents = append(state.Engagement.Presents, present)
	auditPayload := map[string]any{
		"idempotency_key": mail.IdempotencyKey, "present_id": presentID,
		"title": mail.Title, "message": mail.Message, "discardable": mail.Discardable,
		"reward":  reward,
		"catalog": map[string]any{"kind": entry.Kind, "name": entry.Name},
	}
	if err := admin.accounts.PersistStateWithAudit(userID, state, &accountstore.AdminAudit{
		Operation: "account-mail", Target: strconv.Itoa(userID), Payload: auditPayload,
	}); err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	admin.business.Invalidate(userID)
	WriteAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "user_id": userID, "present_id": presentID,
		"duplicate": false, "discardable": mail.Discardable,
		"reward": reward, "catalog": entry,
		"audit_ok": true,
	})
}

func adminMailPayloadMatches(existing gamestate.Present, mail AdminMailRequest, reward gamestate.Reward) bool {
	previous := existing.Reward
	return existing.Title == mail.Title && existing.Comment == mail.Message &&
		previous.Type == reward.Type && previous.Num == reward.Num && previous.RewardTypeID == reward.RewardTypeID &&
		previous.CardLevel == reward.CardLevel && previous.CardFame == reward.CardFame && previous.CardLove == reward.CardLove &&
		slices.Equal(previous.CardSkillLevels, reward.CardSkillLevels)
}

func (admin *API) grantAccountResources(writer http.ResponseWriter, request *http.Request) {
	if err := requireAdminMutation(request); err != nil {
		WriteAdminError(writer, http.StatusForbidden, err.Error())
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(request, "userID"))
	if err != nil || userID < accountstore.PrimaryUserID {
		WriteAdminError(writer, http.StatusBadRequest, "invalid user ID")
		return
	}
	var grant struct {
		IdempotencyKey   string `json:"idempotency_key"`
		Gold             int    `json:"gold"`
		FriendPoint      int    `json:"friend_point"`
		FreeCrystal      int    `json:"free_crystal"`
		PaidCrystal      int    `json:"paid_crystal"`
		TargetLevel      int    `json:"target_level"`
		TargetArthurRank int    `json:"target_arthur_rank"`
		FillAP           bool   `json:"fill_ap"`
		FillBP           bool   `json:"fill_bp"`
	}
	if err := decodeAdminJSON(request, &grant); err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	for _, value := range []int{grant.Gold, grant.FriendPoint, grant.FreeCrystal, grant.PaidCrystal} {
		if value < 0 || value > 10000000 {
			WriteAdminError(writer, http.StatusBadRequest, "each resource grant must be 0 through 10,000,000")
			return
		}
	}
	if grant.TargetLevel < 0 || grant.TargetLevel > admin.progression.MaxLevel {
		WriteAdminError(writer, http.StatusBadRequest, "target level is outside the active progression policy")
		return
	}
	if grant.TargetArthurRank < 0 || grant.TargetArthurRank > adminMaximumArthurRank {
		WriteAdminError(writer, http.StatusBadRequest, "target Arthur rank is outside the CN client deck-rank range")
		return
	}
	if grant.Gold == 0 && grant.FriendPoint == 0 && grant.FreeCrystal == 0 && grant.PaidCrystal == 0 && grant.TargetLevel == 0 && grant.TargetArthurRank == 0 && !grant.FillAP && !grant.FillBP {
		WriteAdminError(writer, http.StatusBadRequest, "resource grant is empty")
		return
	}
	if grant.IdempotencyKey != "" && !validBatchID(grant.IdempotencyKey) {
		WriteAdminError(writer, 400, "操作编号无效")
		return
	}
	requestJSON, _ := json.Marshal(grant)
	digest := sha256.Sum256(requestJSON)
	requestSHA := hex.EncodeToString(digest[:])
	operationKey := "account-grant:" + grant.IdempotencyKey
	lock := admin.business.AccountLock(userID)
	lock.Lock()
	defer lock.Unlock()
	if grant.IdempotencyKey != "" {
		receipts := &Operations{storage: admin.accounts.Database()}
		previous, err := receipts.storage.ActionReceipt(operationKey, userID, requestSHA)
		if err != nil {
			WriteAdminError(writer, 409, err.Error())
			return
		}
		if previous != nil {
			WriteAdminJSON(writer, 200, previous)
			return
		}
	}
	state, err := admin.loadAccountState(userID)
	if err != nil {
		WriteAdminError(writer, http.StatusNotFound, "account not found")
		return
	}
	if err := applyAdminTargetLevel(&state, admin.progression, grant.TargetLevel); err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if err := applyAdminTargetArthurRank(&state, grant.TargetArthurRank); err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	for _, pair := range [][2]int{{state.User.Gold, grant.Gold}, {state.User.FriendPoint, grant.FriendPoint}, {state.User.CoinFree, grant.FreeCrystal}, {state.User.Coin, grant.PaidCrystal}} {
		if pair[1] > math.MaxInt-pair[0] {
			WriteAdminError(writer, http.StatusBadRequest, "resource total would overflow")
			return
		}
	}
	state.User.Gold += grant.Gold
	state.User.FriendPoint += grant.FriendPoint
	state.User.CoinFree += grant.FreeCrystal
	state.User.Coin += grant.PaidCrystal
	if grant.FillAP {
		state.User.AP = state.User.APMax
		state.Explore.APNextRecoveryUnix = 0
	}
	if grant.FillBP {
		state.User.BP = state.User.BPMax
		state.BattlePoint.NextRecoveryUnix = 0
	}
	response := map[string]any{
		"state": "PASS", "user_id": userID, "gold": state.User.Gold,
		"friend_point": state.User.FriendPoint,
		"free_crystal": state.User.CoinFree, "paid_crystal": state.User.Coin,
		"level": state.User.Level, "arthur_rank": state.User.ArthurRank, "ap": state.User.AP,
		"bp": state.User.BP, "bp_max": state.User.BPMax, "audit_ok": true,
	}
	var receipt *accountstore.AdminActionReceipt
	if grant.IdempotencyKey != "" {
		receipt = &accountstore.AdminActionReceipt{OperationKey: operationKey, UserID: userID, RequestSHA256: requestSHA, Result: response}
	}
	auditPayload := map[string]any{
		"request": grant, "result": map[string]int{
			"gold": state.User.Gold, "friend_point": state.User.FriendPoint, "free_crystal": state.User.CoinFree,
			"paid_crystal": state.User.Coin, "level": state.User.Level, "arthur_rank": state.User.ArthurRank, "ap": state.User.AP,
			"bp": state.User.BP, "bp_max": state.User.BPMax,
		},
	}
	if err := admin.accounts.PersistStateWithAudit(userID, state, &accountstore.AdminAudit{
		Operation: "account-grant", Target: strconv.Itoa(userID), Payload: auditPayload, Receipt: receipt,
	}); err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	admin.business.Invalidate(userID)
	WriteAdminJSON(writer, http.StatusOK, response)
}

func applyAdminTargetArthurRank(state *gamestate.State, target int) error {
	if target == 0 {
		return nil
	}
	if target < state.User.ArthurRank {
		return errors.New("admin target Arthur rank cannot lower an account")
	}
	if target > adminMaximumArthurRank {
		return errors.New("admin target Arthur rank is outside the CN client deck-rank range")
	}
	state.User.ArthurRank = target
	return nil
}

func applyAdminTargetLevel(state *gamestate.State, policy gamestate.PlayerProgressionPolicy, target int) error {
	if target == 0 {
		return nil
	}
	if target < state.User.Level {
		return errors.New("admin target level cannot lower an account")
	}
	if target > policy.MaxLevel || policy.MaxLevel <= 0 {
		return errors.New("admin target level is outside the active progression policy")
	}
	if target == state.User.Level {
		return nil
	}
	state.User.Level = target
	state.User.Experience = policy.CumulativeExperience(target)
	state.User.NowLevelExperience = 0
	state.User.NextLevelExperience = 0
	if target < policy.MaxLevel {
		state.User.NextLevelExperience = policy.ExperienceRequired(target)
	}
	state.User.BPMax = policy.BattlePointMaximum(target)
	state.User.FollowMax = policy.FollowMaximum(target)
	state.User.Jobs = policy.JobsAtLevel(target)
	state.User.AP = state.User.APMax
	state.Explore.APNextRecoveryUnix = 0
	state.User.BP = state.User.BPMax
	state.BattlePoint.NextRecoveryUnix = 0
	return masterdata.ValidatePlayerProgressionState(state.User, policy)
}

func (admin *API) bossGroups(writer http.ResponseWriter, request *http.Request) {
	_, groups, _, err := admin.bossCatalog(request)
	if err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	WriteAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "groups": groups})
}

func (admin *API) publicationState() (adminPublicationState, error) {
	doc, err := admin.operations.storage.ReadDocument(teamBattlePublicationKey)
	if err != nil {
		return adminPublicationState{}, err
	}
	return adminPublicationFromDocument(doc)
}

func adminPublicationFromDocument(doc accountstore.Document) (adminPublicationState, error) {
	publication := TeamBattlePublication{Mode: "all", GroupIDs: []int{}}
	if doc.Revision != 0 {
		if err := json.Unmarshal(doc.Payload, &publication); err != nil {
			return adminPublicationState{}, err
		}
	}
	if publication.GroupIDs == nil {
		publication.GroupIDs = []int{}
	}
	return adminPublicationState{
		Mode: publication.Mode, GroupIDs: publication.GroupIDs, StartUnix: publication.StartUnix, EndUnix: publication.EndUnix,
		Revision: doc.Revision, UpdatedUTC: doc.UpdatedUTC,
	}, nil
}

func (admin *API) bossPolicy(writer http.ResponseWriter, request *http.Request) {
	key, _, _, err := admin.bossCatalog(request)
	if err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	doc, err := admin.operations.storage.ReadDocument(key)
	if err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	policy, err := adminPublicationFromDocument(doc)
	if err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	result := map[string]any{"state": "PASS", "publication": policy}
	if key == towerPublicationKey {
		result["towers"] = admin.towerPublicationOptions(policy)
	}
	WriteAdminJSON(writer, http.StatusOK, result)
}

func (admin *API) setBossPolicy(writer http.ResponseWriter, request *http.Request) {
	if err := requireAdminMutation(request); err != nil {
		WriteAdminError(writer, http.StatusForbidden, err.Error())
		return
	}
	key, _, known, err := admin.bossCatalog(request)
	if err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	var publication TeamBattlePublication
	if err := decodeAdminJSON(request, &publication); err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if publication.Mode == "allowlist" {
		for _, groupID := range publication.GroupIDs {
			if _, exists := known[groupID]; !exists {
				WriteAdminError(writer, http.StatusBadRequest, fmt.Sprintf("unknown group ID %d", groupID))
				return
			}
		}
	}
	var doc accountstore.Document
	if key == towerPublicationKey {
		doc, err = admin.operations.SetTowerPublication(publication)
	} else {
		doc, err = admin.operations.setBattlePublication(key, publication)
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			status = http.StatusConflict
		}
		WriteAdminError(writer, status, err.Error())
		return
	}
	policy, err := adminPublicationFromDocument(doc)
	if err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	result := map[string]any{"state": "PASS", "publication": policy}
	if key == towerPublicationKey {
		result["towers"] = admin.towerPublicationOptions(policy)
	}
	WriteAdminJSON(writer, http.StatusOK, result)
}

func (admin *API) gachaPresetList(writer http.ResponseWriter, _ *http.Request) {
	admin.operations.configMu.RLock()
	defer admin.operations.configMu.RUnlock()
	presets := append(append([]AdminGachaPreset(nil), admin.gachaPresets...), admin.customGachaPresets()...)
	for i := range presets {
		for _, config := range admin.operations.gachaConfigurations {
			if len(presets[i].GachaIDs) > 0 && presets[i].GachaIDs[0] == config.Profile.GachaID {
				presets[i].Name = config.Profile.Name
				presets[i].Price = config.Profile.Price
				presets[i].CardCount = len(config.Profile.CardIDs) + len(config.Profile.RewardPool)
				presets[i].CategoryNum = config.Profile.CategoryNum
				if config.Profile.CoverPath != "" {
					presets[i].ImageURL = "/" + config.Profile.CoverPath
				}
			}
		}
	}

	for i := range presets {
		if category, ok := gachaEditorCategoryByID(presets[i].CategoryNum); ok && category.ID != 0 {
			presets[i].Category = category.Name
		}
	}

	WriteAdminJSON(writer, http.StatusOK, map[string]any{
		"state": "PASS", "presets": presets,
	})
}

func (admin *API) gachaAsset(writer http.ResponseWriter, request *http.Request) {
	fileName := chi.URLParam(request, "file")
	if filepath.Base(fileName) != fileName || filepath.Ext(fileName) != ".png" {
		WriteAdminError(writer, http.StatusNotFound, "gacha asset not found")
		return
	}
	key := strings.TrimSuffix(fileName, ".png")
	assetPath, exists := admin.gachaBannerPaths[key]
	if !exists {
		WriteAdminError(writer, http.StatusNotFound, "gacha asset not found")
		return
	}
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("Cache-Control", "no-store")
	http.ServeFile(writer, request, assetPath)
}

func (admin *API) gachaPublicationState() (adminGachaPublicationState, error) {
	doc, err := admin.operations.storage.ReadDocument(gachaPublicationKey)
	if err != nil {
		return adminGachaPublicationState{}, err
	}
	return admin.gachaPublicationFromDocument(doc)
}

func (admin *API) gachaPublicationFromDocument(doc accountstore.Document) (adminGachaPublicationState, error) {
	active, err := admin.operations.DecodeGachaPublication(doc)
	if err != nil {
		return adminGachaPublicationState{}, err
	}
	groupIDs := make([]int, 0, len(active))
	for groupID := range active {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Ints(groupIDs)
	return adminGachaPublicationState{GroupIDs: groupIDs, Revision: doc.Revision, UpdatedUTC: doc.UpdatedUTC}, nil
}

func (admin *API) gachaPolicy(writer http.ResponseWriter, _ *http.Request) {
	policy, err := admin.gachaPublicationState()
	if err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	WriteAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "publication": policy})
}

func (admin *API) setGachaPolicy(writer http.ResponseWriter, request *http.Request) {
	if err := requireAdminMutation(request); err != nil {
		WriteAdminError(writer, http.StatusForbidden, err.Error())
		return
	}
	var publication gachaPublication
	if err := decodeAdminJSON(request, &publication); err != nil {
		WriteAdminError(writer, http.StatusBadRequest, err.Error())
		return
	}
	known := admin.operations.ManagedGroups()
	for _, groupID := range publication.GroupIDs {
		if _, exists := known[groupID]; !exists {
			WriteAdminError(writer, http.StatusBadRequest, fmt.Sprintf("unknown gacha group ID %d", groupID))
			return
		}
	}
	doc, err := admin.operations.setGachaPublication(publication)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, accountstore.ErrDocumentConflict) {
			status = http.StatusConflict
		}
		WriteAdminError(writer, status, err.Error())
		return
	}
	policy, err := admin.gachaPublicationFromDocument(doc)
	if err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	WriteAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "publication": policy})
}

func (admin *API) audit(writer http.ResponseWriter, request *http.Request) {
	limit, offset, err := adminPage(request, 50, 200)
	if err != nil {
		WriteAdminError(writer, 400, err.Error())
		return
	}
	query := request.URL.Query()
	filter := accountstore.AuditFilter{Search: strings.TrimSpace(query.Get("q")), Operation: strings.TrimSpace(query.Get("operation"))}
	for key, target := range map[string]*string{"after": &filter.After, "before": &filter.Before} {
		if value := query.Get(key); value != "" {
			stamp, err := time.Parse(time.RFC3339, value)
			if err != nil {
				WriteAdminError(writer, 400, "时间须为有效日期")
				return
			}
			*target = stamp.UTC().Format(time.RFC3339)
		}
	}
	if filter.After != "" && filter.Before != "" && filter.After >= filter.Before {
		WriteAdminError(writer, 400, "开始时间须早于结束时间")
		return
	}
	records, total, err := admin.accounts.QueryAuditRecords(filter, limit, offset)
	if err != nil {
		WriteAdminError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	WriteAdminJSON(writer, http.StatusOK, map[string]any{"state": "PASS", "records": records, "total": total, "limit": limit, "offset": offset})
}
