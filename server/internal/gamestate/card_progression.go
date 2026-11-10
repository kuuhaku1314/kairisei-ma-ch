package gamestate

import (
	"errors"
	"math"
)

type CardExperienceState struct {
	Level               int
	Experience          int
	NowLevelExperience  int
	NextLevelExperience int
}

// CardFusionMaterialLimit is the original CN selector limit per inventory group.
const CardFusionMaterialLimit = 100

func (policy CardProgressionPolicy) FusionGoldPerMaterial(level int) (int, error) {
	if policy.FusionGoldPerMaterialPerBaseLevel <= 0 || level <= 0 ||
		level > math.MaxInt/policy.FusionGoldPerMaterialPerBaseLevel {
		return 0, errors.New("card fusion gold inputs are invalid")
	}
	return level * policy.FusionGoldPerMaterialPerBaseLevel, nil
}

func NormalizeCardExperience(levelMaximum int, experience int, table []int) (CardExperienceState, error) {
	if levelMaximum < 1 || experience < 0 || len(table) < levelMaximum-1 {
		return CardExperienceState{}, errors.New("card experience inputs are invalid")
	}
	maximum := 0
	for _, required := range table[:levelMaximum-1] {
		if required <= 0 || maximum > math.MaxInt-required {
			return CardExperienceState{}, errors.New("card experience table is invalid")
		}
		maximum += required
	}
	if experience > maximum {
		experience = maximum
	}
	level := 1
	spent := 0
	for level < levelMaximum && experience >= spent+table[level-1] {
		spent += table[level-1]
		level++
	}
	state := CardExperienceState{Level: level, Experience: experience}
	if level < levelMaximum {
		state.NowLevelExperience = experience - spent
		state.NextLevelExperience = table[level-1] - state.NowLevelExperience
	}
	return state, nil
}

func CardExperienceAtLevelStart(level int, levelMaximum int, table []int) (int, error) {
	if level < 1 || level > levelMaximum || len(table) < levelMaximum-1 {
		return 0, errors.New("card level is outside its experience table")
	}
	total := 0
	for _, required := range table[:level-1] {
		if required <= 0 || total > math.MaxInt-required {
			return 0, errors.New("card experience table is invalid")
		}
		total += required
	}
	return total, nil
}

func (policy CardProgressionPolicy) ParametersAt(card Card, level int, love int, fame int) (CardParameter, error) {
	if level < 1 || level > card.LevelMax || card.LevelMax < 1 ||
		love < 0 || love > card.LoveMax || fame < 0 || fame > card.FameMax {
		return CardParameter{}, errors.New("card parameter inputs are invalid")
	}
	fameBonus := policy.FameNormal
	if card.PremiumRarity {
		fameBonus = policy.FamePremium
	}
	return CardParameter{
		HP: calculateCardParameter(card.ParameterInitial.HP, card.ParameterMaximum.HP,
			card.ParameterLoveMaximumBonus.HP, fameBonus.HP, level, card.LevelMax, love, card.LoveMax, fame),
		Attack: calculateCardParameter(card.ParameterInitial.Attack, card.ParameterMaximum.Attack,
			card.ParameterLoveMaximumBonus.Attack, fameBonus.Attack, level, card.LevelMax, love, card.LoveMax, fame),
		Magic: calculateCardParameter(card.ParameterInitial.Magic, card.ParameterMaximum.Magic,
			card.ParameterLoveMaximumBonus.Magic, fameBonus.Magic, level, card.LevelMax, love, card.LoveMax, fame),
		Mind: calculateCardParameter(card.ParameterInitial.Mind, card.ParameterMaximum.Mind,
			card.ParameterLoveMaximumBonus.Mind, fameBonus.Mind, level, card.LevelMax, love, card.LoveMax, fame),
	}, nil
}

func calculateCardParameter(
	initial int,
	maximum int,
	loveMaximumBonus int,
	fameBonus int,
	level int,
	levelMaximum int,
	love int,
	loveMaximum int,
	fame int,
) int {
	value := initial + fameBonus*fame/100
	if level > 1 && levelMaximum > 1 {
		grown := initial + (((maximum-initial)*(level-1)*1000)/(levelMaximum-1))/1000
		if grown > maximum {
			grown = maximum
		}
		value = grown + fameBonus*fame/100
	}
	if love >= loveMaximum {
		value += loveMaximumBonus
	}
	return value
}
