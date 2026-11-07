package multiplayer

// PartnerCardKinds uses the same normal-skill column as CardCsvData.GetSkillKind.
// The returned values are SKILL_KIND indexes, including NULL=0 and SPECIAL=7.
func (h *Hub) PartnerCardKinds(cardIDs []int) map[int]int {
	result := make(map[int]int, len(cardIDs))
	if h == nil {
		return result
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.combat == nil {
		return result
	}
	kinds := map[string]int{"ATTACK": 1, "SORCERY": 2, "RECOVERY": 3, "SUPPORT": 4, "DEFENSE": 5, "JAMMING": 6, "SPECIAL": 7}
	for _, id := range cardIDs {
		card, ok := h.combat.Cards[id]
		if !ok {
			continue
		}
		if skills := h.combat.Skills[card.NormalSkillID]; len(skills) > 0 {
			result[id] = kinds[skills[0].Kind]
		}
	}
	return result
}
