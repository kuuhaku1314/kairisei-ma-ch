// Package clientbridge adapts client parameter inputs to the existing Go domain.
package clientbridge

import (
	"fmt"
	"math"

	"kairisei.local/server/internal/gamestate"
)

// CardBase matches the original card-parameter call's input fields. This is
// supplied master data, not an account state mutation or reward calculation.
type CardBase struct {
	HP       int `json:"hp"`
	HPLimit  int `json:"hp_limit"`
	HPLove   int `json:"hp_love_max_bonus"`
	Attack   int `json:"atkp"`
	AtkLimit int `json:"atkp_limit"`
	AtkLove  int `json:"atkp_love_max_bonus"`
	Magic    int `json:"intp"`
	MagLimit int `json:"intp_limit"`
	MagLove  int `json:"intp_love_max_bonus"`
	Mind     int `json:"mndp"`
	MndLimit int `json:"mndp_limit"`
	MndLove  int `json:"mndp_love_max_bonus"`
	LevelMax int `json:"lv_max"`
	LoveMax  int `json:"love_max"`
}

type CardBonus struct {
	HP     int `json:"hp_value"`
	Attack int `json:"atkp_value"`
	Magic  int `json:"intp_value"`
	Mind   int `json:"mndp_value"`
}

type CardParameterRequest struct {
	CardID int       `json:"card_id"`
	Level  int       `json:"lv"`
	Love   int       `json:"love"`
	Fame   int       `json:"fame"`
	Base   CardBase  `json:"param_base"`
	Bonus  CardBonus `json:"bonus"`
}

type CardParameterResult struct {
	HP     int `json:"hp"`
	Attack int `json:"atkp"`
	Magic  int `json:"intp"`
	Mind   int `json:"mndp"`
}

// CalculateCardParameters is a DTO projection onto the server's existing
// ParametersAt implementation. It does not reproduce its formula here.
// Current constraints cover the observed nonnegative native master domain;
// overflow/unsupported inputs fail, rather than yielding a plausible result.
func CalculateCardParameters(req CardParameterRequest) (CardParameterResult, error) {
	if req.CardID <= 0 || req.CardID > math.MaxInt32 || req.Base.LevelMax < 1 || req.Base.LevelMax > 1000 || req.Base.LoveMax < 0 || req.Base.LoveMax > 1000000 {
		return CardParameterResult{}, fmt.Errorf("invalid card identity or parameter range")
	}
	values := []int{req.Base.HP, req.Base.HPLimit, req.Base.HPLove, req.Base.Attack, req.Base.AtkLimit, req.Base.AtkLove, req.Base.Magic, req.Base.MagLimit, req.Base.MagLove, req.Base.Mind, req.Base.MndLimit, req.Base.MndLove, req.Bonus.HP, req.Bonus.Attack, req.Bonus.Magic, req.Bonus.Mind}
	for _, value := range values {
		if value < 0 || value > 1000000 {
			return CardParameterResult{}, fmt.Errorf("unsupported native parameter bound")
		}
	}
	card := gamestate.Card{
		CardID: req.CardID, LevelMax: req.Base.LevelMax, LoveMax: req.Base.LoveMax, FameMax: 100,
		ParameterInitial:          gamestate.CardParameter{HP: req.Base.HP, Attack: req.Base.Attack, Magic: req.Base.Magic, Mind: req.Base.Mind},
		ParameterMaximum:          gamestate.CardParameter{HP: req.Base.HPLimit, Attack: req.Base.AtkLimit, Magic: req.Base.MagLimit, Mind: req.Base.MndLimit},
		ParameterLoveMaximumBonus: gamestate.CardParameter{HP: req.Base.HPLove, Attack: req.Base.AtkLove, Magic: req.Base.MagLove, Mind: req.Base.MndLove},
	}
	policy := gamestate.CardProgressionPolicy{FameNormal: gamestate.CardParameter{HP: req.Bonus.HP, Attack: req.Bonus.Attack, Magic: req.Bonus.Magic, Mind: req.Bonus.Mind}}
	result, err := policy.ParametersAt(card, req.Level, req.Love, req.Fame)
	if err != nil {
		return CardParameterResult{}, err
	}
	return CardParameterResult{HP: result.HP, Attack: result.Attack, Magic: result.Magic, Mind: result.Mind}, nil
}
