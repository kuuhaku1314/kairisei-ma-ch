package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"kairisei.local/server/internal/gamestate"
	"time"
)

const DefaultAccountCreationDailyLimit = 10

// ParseAccountCreationDailyLimit distinguishes omission from an explicit zero
// or invalid null, for both persisted settings and partial Admin updates.
func ParseAccountCreationDailyLimit(raw json.RawMessage, fallback int) (int, error) {
	if len(raw) == 0 {
		return fallback, nil
	}
	var limit int
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &limit) != nil || limit < 0 || limit > 10000 {
		return 0, errors.New("同 IP 每日创建账号上限须为 0–10000 的整数，0 表示暂停创建新账号")
	}
	return limit, nil
}

func ParseAccountCreationVerificationBits(raw json.RawMessage, fallback int) (int, error) {
	if len(raw) == 0 {
		return fallback, nil
	}
	var bits int
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &bits) != nil || bits != 0 && (bits < 16 || bits > 32) {
		return 0, errors.New("新账号创建验证强度须为 0 或 16–32 的整数，0 表示关闭")
	}
	return bits, nil
}

// RuntimeSettings is shared operator policy, never part of a player snapshot.
// The zero value deliberately leaves local crystal purchases disabled.
type RuntimeSettings struct {
	Revision                        int               `json:"-"` // In-memory publication identity; zero means unversioned caller.
	CrystalPurchaseEnabled          bool              `json:"crystal_purchase_enabled"`
	TeamBattleSpeed                 int               `json:"team_battle_speed"` // 0 follows the host client; nonzero overrides new rooms.
	AccountCreationDailyLimit       int               `json:"account_creation_daily_limit"`
	AccountCreationVerificationBits int               `json:"account_creation_verification_bits"` // Zero disables proof-of-work; otherwise 16–32 leading zero bits.
	ItemShop                        []ItemShopSetting `json:"item_shop,omitempty"`
	// GachaCoverSource selects where uploaded pool covers and home banners are read from: "" or "server"
	// serves the local gacha-covers / home-banners folders, "storage" prefixes GachaCoverBaseURL (for
	// example an S3/CDN bucket) to the same relative paths.
	GachaCoverSource  string `json:"gacha_cover_source,omitempty"`
	GachaCoverBaseURL string `json:"gacha_cover_base_url,omitempty"`
}

type ItemShopSetting struct {
	LineupID    int    `json:"lineup_id"`
	Enabled     bool   `json:"enabled"`
	Price       int    `json:"price"`
	ItemID      int    `json:"item_id,omitempty"`
	BuyType     int    `json:"buy_type,omitempty"`
	Name        string `json:"name,omitempty"`
	Quantity    int    `json:"quantity,omitempty"`
	PayType     int    `json:"pay_type,omitempty"`
	TabType     int    `json:"tab_type,omitempty"`
	BuyNumMax   int    `json:"buy_num_max,omitempty"`
	TotalLimit  int    `json:"total_limit,omitempty"`
	Period      string `json:"period,omitempty"`
	PeriodLimit int    `json:"period_limit,omitempty"`
}

type RuntimeConfigurator interface {
	ApplyRuntimeSettings(RuntimeSettings)
}

func (s *Account) ApplyRuntimeSettings(settings RuntimeSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if settings.Revision != 0 && settings.Revision == s.runtimeSettingsRevision {
		return
	}
	s.runtimeSettingsRevision = settings.Revision
	s.localCrystalPurchaseEnabled = settings.CrystalPurchaseEnabled
	s.gachaCoverBaseURL = ""
	if settings.GachaCoverSource == "storage" {
		s.gachaCoverBaseURL = settings.GachaCoverBaseURL
	}
	s.itemShopSettings = make(map[int]ItemShopSetting, len(settings.ItemShop))
	for _, setting := range settings.ItemShop {
		s.itemShopSettings[setting.LineupID] = setting
	}
}

// GachaCoverBaseURL is the storage-container prefix for pool covers, or "" to serve them locally.
func (s *Account) GachaCoverBaseURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gachaCoverBaseURL
}

func (s *Account) configuredItemShopLineup(lineup gamestate.ItemShopLineup) gamestate.ItemShopLineup {
	return s.itemShopLineupAt(lineup, time.Now())
}
