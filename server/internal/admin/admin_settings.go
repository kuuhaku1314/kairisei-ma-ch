package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

const runtimeSettingsKey = "runtime-settings"

func (operations *Operations) loadRuntimeSettings() error {
	doc, err := operations.storage.ReadDocument(runtimeSettingsKey)
	if err != nil {
		return err
	}
	settings := game.RuntimeSettings{AccountCreationDailyLimit: game.DefaultAccountCreationDailyLimit}
	if doc.Revision != 0 {
		stored := struct {
			*game.RuntimeSettings
			CreationLimit            json.RawMessage `json:"account_creation_daily_limit"`
			CreationVerificationBits json.RawMessage `json:"account_creation_verification_bits"`
		}{RuntimeSettings: &settings}
		if err := json.Unmarshal(doc.Payload, &stored); err != nil {
			return err
		}
		settings.AccountCreationDailyLimit, err = game.ParseAccountCreationDailyLimit(stored.CreationLimit, game.DefaultAccountCreationDailyLimit)
		if err != nil {
			return err
		}
		settings.AccountCreationVerificationBits, err = game.ParseAccountCreationVerificationBits(stored.CreationVerificationBits, 0)
		if err != nil {
			return err
		}
	}
	if err := operations.validateItemShopSettings(settings.ItemShop); err != nil {
		return err
	}
	if !multiplayer.ValidOperatorGameSpeed(settings.TeamBattleSpeed) {
		return errors.New("组队倍速须为不限（0）、1、1.5或2倍")
	}
	operations.configMu.Lock()
	defer operations.configMu.Unlock()
	operations.runtimeSettings, operations.runtimeSettingsRevision = settings, doc.Revision
	return nil
}

func (admin *API) runtimeSettings(w http.ResponseWriter, _ *http.Request) {
	admin.operations.configMu.RLock()
	settings, revision := admin.operations.runtimeSettings, admin.operations.runtimeSettingsRevision
	admin.operations.configMu.RUnlock()
	admin.writeRuntimeSettings(w, settings, revision)
}

func (operations *Operations) validateItemShopSettings(settings []game.ItemShopSetting) error {
	if len(settings) > 500 {
		return errors.New("道具商店最多500项商品")
	}
	allowed := make(map[int]bool, len(operations.itemShopBases))
	for _, lineup := range operations.itemShopBases {
		allowed[lineup.LineupID] = true
	}
	seen := map[int]bool{}
	for _, setting := range settings {
		if seen[setting.LineupID] || setting.Price < 1 || setting.Price > 10000000 {
			return errors.New("商品不存在、重复或价格不在1–10000000之间")
		}
		seen[setting.LineupID] = true
		if setting.Quantity < 0 || setting.Quantity > 1000000 || setting.BuyNumMax < 0 || setting.BuyNumMax > 9999 || setting.TotalLimit < 0 || setting.TotalLimit > 10000000 || setting.PeriodLimit < 0 || setting.PeriodLimit > 10000000 {
			return errors.New("商品数量、单次上限或限购数量无效")
		}
		if setting.PayType != 0 && setting.PayType != 1 && setting.PayType != 3 {
			return errors.New("商品币种仅支持金币或水晶")
		}
		if !slices.Contains([]string{"", "day", "week", "month"}, setting.Period) || (setting.PeriodLimit > 0 && setting.Period == "") {
			return errors.New("限购周期须为每日、每周或每月")
		}
		if allowed[setting.LineupID] {
			if setting.ItemID != 0 || setting.BuyType != 0 || setting.TabType != 0 || setting.Name != "" {
				return errors.New("内置商品身份固定；请新增商品")
			}
			continue
		}
		if setting.LineupID < game.OperatorItemShopFirstID || setting.LineupID >= 90000000 || setting.Quantity < 1 || setting.BuyNumMax < 1 || setting.PayType == 0 || strings.TrimSpace(setting.Name) == "" || len([]rune(setting.Name)) > 60 {
			return errors.New("新商品须有有效编号、名称、数量、币种与单次上限")
		}
		product, exists := operations.itemShopProducts[[2]int{setting.BuyType, setting.ItemID}]
		if !exists || setting.Quantity > product.MaxOwned {
			return errors.New("商品内容不存在或每份数量超过持有上限")
		}
		switch setting.BuyType {
		case 1:
			if setting.TabType != 0 && setting.TabType != 1 && setting.TabType != 3 {
				return errors.New("道具商品应放在道具、钥匙或礼包页")
			}
		case 2:
			if setting.TabType != 2 || setting.Quantity != 1 || setting.BuyNumMax != 1 {
				return errors.New("表情须放在表情页，每份和单次数量均为1")
			}
		case 3, 4:
			if setting.TabType != 4 || setting.ItemID != 0 {
				return errors.New("扩容商品须放在扩容页")
			}
		default:
			return errors.New("不支持的商品类型")
		}
	}
	return nil
}

