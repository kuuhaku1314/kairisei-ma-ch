package admin

import (
	"kairisei.local/server/internal/operations"
)

// Called while configMu is held, or before the editor is exposed at startup.
// Content drafts and failed writes never call this publication boundary.
// Registering a new pool updates group metadata, without publishing its draft.
func (o *Operations) publishRuntimeLocked() {
	settings := o.runtimeSettings
	settings.Revision = o.runtimeSettingsRevision + 1
	groups := make(map[int]int, len(o.managedGachaGroupByID))
	for id, group := range o.managedGachaGroupByID {
		if _, custom := o.customGachas[id]; !custom {
			groups[id] = group
		}
	}
	for _, config := range o.gachaConfigurations {
		groups[config.Profile.GachaID] = config.Profile.GroupID
	}
	publication := operations.Publication{
		IPBlacklist:     o.ipBlacklist,
		LoginWhitelist:  o.loginWhitelist,
		DisabledModules: o.disabledModules,
		Missions:        o.missionConfig, Evolution: o.evolutionPolicy, Settings: settings, Gachas: o.gachaConfigurations, GachaRevision: o.gachaRevision,
		TowerAdmission: o.towerAdmission,
		GachaGroups:    groups, ManagedGachaGroups: o.managedGachaGroups,
		DefaultGachaGroups: o.defaultGachaPublication, PermanentGachaGroups: map[int]struct{}{},
	}
	if p := o.playerPolicy.Load(); p != nil {
		publication.Player = p.Runtime
		publication.HomeBanners = p.Value.HomeBanners
	}
	if o.content != nil {
		publication.Content = o.content.configuration
	}
	for _, base := range o.gachaBases {
		if base.PublicationKey == "" && base.GroupID > 0 {
			publication.PermanentGachaGroups[base.GroupID] = struct{}{}
		}
	}
	o.Runtime.Publish(publication)
}
