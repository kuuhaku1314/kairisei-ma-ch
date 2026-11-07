package battlecore

import "errors"

// DeckIdentityResults emits the original remote-card identity prefix without
// room/account metadata. Source deck slots, including support holes, are kept.
func DeckIdentityResults(members []Participant) ([]BattleResult, error) {
	if len(members) != ParticipantCount {
		return nil, errors.New("battle room party is incomplete")
	}
	rows := make([]BattleResult, 0, ParticipantCount*10)
	for _, member := range members {
		if len(member.DeckCards) != 10 {
			return nil, errors.New("battle game start member deck is incomplete")
		}
		for index, card := range member.DeckCards {
			if card.CardType != index+1 || card.CardID <= 0 || card.Level <= 0 {
				return nil, errors.New("battle game start member card is invalid")
			}
			rows = append(rows, CardIdentityResult(member.MemberType, card))
		}
		if err := ValidateSupportCards(member.SupportCards); err != nil {
			return nil, err
		}
		for _, card := range member.SupportCards {
			rows = append(rows, CardIdentityResult(member.MemberType, card))
		}
	}
	return rows, nil
}
