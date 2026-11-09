package admin

import (
	"slices"
	"strings"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

// Region comes from the import receipt, never from names or card ID ranges.
// It is a catalog shortcut, not evidence of a crystal-gacha acquisition path.
func adminJPCardIDs(source masterdata.CardMasterIdentity) map[int]bool {
	ids := make(map[int]bool)
	for _, imported := range source.Imports {
		if imported.Region == "JP" {
			for _, id := range imported.IDs.Cards {
				ids[id] = true
			}
		}
	}
	return ids
}

// Operator pools may offer any existing card (user decision 2026-09-27, LOCAL_POLICY); the
// unowned-six-star pool keeps its six-star identity because its draw excludes owned card families.
func adminGachaCardEligible(base gamestate.GachaProfile, entry AdminCatalogEntry) bool {
	if base.ArthurType >= 1 && base.ArthurType <= 4 && entry.ArthurType != 0 && entry.ArthurType != base.ArthurType {
		return false
	}
	return !base.UnownedOnly || entry.Rarity == 6
}

func cardCrystalGachaSource(text string) bool {
	return strings.Contains(text, "扭蛋") && !strings.Contains(strings.ToLower(text), "boss币")
}

// Acquisition channels come from the source text, normal/god evolution from
// incoming recipes. Point stars and factor breakthrough belong to rarity filters.
func cardSourceTags(text string, evolutionTypes []int) []string {
	tags := []string{}
	if cardCrystalGachaSource(text) {
		tags = append(tags, "gacha")
	}
	if strings.Contains(text, "副本") {
		tags = append(tags, "dungeon")
	}
	if strings.Contains(strings.ToLower(text), "boss币") {
		tags = append(tags, "boss_coin")
	}
	for _, kind := range evolutionTypes {
		var tag string
		switch kind { // Original EVOLUTION_TYPE: NORMAL/GOD.
		case 0, 1:
			tag = "evolution"
		}
		if tag != "" && !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if strings.Contains(text, "特典") || strings.Contains(text, "活动") || strings.Contains(text, "任务") || strings.Contains(text, "礼包") || text == "PVP" {
		tags = append(tags, "event")
	}
	if len(tags) == 0 {
		tags = append(tags, "other")
	}
	return tags
}