func (admin *API) writeRuntimeSettings(w http.ResponseWriter, settings game.RuntimeSettings, revision int) {
	overrides := make(map[int]game.ItemShopSetting, len(settings.ItemShop))
	for _, setting := range settings.ItemShop {
		overrides[setting.LineupID] = setting
	}
	catalog := append([]gamestate.ItemShopLineup(nil), admin.operations.itemShopBases...)
	for _, setting := range settings.ItemShop {
		if setting.LineupID >= game.OperatorItemShopFirstID {
			catalog = append(catalog, game.ConfiguredItemShopProduct(gamestate.ItemShopLineup{}, setting, admin.operations.itemShopProducts[[2]int{setting.BuyType, setting.ItemID}]))
		}
	}
	slices.SortFunc(catalog, func(a, b gamestate.ItemShopLineup) int { return a.LineupID - b.LineupID })
	settings.ItemShop = make([]game.ItemShopSetting, 0, len(catalog))
	for _, lineup := range catalog {
		setting, exists := overrides[lineup.LineupID]
		if !exists {
			setting = game.ItemShopSetting{LineupID: lineup.LineupID, Enabled: !lineup.Disabled, Price: lineup.Price}
		}
		settings.ItemShop = append(settings.ItemShop, setting)
	}
	products := []map[string]any{}
	for key, item := range admin.operations.itemShopProducts {
		products = append(products, map[string]any{"buy_type": key[0], "item_id": key[1], "name": item.Name, "item_type": item.ItemType, "max_owned": item.MaxOwned})
	}
	slices.SortFunc(products, func(a, b map[string]any) int {
		if a["buy_type"] != b["buy_type"] {
			return a["buy_type"].(int) - b["buy_type"].(int)
		}
		return a["item_id"].(int) - b["item_id"].(int)
	})
	WriteAdminJSON(w, 200, map[string]any{"state": "PASS", "settings": settings, "revision": revision, "item_shop_catalog": catalog, "item_shop_products": products})
}

