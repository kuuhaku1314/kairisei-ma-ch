package clientbridge

import (
	"fmt"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/gamestate"
)

type SummonParameterResult struct {
	SummonID     int   `json:"summonsid"`
	HP           int32 `json:"hp"`
	Attack       int32 `json:"atkp"`
	Magic        int32 `json:"intp"`
	Mind         int32 `json:"mndp"`
	Defense      int32 `json:"defp"`
	MagicDefense int32 `json:"mdefp"`
}

type CardParameterFamilies struct {
	Alchemist []CardParameterResult
	Summons   []SummonParameterResult
}

// CardParameterCatalog is a read-only projection over the same immutable
// definitions used by battles. It owns no account state or editable masters.
type CardParameterCatalog struct {
	definitions *battlemodel.Definitions
	normal      CardBonus
	premium     CardBonus
}

func NewCardParameterCatalog(definitions *battlemodel.Definitions, policy gamestate.CardProgressionPolicy) *CardParameterCatalog {
	if definitions == nil {
		return nil
	}
	bonus := func(p gamestate.CardParameter) CardBonus {
		return CardBonus{HP: p.HP, Attack: p.Attack, Magic: p.Magic, Mind: p.Mind}
	}
	return &CardParameterCatalog{definitions: definitions, normal: bonus(policy.FameNormal), premium: bonus(policy.FamePremium)}
}

func (catalog *CardParameterCatalog) Resolve(cardID, level, love, fame int) (CardParameterFamilies, error) {
	var result CardParameterFamilies
	if catalog == nil || cardID == 0 {
		return result, nil
	}
	card, exists := catalog.definitions.Cards[cardID]
	if !exists {
		return result, fmt.Errorf("card parameter definition %d is unavailable", cardID)
	}
	if !card.HasParameterFamilies {
		return result, nil
	}
	initial, maximum, bonus := card.AlchemistParameters[0], card.AlchemistParameters[1], card.AlchemistParameters[2]
	fameBonus := catalog.normal
	if card.RarityPlus >= 1 && card.RarityPlus <= 3 {
		fameBonus = catalog.premium
	}
	parameters, err := CalculateCardParameters(CardParameterRequest{
		CardID: cardID, Level: level, Love: love, Fame: fame, Bonus: fameBonus,
		Base: CardBase{
			HP: initial[0], HPLimit: maximum[0], HPLove: bonus[0],
			Attack: initial[1], AtkLimit: maximum[1], AtkLove: bonus[1],
			Magic: initial[2], MagLimit: maximum[2], MagLove: bonus[2],
			Mind: initial[3], MndLimit: maximum[3], MndLove: bonus[3],
			LevelMax: card.MaxLevel, LoveMax: card.LoveMax,
		},
	})
	if err != nil {
		return result, fmt.Errorf("card %d alchemist parameters: %w", cardID, err)
	}
	result.Alchemist = []CardParameterResult{parameters}
	if card.SummonID == 0 {
		return result, nil
	}
	summon, exists := catalog.definitions.Summons[card.SummonID]
	if !exists {
		return CardParameterFamilies{}, fmt.Errorf("card %d summon definition %d is unavailable", cardID, card.SummonID)
	}
	var ranges [6]battlecore.SummonsParameterRange
	for i := range ranges {
		ranges[i] = battlecore.SummonsParameterRange{Initial: summon.Initial[i], Maximum: summon.Maximum[i]}
	}
	values := battlecore.SummonsParameters(int32(level), int32(card.MaxLevel), ranges)
	result.Summons = []SummonParameterResult{{SummonID: summon.ID, HP: values[0], Attack: values[1], Magic: values[2], Mind: values[3], Defense: values[4], MagicDefense: values[5]}}
	return result, nil
}

// AlchemistMain exposes the already derived parameter family to deck summaries.
// A card without that family keeps its ordinary owned parameters.
func (catalog *CardParameterCatalog) AlchemistMain(cardID, level, love, fame int) (gamestate.CardParameter, bool, error) {
	if catalog == nil {
		return gamestate.CardParameter{}, false, fmt.Errorf("alchemist card parameter catalog is unavailable")
	}
	families, err := catalog.Resolve(cardID, level, love, fame)
	if err != nil || len(families.Alchemist) == 0 {
		return gamestate.CardParameter{}, false, err
	}
	value := families.Alchemist[0]
	return gamestate.CardParameter{HP: value.HP, Attack: value.Attack, Magic: value.Magic, Mind: value.Mind}, true, nil
}
