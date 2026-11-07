package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	operationservice "kairisei.local/server/internal/operations"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/protocol"
)

const (
	teamBattlePublicationKey = operationservice.TeamBattlePublicationKey
	PastBattlePublicationKey = operationservice.PastBattlePublicationKey
	towerPublicationKey      = operationservice.TowerPublicationKey
	gachaPublicationKey      = operationservice.GachaPublicationKey
)

type TeamBattlePublication = operationservice.TeamBattlePublication

type gachaPublication struct {
	CatalogVersion   int   `json:"catalog_version,omitempty"`
	ExpectedRevision *int  `json:"expected_revision,omitempty"`
	GroupIDs         []int `json:"group_ids"`
}

func (operations *Operations) SetTeamBattlePublication(publication TeamBattlePublication) (accountstore.Document, error) {
	return operations.setBattlePublication(teamBattlePublicationKey, publication)
}

// The tower catalog is written like the Boss catalogs, then applied to live
// tower admission. Its IDs were checked against the tower catalog by the caller.
func (operations *Operations) SetTowerPublication(publication TeamBattlePublication) (accountstore.Document, error) {
	operations.configMu.Lock()
	defer operations.configMu.Unlock()
	doc, err := operations.setBattlePublication(towerPublicationKey, publication)
	if err != nil {
		return doc, err
	}
	operations.towerAdmission = operationservice.TowerAdmissionFromPublication(publication)
	operations.publishRuntimeLocked()
	return doc, nil
}

func (operations *Operations) setBattlePublication(key string, publication TeamBattlePublication) (accountstore.Document, error) {
	if publication.ExpectedRevision == nil {
		return accountstore.Document{}, errors.New("缺少配置版本，请重新载入后发布")
	}
	if publication.StartUnix < 0 || publication.EndUnix < 0 || (publication.EndUnix != 0 && publication.EndUnix <= publication.StartUnix) {
		return accountstore.Document{}, errors.New("结束时间须晚于开始时间")
	}
	switch publication.Mode {
	case "all":
		publication.GroupIDs = nil
	case "allowlist":
		sort.Ints(publication.GroupIDs)
		unique := publication.GroupIDs[:0]
		for _, groupID := range publication.GroupIDs {
			if groupID <= 0 {
				return accountstore.Document{}, errors.New("CN team battle publication contains an invalid group ID")
			}
			if len(unique) == 0 || unique[len(unique)-1] != groupID {
				unique = append(unique, groupID)
			}
		}
		publication.GroupIDs = unique
	default:
		return accountstore.Document{}, errors.New("CN team battle publication mode is invalid")
	}
	expected := *publication.ExpectedRevision
	publication.ExpectedRevision = nil
	return operations.writeDocument(key, expected, publication)
}

// Operations owns Admin editing catalogs and publication transactions. Player
// request paths consume the embedded immutable Runtime, never configMu.
type Operations struct {
	popupAnnouncements         popupAnnouncementRegistry
	popupAnnouncementsRevision int
	missionBase                map[int]gamestate.MissionDefinition
	missionConfig              game.MissionConfiguration
	*operationservice.Runtime
	evolutionEdges          map[game.EvolutionPath]int
	evolutionClosed         []game.EvolutionPath
	evolutionRevision       int
	evolutionPolicy         *game.EvolutionRestrictions
	playerPolicy            atomic.Pointer[playerPolicySnapshot]
	playerDefaults          *PlayerPolicy
	playerLoginBase         gamestate.LoginBonusPolicy
	playerNaviNames         map[int8]string
	content                 *contentStore
	towerAdmission          game.TowerAdmission
	runtimeSettings         game.RuntimeSettings
	runtimeSettingsRevision int
	ipBlacklist             operationservice.IPBlacklist
	ipBlacklistRevision     int
	loginWhitelist          accountstore.LoginWhitelist
	loginWhitelistRevision  int
	disabledModules         protocol.ModuleSwitch
	moduleSwitchesRevision  int
	itemShopBases           []gamestate.ItemShopLineup
	itemShopProducts        map[[2]int]gamestate.ItemDefinition
	storage                 *accountstore.Database
	configMu                sync.RWMutex
	gachaBases              map[int]gamestate.GachaProfile
	gachaCardJobs           map[int]int8
	gachaConfigurations     []game.GachaConfiguration
	gachaRevision           uint64
	managedGachaGroups      map[int]struct{}
	managedGachaGroupByID   map[int]int
	defaultGachaPublication map[int]struct{}
	customGachas            map[int]customGachaPool
	customGachaRevision     int
	customGachaNextID       int
}