// validGachaCoverBase accepts "server" (local folder) or "storage", which requires an absolute http(s)
// prefix. A prefix kept while serving locally is validated the same way so switching back is safe. The
// stored prefix always ends with "/" so it joins the "gacha-covers/<file>" and "home-banners/<file>"
// suffixes directly.
func validGachaCoverBase(source, base string) (string, error) {
	if source != "" && source != "server" && source != "storage" {
		return "", errors.New("未知的封面来源")
	}
	if base == "" {
		if source == "storage" {
			return "", errors.New("使用存储容器时请填写存储容器地址")
		}
		return "", nil
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || len(base) > 512 {
		return "", errors.New("存储容器地址须为 http(s):// 开头的完整地址，不含账号、查询参数")
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base, nil
}

func (admin *API) setRuntimeSettings(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Enabled                  *bool                  `json:"crystal_purchase_enabled"`
		Expected                 *int                   `json:"expected_revision"`
		ItemShop                 []game.ItemShopSetting `json:"item_shop"`
		TeamBattleSpeed          *int                   `json:"team_battle_speed"`
		CreationLimit            json.RawMessage        `json:"account_creation_daily_limit"`
		CreationVerificationBits json.RawMessage        `json:"account_creation_verification_bits"`
		CoverSource              *string                `json:"gacha_cover_source"`
		CoverBaseURL             *string                `json:"gacha_cover_base_url"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Enabled == nil || body.Expected == nil {
		WriteAdminError(w, 400, "缺少开关或配置版本，请重新加载")
		return
	}
	if err := admin.validateItemShopRequest(body.ItemShop); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	if body.TeamBattleSpeed != nil && !multiplayer.ValidOperatorGameSpeed(*body.TeamBattleSpeed) {
		WriteAdminError(w, 400, "组队倍速须为不限（0）、1、1.5或2倍")
		return
	}
	coverBase := ""
	if body.CoverBaseURL != nil {
		coverBase = strings.TrimSpace(*body.CoverBaseURL)
	}
	if body.CoverSource != nil {
		var err error
		if coverBase, err = validGachaCoverBase(*body.CoverSource, coverBase); err != nil {
			WriteAdminError(w, 400, err.Error())
			return
		}
	}
	admin.saveRuntimeSettings(w, *body.Expected, "runtime-settings", func(settings *game.RuntimeSettings) error {
		limit, err := game.ParseAccountCreationDailyLimit(body.CreationLimit, settings.AccountCreationDailyLimit)
		if err != nil {
			return err
		}
		settings.AccountCreationDailyLimit = limit
		bits, err := game.ParseAccountCreationVerificationBits(body.CreationVerificationBits, settings.AccountCreationVerificationBits)
		if err != nil {
			return err
		}
		settings.AccountCreationVerificationBits = bits
		// The settings page no longer sends the shop; a request without it keeps the saved shop.
		if body.ItemShop != nil {
			rows, err := admin.operations.mergeItemShop(body.ItemShop)
			if err != nil {
				return err
			}
			settings.ItemShop = rows
		}
		settings.CrystalPurchaseEnabled = *body.Enabled
		if body.TeamBattleSpeed != nil {
			settings.TeamBattleSpeed = *body.TeamBattleSpeed
		}
		if body.CoverSource != nil {
			settings.GachaCoverSource, settings.GachaCoverBaseURL = *body.CoverSource, coverBase
		}
		return nil
	})
}

// setItemShop saves only the item shop. It shares the runtime settings document and revision with
// 运营设置 but is audited separately, so shop edits are found under their own operation.
func (admin *API) setItemShop(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Expected *int                   `json:"expected_revision"`
		ItemShop []game.ItemShopSetting `json:"item_shop"`
	}
	if err := decodeAdminJSON(r, &body); err != nil || body.Expected == nil || body.ItemShop == nil {
		WriteAdminError(w, 400, "缺少商品列表或配置版本，请重新加载")
		return
	}
	if err := admin.validateItemShopRequest(body.ItemShop); err != nil {
		WriteAdminError(w, 400, err.Error())
		return
	}
	admin.saveRuntimeSettings(w, *body.Expected, "item-shop", func(settings *game.RuntimeSettings) error {
		rows, err := admin.operations.mergeItemShop(body.ItemShop)
		settings.ItemShop = rows
		return err
	})
}

func (admin *API) validateItemShopRequest(rows []game.ItemShopSetting) error {
	if err := admin.operations.validateItemShopSettings(rows); err != nil {
		return err
	}
	for _, row := range rows {
		kind := map[int]int{1: 8, 2: 16}[row.BuyType]
		if entry, exists := admin.catalogByKey[adminCatalogKey(kind, row.ItemID)]; row.Enabled && exists && entry.ResourceState == "unavailable" {
			return errors.New("商品资源未就绪，不能上架")
		}
	}
	return nil
}

// mergeItemShop keeps operator products that an older admin page omitted: disabling is explicit and
// purchase histories are never reset by omission. The caller holds configMu.
func (operations *Operations) mergeItemShop(rows []game.ItemShopSetting) ([]game.ItemShopSetting, error) {
	byID := map[int]game.ItemShopSetting{}
	for _, row := range rows {
		byID[row.LineupID] = row
	}
	for _, old := range operations.runtimeSettings.ItemShop {
		if old.LineupID < game.OperatorItemShopFirstID {
			continue
		}
		if next, exists := byID[old.LineupID]; exists {
			if next.ItemID != old.ItemID || next.BuyType != old.BuyType {
				return nil, errors.New("已有商品不能替换内容身份；请下架后新增")
			}
		} else {
			rows = append(rows, old)
		}
	}
	return rows, operations.validateItemShopSettings(rows)
}

// saveRuntimeSettings applies change to a copy of the current settings and writes the document under
// the given audit operation; a failed change or a stale revision leaves the settings untouched.
func (admin *API) saveRuntimeSettings(w http.ResponseWriter, expected int, operation string, change func(*game.RuntimeSettings) error) {
	admin.operations.configMu.Lock()
	if expected != admin.operations.runtimeSettingsRevision {
		admin.operations.configMu.Unlock()
		WriteAdminError(w, 409, "设置已被更新，请刷新后重试")
		return
	}
	settings := admin.operations.runtimeSettings
	if err := change(&settings); err != nil {
		admin.operations.configMu.Unlock()
		WriteAdminError(w, 400, err.Error())
		return
	}
	doc, err := admin.operations.storage.WriteDocument(runtimeSettingsKey, expected, settings, operation)
	if err == nil {
		admin.operations.runtimeSettings, admin.operations.runtimeSettingsRevision = settings, doc.Revision
		admin.operations.publishRuntimeLocked()
	}
	admin.operations.configMu.Unlock()
	if errors.Is(err, accountstore.ErrDocumentConflict) {
		WriteAdminError(w, 409, "设置已被更新，请刷新后重试")
		return
	}
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	admin.writeRuntimeSettings(w, settings, doc.Revision)
}
