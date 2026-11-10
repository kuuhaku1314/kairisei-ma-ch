package admin

import (
	"encoding/json"
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

// These fields describe persisted input formats, not runtime rule choices.
// Successful conversion rewrites the entire registry to owned profiles once.
type storedCustomGachaSource struct {
	GachaID     int            `json:"gacha_id"`
	GroupID     int            `json:"group_id"`
	TemplateID  int            `json:"template_id"`
	Name        string         `json:"name"`
	Deleted     bool           `json:"deleted"`
	RuleVersion int            `json:"rule_version"`
	ArthurType  int8           `json:"arthur_type"`
	Rule        *gachaPoolRule `json:"rule"`
}

func (o *Operations) decodeStoredCustomGacha(raw json.RawMessage) (customGachaPool, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return customGachaPool{}, false, fmt.Errorf("decode operator gacha: %w", err)
	}
	if _, owned := fields["gachaid"]; owned {
		var pool customGachaPool
		if err := json.Unmarshal(raw, &pool); err != nil {
			return pool, false, fmt.Errorf("decode owned operator gacha: %w", err)
		}
		return pool, false, nil
	}
	var source storedCustomGachaSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return customGachaPool{}, false, fmt.Errorf("decode stored operator gacha: %w", err)
	}
	var profile gamestate.GachaProfile
	var kind string
	if source.Rule != nil {
		if err := source.Rule.validate(); err != nil {
			return customGachaPool{}, false, fmt.Errorf("operator gacha %d: %w", source.GachaID, err)
		}
		profile = source.Rule.profile()
		profile.GachaID, profile.GroupID, profile.Name, profile.ArthurType = source.GachaID, source.GroupID, source.Name, source.ArthurType
		profile.PublicationKey = "operator"
		kind = source.Rule.Type
	} else {
		template, exists := o.gachaBases[source.TemplateID]
		allowed := template.GroupID > 0 && len(template.RewardPool) == 0 && len(template.Steps) == 0
		if source.RuleVersion == 1 {
			allowed = template.GroupID > 0 && template.PublicationKey != "operator" && template.UserSelectMax == 0 && template.CardNum > 0 && template.CardNumMax <= 100 && gamestate.ValidateGachaRules(template) == nil
		}
		if !exists || !allowed || source.RuleVersion < 0 || source.RuleVersion > 1 {
			return customGachaPool{}, false, fmt.Errorf("operator gacha %d references unavailable migration template %d", source.GachaID, source.TemplateID)
		}
		profile = gamestate.CloneGachas([]gamestate.GachaProfile{template})[0]
		profile.GachaID, profile.GroupID = source.GachaID, source.GroupID
		if source.Name != "" {
			profile.Name = source.Name
		}
		if source.ArthurType != 0 {
			profile.ArthurType = source.ArthurType
		}
		profile.PublicationKey = "operator"
		profile.PlayCount = 0
		if source.RuleVersion == 0 {
			profile.DailyFirstFree, profile.Gifts = false, nil
		}
		profile = o.filterGachaProfession(profile)
		kind = ownedGachaType(profile)
	}
	return customGachaPool{GachaProfile: profile, Type: kind, Deleted: source.Deleted}, true, nil
}

func ownedGachaType(p gamestate.GachaProfile) string {
	switch {
	case len(p.Steps) > 0:
		return "step"
	case len(p.RewardPool) > 0:
		return "mixed"
	case p.UserSelectMax > 0:
		return "select"
	case p.UnownedOnly:
		return "unowned"
	case p.GuaranteedCount > 0:
		return "guaranteed"
	case p.PayType == 2:
		return "friend"
	default:
		return "standard"
	}
}