func NewOperations(storage *accountstore.Database, gachas []gamestate.GachaProfile) (*Operations, error) {
	if storage == nil {
		return nil, errors.New("CN operation database is required")
	}
	if err := storage.EnsureSchema(); err != nil {
		return nil, err
	}
	managedGroups := make(map[int]struct{})
	groupByID := make(map[int]int)
	defaults := make(map[int]struct{})
	publicationKeys := make(map[int]string)
	for _, gacha := range gachas {
		if gacha.GachaID == 90000100 || gacha.GachaID == 90000200 || gacha.GroupID <= 0 {
			continue
		}
		if previous, exists := publicationKeys[gacha.GroupID]; exists && previous != gacha.PublicationKey {
			return nil, fmt.Errorf("CN managed gacha group %d mixes publication keys", gacha.GroupID)
		}
		publicationKeys[gacha.GroupID] = gacha.PublicationKey
		managedGroups[gacha.GroupID] = struct{}{}
		groupByID[gacha.GachaID] = gacha.GroupID
		if gacha.PublicationKey == "water_coin" || gacha.PublicationKey == "" {
			defaults[gacha.GroupID] = struct{}{}
		}
	}
	bases := make(map[int]gamestate.GachaProfile)
	for _, profile := range gachas {
		if profile.GachaID != 90000200 && profile.GachaID != 90000100 {
			bases[profile.GachaID] = profile
		}
	}
	runtime, err := operationservice.NewRuntime(storage)
	if err != nil {
		return nil, err
	}
	operations := &Operations{
		Runtime: runtime, storage: storage, managedGachaGroups: managedGroups,
		managedGachaGroupByID: groupByID, defaultGachaPublication: defaults, gachaBases: bases,
	}
	catalog, err := storage.CatalogState()
	if err != nil {
		return nil, err
	}
	operations.gachaCardJobs = map[int]int8{}
	operations.evolutionEdges = map[game.EvolutionPath]int{}
	for _, edge := range catalog.CardActions.EvolutionTransitions {
		operations.evolutionEdges[game.EvolutionPath{FromCardID: edge.FromCardID, ToCardID: edge.ToCardID}] = edge.Type
	}
	for id, card := range catalog.DeckRankPolicy.Cards {
		operations.gachaCardJobs[id] = card.ArthurType
	}
	for _, tab := range catalog.ItemShopTabs {
		for _, lineup := range tab.Lineup {
			if !lineup.Hidden {
				operations.itemShopBases = append(operations.itemShopBases, lineup)
			}
		}
	}
	operations.itemShopProducts = map[[2]int]gamestate.ItemDefinition{}
	for _, item := range catalog.ItemDefinitions {
		operations.itemShopProducts[[2]int{1, item.ItemID}] = item
	}
	for _, reward := range catalog.CollectionRewards {
		if reward.Type == 16 {
			operations.itemShopProducts[[2]int{2, reward.ID}] = gamestate.ItemDefinition{ItemID: reward.ID, Name: reward.Name, MaxOwned: 1}
		}
	}
	operations.itemShopProducts[[2]int{3, 0}] = gamestate.ItemDefinition{Name: "卡牌仓库扩容", MaxOwned: gamestate.CardCapacityLimit}
	operations.itemShopProducts[[2]int{4, 0}] = gamestate.ItemDefinition{Name: "卡牌持有上限扩容", MaxOwned: gamestate.CardCapacityLimit}
	if err := operations.loadCustomGachas(); err != nil {
		return nil, err
	}
	if err := operations.reloadGachaConfigurations(); err != nil {
		return nil, err
	}
	if err := operations.loadRuntimeSettings(); err != nil {
		return nil, err
	}
	if err := operations.loadIPBlacklist(); err != nil {
		return nil, err
	}
	if err := operations.loadLoginWhitelist(); err != nil {
		return nil, err
	}
	if err := operations.loadModuleSwitches(); err != nil {
		return nil, err
	}
	if err := operations.loadPopupAnnouncements(); err != nil {
		return nil, err
	}
	if err := operations.loadEvolutionRestrictions(); err != nil {
		return nil, err
	}
	if err := operations.loadMissions(catalog.MissionDefinitions); err != nil {
		return nil, err
	}
	operations.towerAdmission, err = operations.Runtime.TowerAdmission()
	if err != nil {
		return nil, err
	}
	operations.publishRuntimeLocked()
	return operations, nil
}

func (operations *Operations) setGachaPublication(publication gachaPublication) (accountstore.Document, error) {
	if publication.ExpectedRevision == nil {
		return accountstore.Document{}, errors.New("缺少配置版本，请重新载入后发布")
	}
	sort.Ints(publication.GroupIDs)
	unique := publication.GroupIDs[:0]
	operations.configMu.RLock()
	defer operations.configMu.RUnlock()
	for _, groupID := range publication.GroupIDs {
		if _, exists := operations.managedGachaGroups[groupID]; !exists {
			return accountstore.Document{}, fmt.Errorf("unknown managed CN gacha group %d", groupID)
		}
		if len(unique) == 0 || unique[len(unique)-1] != groupID {
			unique = append(unique, groupID)
		}
	}
	publication.GroupIDs = unique
	// A deleted operator pool stays hidden by its configuration. Keeping it in an existing publication lets a
	// restore bring it back as before; only newly opening a deleted pool is refused.
	current, err := operations.storage.ReadDocument(gachaPublicationKey)
	if err != nil {
		return accountstore.Document{}, err
	}
	var saved gachaPublication
	if current.Revision > 0 {
		if err := json.Unmarshal(current.Payload, &saved); err != nil {
			return accountstore.Document{}, fmt.Errorf("decode CN gacha publication: %w", err)
		}
	}
	for _, groupID := range unique {
		if pool, custom := operations.customGachaByGroup(groupID); custom && pool.Deleted && !slices.Contains(saved.GroupIDs, groupID) {
			return accountstore.Document{}, fmt.Errorf("卡池 %d 已删除，恢复后才能开启", pool.GachaID)
		}
	}
	expected := *publication.ExpectedRevision
	publication.ExpectedRevision = nil
	publication.CatalogVersion = 2
	return operations.writeDocument(gachaPublicationKey, expected, publication)
}

func (operations *Operations) writeDocument(key string, expected int, value any) (accountstore.Document, error) {
	operation := strings.Split(key, ":")[0]
	if key == teamBattlePublicationKey || key == PastBattlePublicationKey || key == towerPublicationKey {
		operation = "boss-policy"
	} else if key == gachaPublicationKey {
		operation = "gacha-policy"
	}
	return operations.storage.WriteDocument(key, expected, value, operation)
}

func (operations *Operations) customGachaByGroup(groupID int) (customGachaPool, bool) {
	for _, pool := range operations.customGachas {
		if pool.GroupID == groupID {
			return pool, true
		}
	}
	return customGachaPool{}, false
}
