package game

import (
	"fmt"
	"maps"

	"kairisei.local/server/internal/gamestate"
)

// CardCatalog owns the application's immutable card master and derived indexes.
// Build it once from prepared master data; player inventory never lives here.
// New without WithCardCatalog builds a private catalog for isolated callers.
type CardCatalog struct {
	templates       []gamestate.Card
	definitions     map[int]gamestate.Card
	grants          map[int]CardInfo
	love            map[int]cardLoveRule
	development     map[int]cardDevelopmentRule
	experience      map[int][]int
	categories      []gamestate.CardCategoryProfile
	groups          []gamestate.CardGroupProfile
	collectionPages [][10]int
	sellExtract     *gamestate.CardSellExtractPolicy
}

func NewCardCatalog(state gamestate.State) (*CardCatalog, error) {
	c := &CardCatalog{
		templates:       append([]gamestate.Card(nil), state.CardTemplates...),
		definitions:     make(map[int]gamestate.Card, len(state.CardTemplates)),
		grants:          make(map[int]CardInfo, len(state.CardTemplates)),
		love:            make(map[int]cardLoveRule, len(state.CardTemplates)),
		development:     make(map[int]cardDevelopmentRule, len(state.CardTemplates)),
		experience:      cloneCardExperienceTables(state.CardExperienceTables),
		categories:      append([]gamestate.CardCategoryProfile(nil), state.CardCategoryProfiles...),
		groups:          cloneCardGroupProfiles(state.CardGroupProfiles),
		collectionPages: append([][10]int(nil), state.CardCollectionPages...),
	}
	grants := CardsFromState(state.CardTemplates, 0)
	for i := range c.templates {
		persisted := &c.templates[i]
		if persisted.LoveMax < 0 {
			return nil, fmt.Errorf("card template %d has an invalid love maximum", persisted.CardID)
		}
		persisted.SkillLevels = append([]int16(nil), persisted.SkillLevels...)
		card := grants[i]
		card.CreateTime = 0
		card.IsLock = 0
		c.grants[card.CardID] = card
		c.definitions[card.CardID] = *persisted
		c.love[card.CardID] = cardLoveRule{Max: persisted.LoveMax, Premium: persisted.PremiumRarity}
		c.development[card.CardID] = cardDevelopmentRule{
			FameMax: persisted.FameMax, DevelopmentType: persisted.DevelopmentType,
			DecomposeRadix: persisted.DecomposeRadix, DevelopRadix: persisted.DevelopRadix,
			AcquisitionText: persisted.AcquisitionText,
		}
	}
	if policy := state.CardSellExtract; policy != nil {
		foundItem := false
		for _, item := range state.ItemDefinitions {
			if item.ItemID == policy.ItemID {
				foundItem = true
				break
			}
		}
		if !foundItem {
			return nil, fmt.Errorf("card sell extract item %d is not in the item catalog", policy.ItemID)
		}
		c.sellExtract = &gamestate.CardSellExtractPolicy{
			ItemID: policy.ItemID, CountByBaseRarity: maps.Clone(policy.CountByBaseRarity),
			BaseCardIDs: maps.Clone(policy.BaseCardIDs), BaseRarities: maps.Clone(policy.BaseRarities),
		}
	}
	return c, nil
}

// ApplyDefinitions replaces only public card definitions with read-only views.
// Call after master normalization; consumers must not mutate these fields.
func (c *CardCatalog) ApplyDefinitions(state *gamestate.State) {
	state.CardTemplates = c.templates
	state.CardExperienceTables = c.experience
	state.CardCategoryProfiles = c.categories
	state.CardGroupProfiles = c.groups
	state.CardCollectionPages = c.collectionPages
	state.CardSellExtract = c.sellExtract
}

func WithCardCatalog(catalog *CardCatalog) AccountOption {
	return func(options *accountOptions) { options.cardCatalog = catalog }
}

func (s *Account) useCardCatalog(c *CardCatalog) {
	s.cardTemplates = c.grants
	s.cardDefinitions = c.definitions
	s.cardLoveRules = c.love
	s.cardDevelopmentRules = c.development
	s.cardExperience = c.experience
	s.cardCategoryProfiles = c.categories
	s.cardGroupProfiles = c.groups
	s.cardCollectionPages = c.collectionPages
	s.cardSellExtract = c.sellExtract
}
