package operations

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
)

const (
	TeamBattlePublicationKey = "team_battle_publication"
	PastBattlePublicationKey = "past_battle_publication"
	TowerPublicationKey      = "tower_publication"
	GachaPublicationKey      = "gacha_publication"
)

type gachaPublication struct {
	CatalogVersion int   `json:"catalog_version,omitempty"`
	GroupIDs       []int `json:"group_ids"`
}

type TeamBattlePublication struct {
	StartUnix        int64  `json:"start_unix,omitempty"`
	EndUnix          int64  `json:"end_unix,omitempty"`
	ExpectedRevision *int   `json:"expected_revision,omitempty"`
	Mode             string `json:"mode"`
	GroupIDs         []int  `json:"group_ids,omitempty"`
}

// Towers are a third publication catalog with its own document: whole native
// towers by tower ID in GroupIDs, with their own mode and window. All mode
// opens every tower, including towers added by later content.
func TowerAdmissionFromPublication(publication TeamBattlePublication) game.TowerAdmission {
	var allowed map[int]struct{}
	if publication.Mode != "all" {
		allowed = make(map[int]struct{}, len(publication.GroupIDs))
		for _, id := range publication.GroupIDs {
			allowed[id] = struct{}{}
		}
	}
	return game.TowerAdmission{TowerIDs: allowed, StartUnix: publication.StartUnix, EndUnix: publication.EndUnix}
}

func (operations *Runtime) TowerAdmission() (game.TowerAdmission, error) {
	doc, err := operations.storage.ReadDocument(TowerPublicationKey)
	if err != nil || doc.Revision == 0 {
		return game.TowerAdmission{}, err
	}
	var publication TeamBattlePublication
	if err := json.Unmarshal(doc.Payload, &publication); err != nil {
		return game.TowerAdmission{}, fmt.Errorf("decode CN tower publication: %w", err)
	}
	return TowerAdmissionFromPublication(publication), nil
}

func (operations *Runtime) GachaPublication() (map[int]struct{}, error) {
	doc, err := operations.storage.ReadDocument(GachaPublicationKey)
	if err != nil {
		return nil, err
	}
	return operations.DecodeGachaPublication(doc)
}

func (operations *Runtime) DecodeGachaPublication(doc accountstore.Document) (map[int]struct{}, error) {
	published := operations.current.Load()
	if doc.Revision == 0 {
		return maps.Clone(published.DefaultGachaGroups), nil
	}
	var publication gachaPublication
	if err := json.Unmarshal(doc.Payload, &publication); err != nil {
		return nil, fmt.Errorf("decode CN gacha publication: %w", err)
	}
	active := make(map[int]struct{}, len(publication.GroupIDs))
	// Older operator documents only covered event presets. Do not silently
	// close previously permanent pools when loading such a document.
	if publication.CatalogVersion < 2 {
		for groupID := range published.PermanentGachaGroups {
			active[groupID] = struct{}{}
		}
	}
	for _, groupID := range publication.GroupIDs {
		if _, exists := published.ManagedGachaGroups[groupID]; !exists {
			return nil, fmt.Errorf("CN gacha publication contains unknown group %d", groupID)
		}
		active[groupID] = struct{}{}
	}
	return active, nil
}

func (operations *Runtime) GachaIDPublished(gachaID int, active map[int]struct{}) bool {
	published := operations.current.Load()
	for _, config := range published.Gachas {
		if config.Profile.GachaID == gachaID {
			now := time.Now().Unix()
			if config.Disabled || now < config.StartUnix || (config.EndUnix != 0 && now >= config.EndUnix) {
				return false
			}
		}
	}
	groupID, managed := published.GachaGroups[gachaID]
	if !managed {
		return gachaID == 90000100 || gachaID == 90000200
	}
	_, enabled := active[groupID]
	return enabled
}

// teamBattleGroupAllowlist returns nil for the default all-published policy.
// It is read at the HTTP presentation boundary so an operations change does
// not rebuild immutable master data and does not require a server restart.

func (operations *Runtime) TeamBattleGroupAllowlist() (map[int]struct{}, error) {
	return operations.BattleGroupAllowlist(TeamBattlePublicationKey)
}

func (operations *Runtime) BattleGroupAllowlist(key string) (map[int]struct{}, error) {
	doc, err := operations.storage.ReadDocument(key)
	if err != nil {
		return nil, err
	}
	if doc.Revision == 0 {
		return nil, nil
	}
	content := doc.Payload
	var publication TeamBattlePublication
	if err := json.Unmarshal(content, &publication); err != nil {
		return nil, fmt.Errorf("decode CN team battle publication: %w", err)
	}
	now := time.Now().Unix()
	if now < publication.StartUnix || (publication.EndUnix != 0 && now >= publication.EndUnix) {
		return map[int]struct{}{}, nil
	}
	switch publication.Mode {
	case "all":
		return nil, nil
	case "allowlist":
		allowed := make(map[int]struct{}, len(publication.GroupIDs))
		for _, groupID := range publication.GroupIDs {
			if groupID <= 0 {
				return nil, errors.New("CN team battle publication contains an invalid group ID")
			}
			allowed[groupID] = struct{}{}
		}
		return allowed, nil
	default:
		return nil, errors.New("CN team battle publication mode is invalid")
	}
}
