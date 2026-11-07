package cnbootstrap

import (
	"kairisei.local/server/internal/clientbridge"
	"kairisei.local/server/internal/httpapi"
)

// CN scalar positions and modern named parameter families are a wire contract,
// projected before JSON encoding so large inventories never make a JSON round trip.
type cnCardShowInfo struct {
	UniqueID       int64                                `json:"0"`
	CardID         int                                  `json:"1"`
	Level          int                                  `json:"2"`
	LevelMax       int                                  `json:"3"`
	Experience     int                                  `json:"4"`
	Love           int                                  `json:"5"`
	SkillLevels    []int16                              `json:"6"`
	HP             int                                  `json:"7"`
	Attack         int                                  `json:"8"`
	Magic          int                                  `json:"9"`
	Mind           int                                  `json:"10"`
	NextLevelEXP   int                                  `json:"11"`
	NowLevelEXP    int                                  `json:"12"`
	AddEXP         int                                  `json:"13"`
	BaseAddPrice   int                                  `json:"14"`
	IsLock         int8                                 `json:"15"`
	CreateTime     int                                  `json:"16"`
	Fame           int                                  `json:"17"`
	Slot           int                                  `json:"18"`
	Passive        []any                                `json:"19"`
	AlchemistParam []clientbridge.CardParameterResult   `json:"alchemist_param"`
	SummonsParam   []clientbridge.SummonParameterResult `json:"summons_param"`
}

func projectCNCardShow(view httpapi.CardShowView) (any, error) {
	cards := make([]cnCardShowInfo, len(view.Cards))
	for i, source := range view.Cards {
		card, err := source.WithParameters()
		if err != nil {
			return nil, err
		}
		cards[i] = cnCardShowInfo{
			UniqueID:       card.UniqueID,
			CardID:         card.CardID,
			Level:          card.Level,
			LevelMax:       card.LevelMax,
			Experience:     card.Experience,
			Love:           card.Love,
			SkillLevels:    card.SkillLevels,
			HP:             card.HP,
			Attack:         card.Attack,
			Magic:          card.Magic,
			Mind:           card.Mind,
			NextLevelEXP:   card.NextLevelEXP,
			NowLevelEXP:    card.NowLevelEXP,
			AddEXP:         card.AddEXP,
			BaseAddPrice:   card.BaseAddPrice,
			IsLock:         card.IsLock,
			CreateTime:     card.CreateTime,
			Fame:           card.Fame,
			Slot:           card.Slot,
			Passive:        card.Passive,
			AlchemistParam: card.AlchemistParam,
			SummonsParam:   card.SummonsParam,
		}
	}
	return map[string]any{"0": int64(0), "1": cards, "2": cnNonNilList(view.StackCards), "3": cnNonNilList(view.Decks), "4": cnNonNilList(view.Avatars), "5": cnNonNilList(view.SupportSlots), "6": view.CardCollectionNum}, nil
}

func cnNonNilList[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
