package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/netstats"
	"kairisei.local/server/internal/objectstore"
)

// AccountRuntime coordinates mutations with in-flight gameplay and drops cached
// handlers after a durable administrative update.
type AccountRuntime interface {
	AccountLock(int) *sync.Mutex
	Invalidate(int)
}

type Config struct {
	Accounts       *accountstore.Accounts
	Runtime        AccountRuntime
	Operations     *Operations
	BattleMaster   string
	CardMaster     string
	ItemMaster     string
	PVPRanks       []game.PVPRankConfig
	Multiplayer    *multiplayer.Hub
	AdvertiseHost  string
	GamePort       int
	Logger         *slog.Logger
	NetworkMetrics *netstats.Counters
	RuntimeLogPath string
	Progression    gamestate.PlayerProgressionPolicy
	GachaBanners   map[string]string
	AssetMaps      []string
	// GachaCoverDir is the writable folder for uploaded pool covers (next to the save database).
	GachaCoverDir string
	// HomeBannerDir is the writable folder for uploaded home banners (next to the save database).
	HomeBannerDir string
	// ImageStorage is the optional cdn-sync.json connection for 同步运营图片; unset leaves it off.
	ImageStorage objectstore.Setting
}

func New(config Config) (http.Handler, error) {
	if config.Accounts == nil || config.Runtime == nil || config.Operations == nil || config.Multiplayer == nil {
		return nil, errors.New("CN admin runtime is incomplete")
	}
	master, err := masterdata.LoadBattleRuntimeMaster(config.BattleMaster)
	if err != nil {
		return nil, err
	}
	master, err = masterdata.ExpandBattleEntries(master)
	if err != nil {
		return nil, err
	}
	groups := make([]AdminBattleGroup, 0, len(master.Groups)+4)
	knownGroups := make(map[int]struct{}, len(master.Groups)+4)
	knownBosses := make(map[int]struct{}, len(master.Replays)+4)
	replaySegments := make(map[int]int, len(master.Replays))
	for _, replay := range master.Replays {
		count := len(replay.Battles)
		if count == 0 {
			count = 1
		}
		replaySegments[replay.BossID] = count
	}
	appendGroup := func(raw json.RawMessage) error {
		var group struct {
			GroupID   int    `json:"0"`
			Name      string `json:"4"`
			PictureID int    `json:"7"`
			Bosses    []struct {
				BossID     int    `json:"0"`
				Difficulty string `json:"4"`
				IsModel    int    `json:"14"`
			} `json:"10"`
		}
		if err := json.Unmarshal(raw, &group); err != nil {
			return fmt.Errorf("decode CN admin battle group: %w", err)
		}
		if group.GroupID <= 0 || len(group.Bosses) == 0 {
			return errors.New("CN admin battle group has an invalid identity or no bosses")
		}
		if _, exists := knownGroups[group.GroupID]; exists {
			return nil
		}
		bossIDs := make([]int, len(group.Bosses))
		difficulties := make([]string, 0, len(group.Bosses))
		segmentCounts := make([]int, len(group.Bosses))
		maxSegments := 1
		category := "2d"
		for index, boss := range group.Bosses {
			if boss.BossID <= 0 {
				return errors.New("CN admin battle group has an invalid boss ID")
			}
			bossIDs[index] = boss.BossID
			if boss.Difficulty != "" && !slices.Contains(difficulties, boss.Difficulty) {
				difficulties = append(difficulties, boss.Difficulty)
			}
			segmentCounts[index] = max(1, replaySegments[boss.BossID])
			maxSegments = max(maxSegments, segmentCounts[index])
			if _, material := masterdata.BattleMaterialGroupIcons[boss.BossID/100]; material {
				category = "material"
			} else if boss.IsModel == 1 && category != "material" {
				category = "3d"
			}
			knownBosses[boss.BossID] = struct{}{}
		}
		groups = append(groups, AdminBattleGroup{
			GroupID: group.GroupID, Name: group.Name, PictureID: group.PictureID,
			BossCount: len(bossIDs), BossIDs: bossIDs, Category: category,
			Difficulties: difficulties, SegmentCounts: segmentCounts, MaxSegments: maxSegments,
			ImageURL: fmt.Sprintf("/assets/boss/%d.webp", group.PictureID),
		})
		knownGroups[group.GroupID] = struct{}{}
		return nil
	}
	for _, raw := range master.Groups {
		if err := appendGroup(raw); err != nil {
			return nil, err
		}
	}
	primaryState, err := config.Accounts.LoadState(accountstore.PrimaryUserID)
	if err != nil {
		return nil, fmt.Errorf("load CN admin baseline battle groups: %w", err)
	}
	cardMaster, err := masterdata.LoadCardRuntimeMaster(config.CardMaster)
	if err != nil {
		return nil, err
	}
	itemMaster, err := masterdata.LoadItemRuntimeMaster(config.ItemMaster)
	if err != nil {
		return nil, err
	}
	itemNames := make(map[int]string, len(itemMaster.Items))
	for _, item := range itemMaster.Items {
		itemNames[item.ItemID] = item.Name
	}
	presetsByGroup := make(map[int]*AdminGachaPreset)
	for _, gacha := range primaryState.Gachas {
		if gacha.GachaID == 90000100 || gacha.GachaID == 90000200 {
			continue
		}
		bannerPath, exists := config.GachaBanners[gacha.BannerKey]
		if !exists || bannerPath == "" {
			return nil, fmt.Errorf("CN managed gacha %d has no published banner", gacha.GachaID)
		}
		preset := presetsByGroup[gacha.GroupID]
		if preset == nil {
			payment := itemNames[gacha.PayTypeID]
			switch gacha.PayType {
			case 2:
				payment = "友情点"
			case 3:
				payment = "水晶"
			case 6:
				payment = "付费水晶"
			}
			preset = &AdminGachaPreset{
				PublicationKey: gacha.PublicationKey, GroupID: gacha.GroupID,
				Name: gacha.Name, BannerKey: gacha.BannerKey,
				ImageURL:      "/gacha-assets/" + gacha.BannerKey + ".png",
				PaymentItemID: gacha.PayTypeID, PaymentItem: payment,
				Price: gacha.Price, DrawCount: gacha.CardNum,
				CardCount:   len(gacha.CardIDs) + len(gacha.RewardPool),
				CategoryNum: gacha.CategoryNum,
			}
			presetsByGroup[gacha.GroupID] = preset
		} else if preset.PublicationKey != gacha.PublicationKey {
			return nil, fmt.Errorf("CN managed gacha group %d has inconsistent metadata", gacha.GroupID)
		}
		preset.GachaIDs = append(preset.GachaIDs, gacha.GachaID)
	}
	gachaPresets := make([]AdminGachaPreset, 0, len(presetsByGroup))
	for _, preset := range presetsByGroup {
		sort.Ints(preset.GachaIDs)
		gachaPresets = append(gachaPresets, *preset)
	}
	sort.Slice(gachaPresets, func(left, right int) bool {
		return gachaPresets[left].GroupID < gachaPresets[right].GroupID
	})
	catalog, _, err := BuildAdminCatalog(cardMaster, itemMaster)
	if err != nil {
		return nil, err
	}
	hasAlchemistCards := false
	for _, card := range cardMaster.CardTemplates {
		if card.RarityPlus == 4 {
			hasAlchemistCards = true
			break
		}
	}
	combatCardPath := filepath.Join(filepath.Dir(config.CardMaster), "cn602-card-master", "card.csv")
	if _, statErr := os.Stat(combatCardPath); statErr == nil {
		var alchemistPaths []string
		if hasAlchemistCards {
			alchemistPaths = append(alchemistPaths, filepath.Join(filepath.Dir(combatCardPath), "card_alchemist.csv"))
		}
		info, err := multiplayer.LoadOperationsCardCatalog(combatCardPath, filepath.Join(filepath.Dir(config.BattleMaster), "cn602-battle-master"), alchemistPaths...)
		if err != nil {
			return nil, fmt.Errorf("load admin card details: %w", err)
		}
		if err := attachAdminCardDetails(catalog, cardMaster.CardTemplates, info); err != nil {
			return nil, err
		}
	} else if os.IsNotExist(statErr) && hasAlchemistCards {
		return nil, errors.New("alchemist card combat definitions are unavailable")
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	assetsRoot, err := filepath.Abs(filepath.Join(filepath.Dir(config.CardMaster), "cn602-admin-assets"))
	if err != nil {
		return nil, fmt.Errorf("resolve CN admin asset root: %w", err)
	}
	var cardResources map[int]adminCardResource
	if len(config.AssetMaps) > 0 {
		cardResources, err = loadAdminCardResources(cardMaster, config.CardMaster, config.AssetMaps[0])
		if err != nil {
			return nil, err
		}
	}
	catalog, err = applyAdminCatalogAssetCoverage(catalog, assetsRoot, cardResources)
	if err != nil {
		return nil, fmt.Errorf("validate CN admin catalog image coverage: %w", err)
	}
	if config.Operations.content != nil {
		for _, definition := range config.Operations.content.base.CollectionRewards {
			kind := map[int]string{14: "costume", 16: "stamp", 18: "honor"}[definition.Type]
			imageURL := ""
			if kind == "costume" || kind == "stamp" {
				imageURL, err = adminOptionalImageURL(assetsRoot, fmt.Sprintf("/assets/%s/%d.webp", kind, definition.ID))
				if err != nil {
					return nil, fmt.Errorf("validate CN admin %s %d image: %w", kind, definition.ID, err)
				}
			}
			catalog = append(catalog, AdminCatalogEntry{Kind: kind, RewardType: definition.Type, RewardTypeID: definition.ID,
				Name: definition.Name, Detail: definition.Detail, PictID: definition.PictID, ImageURL: imageURL, StampKind: definition.StampKind})
		}
	}
	catalogByKey := make(map[string]AdminCatalogEntry, len(catalog))
	for _, entry := range catalog {
		catalogByKey[adminCatalogKey(entry.RewardType, entry.RewardTypeID)] = entry
	}
	for index := range groups {
		imageURL, err := adminOptionalImageURL(assetsRoot, groups[index].ImageURL)
		if err != nil {
			return nil, fmt.Errorf("validate CN admin boss group %d image: %w", groups[index].GroupID, err)
		}
		groups[index].ImageURL = imageURL
	}
	pastGroups, err := buildAdminPastGroups(master.PastBossGroups, groups, catalogByKey)
	if err != nil {
		return nil, err
	}
	if content := config.Operations.content; content != nil {
		for _, list := range [][]AdminBattleGroup{groups, pastGroups} {
			for i := range list {
				for _, id := range list[i].BossIDs {
					if content.rewardBossID(id) != id {
						continue
					}
					if boss, ok := content.bosses[id]; ok {
						list[i].Bosses = append(list[i].Bosses, boss)
					}
				}
			}
		}
	}
	assetURLs := make(map[string]struct{}, len(catalog)+len(groups))
	for _, entry := range catalog {
		if entry.ImageURL != "" {
			assetURLs[entry.ImageURL] = struct{}{}
		}
	}
	for _, group := range groups {
		if group.ImageURL != "" {
			assetURLs[group.ImageURL] = struct{}{}
		}
	}
	admin := &API{
		accounts: config.Accounts, business: config.Runtime, operations: config.Operations,
		groups: groups, catalog: catalog, catalogByKey: catalogByKey,
		pastGroups: pastGroups,
		assetsRoot: assetsRoot, assetURLs: assetURLs,
		knownGroups: knownGroups, bossCount: len(knownBosses),
		multiplayerHub: config.Multiplayer, advertiseHost: config.AdvertiseHost,
		gamePort: config.GamePort, logger: config.Logger, progression: config.Progression, pvpRanks: config.PVPRanks,
		networkMetrics: config.NetworkMetrics,
		gachaPresets:   gachaPresets, gachaCoverDir: config.GachaCoverDir, homeBannerDir: config.HomeBannerDir,
		gachaBannerPaths: config.GachaBanners, imageStorage: config.ImageStorage, imageSync: &imageSyncState{},
	}
	if config.RuntimeLogPath != "" {
		admin.runtimeLog = &runtimeLogReader{path: config.RuntimeLogPath}
	}
	if err := admin.validateStoredGachas(); err != nil {
		return nil, err
	}
	if policy := config.Operations.playerPolicy.Load(); policy != nil {
		if err := admin.validateTutorialMail(policy.Value.TutorialMail); err != nil {
			return nil, fmt.Errorf("validate saved tutorial mail: %w", err)
		}
	}
	if config.Operations.content != nil {
		for _, config := range config.Operations.content.drops {
			if err := admin.validateDropConfig(config); err != nil {
				return nil, fmt.Errorf("saved drop configuration: %w", err)
			}
		}
		for _, shop := range config.Operations.content.shops {
			if shop.Deleted {
				continue
			}
			if err := admin.validateExchange(shop.TradeShopProfile, config.Operations.content.exchangeProfiles()); err != nil {
				return nil, fmt.Errorf("saved exchange configuration: %w", err)
			}
		}
	}
	router := chi.NewRouter()
	router.Use(adminSecurityHeaders)
	router.Use(admin.maintenanceWrites)
	if err := config.Multiplayer.SetMaintenance(config.Operations.MaintenanceState().Enabled); err != nil {
		return nil, err
	}
	router.Get("/api/maintenance", admin.getMaintenance)
	router.Put("/api/maintenance", admin.setMaintenance)
	router.Post("/api/maintenance/cleanup", admin.cleanupOperationalState)
	router.Get("/api/maintenance/history", admin.getMaintenanceHistory)
	router.Post("/api/maintenance/battles/interrupt", admin.interruptBattles)
	router.Get("/api/evolution-policy", admin.evolutionEditor)
	router.Put("/api/evolution-policy", admin.saveEvolutionPolicy)
	router.Get("/api/player-policy", admin.playerPolicy)
	router.Get("/api/popup-announcements", admin.popupAnnouncements)
	router.Post("/api/popup-announcements", admin.popupAnnouncementAction)
	router.Get("/api/player-policy/notice-preview", admin.operations.LocalNotice)
	router.Put("/api/player-policy", admin.savePlayerPolicy)
	router.Get("/player-policy.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(adminPlayerPolicyJS)
	})
	router.Get("/home-banners.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(adminHomeBannersJS)
	})
	router.Get("/", admin.index)
	for path, content := range map[string][]byte{
		"/admin.js": adminJS, "/accounts.js": adminAccountsJS, "/mail.js": adminMailJS, "/settings.js": adminSettingsJS,
		"/evolution.js": adminEvolutionJS, "/insights.js": adminInsightsJS, "/kit.js": adminKitJS, "/missions.js": adminMissionsJS,
		"/gift-codes.js": adminGiftCodesJS, "/pool-rules.js": adminPoolRulesJS, "/maintenance-history.js": adminMaintenanceHistoryJS,
		"/image-sync.js":          adminImageSyncJS,
		"/access.js":              adminAccessJS,
		"/runtime-logs.js":        adminRuntimeLogsJS,
		"/pool-gifts.js":          adminPoolGiftsJS,
		"/popup-announcements.js": adminPopupAnnouncementsJS,
		"/boss-economy.js":        adminBossEconomyJS,
		"/admin.css":              adminCSS, "/editors.css": adminEditorsCSS,
	} {
		router.Get(path, func(w http.ResponseWriter, _ *http.Request) {
			contentType := "text/javascript; charset=utf-8"
			if strings.HasSuffix(path, ".css") {
				contentType = "text/css; charset=utf-8"
			}
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(content)
		})
	}
	router.Get("/favicon.ico", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})
	router.Get("/operations.js", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = writer.Write(adminOperationsJS)
	})
	router.Get("/api/gift-code-batches", admin.giftBatches)
	router.Post("/api/gift-code-batches", admin.saveGiftBatch)
	router.Post("/api/gift-codes/generate", admin.generateGiftCodes)
	router.Post("/api/gift-codes/state", admin.changeGiftCode)
	router.Get("/api/gift-code-batches/{batchID}/codes", admin.listGiftCodes)
	router.Get("/api/accounts/{userID}/missions", admin.accountMissions)
	router.Get("/api/mission-series", admin.missionSeriesList)
	router.Get("/api/mission-targets", admin.missionTargets)
	router.Post("/api/mission-series", admin.missionSeriesAction)
	router.Post("/api/mail-preview", admin.mailPreview)
	router.Post("/api/mail-batches/preview", admin.previewMailBatch)
	router.Post("/api/mail-batches", admin.createMailBatch)
	router.Get("/api/mail-batches", admin.listMailBatches)
	router.Get("/api/mail-batches/{batchID}", admin.mailBatchStatus)
	router.Post("/api/mail-batches/{batchID}/run", admin.runMailBatch)
	router.Get("/api/health", admin.health)
	router.Get("/api/status", admin.status)
	router.Get("/api/runtime-logs", admin.getRuntimeLogs)
	router.Get("/api/settings", admin.runtimeSettings)
	router.Get("/content.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(adminContentJS)
	})
	router.Get("/api/boss-drops", admin.dropEditor)
	router.Get("/api/activity-rewards", admin.activityRewardEditor)
	router.Put("/api/activity-rewards", admin.saveActivityRewards)
	router.Get("/api/boss-rules", admin.bossRules)
	router.Put("/api/boss-rules", admin.saveBossRules)
	router.Get("/api/boss-economy", admin.bossEconomy)
	router.Put("/api/boss-economy", admin.saveBossEconomy)
	router.Put("/api/boss-drops", admin.saveDropEditor)
	router.Get("/tower-drops.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(adminTowerDropsJS)
	})
	router.Get("/api/tower-drops", admin.towerDropEditor)
	router.Put("/api/tower-drops", admin.saveTowerDropEditor)
	router.Get("/api/exchanges", admin.exchangeEditor)
	router.Put("/api/exchanges/{shopID}", admin.saveExchangeEditor)
	router.Delete("/api/exchanges/{shopID}", admin.changeExchangeDeletion)
	router.Post("/api/exchanges/{shopID}/restore", admin.changeExchangeDeletion)
	router.Put("/api/settings", admin.setRuntimeSettings)
	router.Get("/api/settings/ip-blacklist", admin.ipBlacklist)
	router.Put("/api/settings/ip-blacklist", admin.setIPBlacklist)
	router.Get("/api/settings/login-whitelist", admin.loginWhitelist)
	router.Put("/api/settings/login-whitelist", admin.setLoginWhitelist)
	router.Get("/api/settings/module-switches", admin.moduleSwitches)
	router.Put("/api/settings/module-switches", admin.setModuleSwitches)
	router.Put("/api/item-shop", admin.setItemShop)
	router.Get("/api/accounts", admin.accountList)
	router.Post("/api/accounts/resolve", admin.resolveAccounts)
	router.Get("/api/accounts/{userID}", admin.accountDetail)
	router.Put("/api/accounts/{userID}/login-block", admin.changeLoginBlock)
	router.Delete("/api/accounts/{userID}/login-block", admin.changeLoginBlock)
	router.Post("/api/accounts/{userID}/grant", admin.grantAccountResources)
	router.Post("/api/accounts/{userID}/mail", admin.sendAccountMail)
	router.Get("/api/accounts/{userID}/mail", admin.listAccountMail)
	router.Post("/api/accounts/{userID}/mail/delete", admin.deleteAccountMail)
	router.Post("/api/accounts/{userID}/credentials", admin.setAccountCredentials)
	router.Delete("/api/accounts/{userID}/credentials", admin.unbindAccount)
	router.Get("/api/catalog", admin.catalogEntries)
	router.Post("/api/catalog/resolve", admin.resolveCatalog)
	router.Get("/assets/{kind}/{file}", admin.catalogAsset)
	router.Get("/api/boss-groups", admin.bossGroups)
	router.Get("/api/boss-policy", admin.bossPolicy)
	router.Put("/api/boss-policy", admin.setBossPolicy)
	router.Get("/api/gacha-editor", admin.gachaEditorList)
	router.Post("/api/gacha-pools", admin.createCustomGacha)
	router.Post("/api/gacha-pools/{gachaID}/{action:delete|restore}", admin.changeCustomGachaDeletion)
	router.Post("/api/gacha-covers", admin.uploadGachaCover)
	router.Get("/gacha-covers/{file}", ServeGachaCover(config.GachaCoverDir))
	router.Post("/api/home-banners", admin.uploadHomeBanner)
	router.Get("/home-banners/{file}", ServeHomeBanner(config.HomeBannerDir))
	router.Get("/api/image-sync", admin.imageSyncStatus)
	router.Post("/api/image-sync", admin.startImageSync)
	router.Post("/api/image-sync/activate", admin.activateImageSync)
	router.Post("/api/gacha-editor/{action}", admin.gachaEditorAction)
	router.Post("/api/gacha-editor/group/{action:draft|publish|discard}", admin.gachaGroupAction)
	router.Get("/api/gacha-presets", admin.gachaPresetList)
	router.Get("/api/gacha-policy", admin.gachaPolicy)
	router.Put("/api/gacha-policy", admin.setGachaPolicy)
	router.Get("/gacha-assets/{file}", admin.gachaAsset)
	router.Get("/api/audit", admin.audit)
	router.NotFound(func(writer http.ResponseWriter, _ *http.Request) {
		WriteAdminError(writer, http.StatusNotFound, "admin route not found")
	})
	return &lifecycleHandler{Handler: router, images: admin.imageSync}, nil
}
