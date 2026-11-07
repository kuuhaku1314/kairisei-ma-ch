// Package operations owns published player-facing policy. It does not depend
// on Admin HTTP, edit drafts, request DTOs or mutable player accounts.
package operations

import (
	"maps"
	"slices"
	"sync/atomic"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/protocol"
)

// Publication combines immutable, independently versioned policies. Publishing
// one atomic pointer prevents a request observing half a group update.
type Publication struct {
	PopupAnnouncements   []game.PopupAnnouncement
	IPBlacklist          IPBlacklist
	LoginWhitelist       accountstore.LoginWhitelist
	DisabledModules      protocol.ModuleSwitch
	HomeBanners          HomeBannerPolicy
	Missions             game.MissionConfiguration
	Evolution            *game.EvolutionRestrictions
	Player               game.PlayerConfiguration
	Content              game.ContentConfiguration
	TowerAdmission       game.TowerAdmission
	Settings             game.RuntimeSettings
	GachaRevision        uint64
	Gachas               []game.GachaConfiguration
	GachaGroups          map[int]int
	ManagedGachaGroups   map[int]struct{}
	DefaultGachaGroups   map[int]struct{}
	PermanentGachaGroups map[int]struct{}
}

type Runtime struct {
	maintenance maintenanceGate
	storage     *accountstore.Database
	current     atomic.Pointer[Publication]
}

func NewRuntime(storage *accountstore.Database) (*Runtime, error) {
	runtime := &Runtime{storage: storage}
	runtime.current.Store(&Publication{})
	if err := runtime.loadMaintenance(); err != nil {
		return nil, err
	}
	return runtime, nil
}

// Editors call Publish only after a successful database commit. Definitions
// inside each policy remain immutable; copy containers the editor may replace.
func (runtime *Runtime) Publish(publication Publication) {
	publication.PopupAnnouncements = slices.Clone(publication.PopupAnnouncements)
	publication.HomeBanners.Items = slices.Clone(publication.HomeBanners.Items)
	publication.Gachas = slices.Clone(publication.Gachas)
	publication.Settings.ItemShop = slices.Clone(publication.Settings.ItemShop)
	publication.TowerAdmission = publication.TowerAdmission.Clone()
	publication.GachaGroups = maps.Clone(publication.GachaGroups)
	publication.ManagedGachaGroups = maps.Clone(publication.ManagedGachaGroups)
	publication.DefaultGachaGroups = maps.Clone(publication.DefaultGachaGroups)
	publication.PermanentGachaGroups = maps.Clone(publication.PermanentGachaGroups)
	runtime.current.Store(&publication)
}

func (runtime *Runtime) MissionCatalog() *game.MissionCatalog {
	return runtime.current.Load().Missions.Catalog
}

func (runtime *Runtime) TeamBattleSpeed() int { return runtime.current.Load().Settings.TeamBattleSpeed }

func (runtime *Runtime) ModuleSwitches() protocol.ModuleSwitch {
	return protocol.DefaultModuleSwitches() &^ runtime.current.Load().DisabledModules
}

func (runtime *Runtime) AccountCreationPolicy() accountstore.CreationPolicy {
	published := runtime.current.Load()
	return accountstore.CreationPolicy{
		DailyLimit: published.Settings.AccountCreationDailyLimit, VerificationBits: published.Settings.AccountCreationVerificationBits,
		LoginWhitelist: published.LoginWhitelist,
	}
}
func (runtime *Runtime) ContentConfiguration() game.ContentConfiguration {
	return runtime.current.Load().Content
}

// Targets expose the existing narrow domain configuration capabilities. No
// HTTP handler, socket or database work is needed to apply a publication.
func (runtime *Runtime) PrepareBusiness(target any) {
	published := runtime.current.Load()
	if c, ok := target.(game.PopupAnnouncementConfigurator); ok {
		c.ApplyPopupAnnouncements(published.PopupAnnouncements)
	}
	if c, ok := target.(HomeBannerConfigurator); ok {
		config := HomeBannerConfiguration{Policy: published.HomeBanners}
		if published.Settings.GachaCoverSource == "storage" {
			config.ImageBaseURL = published.Settings.GachaCoverBaseURL
		}
		c.ApplyHomeBannerConfiguration(config)
	}
	if c, ok := target.(game.MissionConfigurator); ok {
		c.ApplyMissionConfiguration(published.Missions)
	}
	if c, ok := target.(game.EvolutionConfigurator); ok {
		c.ApplyEvolutionRestrictions(published.Evolution)
	}
	if c, ok := target.(game.PlayerConfigurator); ok && published.Player.Revision != 0 {
		c.ApplyPlayerConfiguration(published.Player)
	}
	if c, ok := target.(game.GachaConfigurator); ok {
		c.ApplyGachaConfiguration(published.GachaRevision, published.Gachas)
	}
	if c, ok := target.(game.ContentConfigurator); ok {
		c.ApplyContentConfiguration(published.Content)
	}
	if c, ok := target.(game.TowerAdmissionConfigurator); ok {
		c.ApplyTowerAdmission(published.TowerAdmission)
	}
	if c, ok := target.(game.RuntimeConfigurator); ok {
		c.ApplyRuntimeSettings(published.Settings)
	}
}

func (runtime *Runtime) ManagedGroups() map[int]struct{} {
	return maps.Clone(runtime.current.Load().ManagedGachaGroups)
}
