package masterdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"kairisei.local/server/internal/gamestate"
)

// The complete modern ordinary-card projection exceeds the former 16 MiB
// snapshot. Keep an explicit bound while preserving the same inventory schema.
const maxCardMasterBytes = 32 * 1024 * 1024

type CardRuntimeMaster struct {
	CardProgressionPolicy   gamestate.CardProgressionPolicy   `json:"card_progression_policy"`
	CardExperienceTables    map[int][]int                     `json:"card_experience_tables"`
	SphereProgressionPolicy gamestate.SphereProgressionPolicy `json:"sphere_progression_policy"`
	BuddyProgressionPolicy  gamestate.BuddyProgressionPolicy  `json:"buddy_progression_policy"`
	SupportDeckSetCardNum   int                               `json:"support_deck_set_card_num"`

	CardTemplates              []gamestate.Card                      `json:"card_templates"`
	CardSellExtract            *gamestate.CardSellExtractPolicy      `json:"card_sell_extract,omitempty"`
	CardCollectionPages        [][]int                               `json:"card_collection_pages"`
	DeckRankPolicy             gamestate.DeckRankPolicy              `json:"deck_rank_policy"`
	AlchemistStarterDeck       *gamestate.AlchemistStarterDeck       `json:"alchemist_starter_deck,omitempty"`
	StackCardTemplates         []gamestate.CardStack                 `json:"stack_card_templates"`
	CardCategoryProfiles       []gamestate.CardCategoryProfile       `json:"card_category_profiles"`
	CardGroupProfiles          []gamestate.CardGroupProfile          `json:"card_group_profiles"`
	EvolutionTransitions       []gamestate.EvolutionTransition       `json:"evolution_transitions"`
	CardDevelopmentPolicy      gamestate.CardDevelopmentPolicy       `json:"card_development_policy"`
	SphereDefinitions          []gamestate.SphereDefinition          `json:"sphere_definitions"`
	SphereExperienceTables     map[int][]int                         `json:"sphere_experience_tables"`
	SphereEvolutionPrices      map[string][]int                      `json:"sphere_evolution_prices"`
	SphereSeedTemplates        []gamestate.Sphere                    `json:"sphere_seed_templates"`
	SphereSeedDecks            map[string][]int64                    `json:"sphere_seed_decks"`
	BuddyDefinitions           []gamestate.BuddyDefinition           `json:"buddy_definitions"`
	BuddyExperienceTables      map[int][]int                         `json:"buddy_experience_tables"`
	BuddyEvolutionPrices       map[string][]int                      `json:"buddy_evolution_prices"`
	BuddySeedTemplates         []gamestate.Buddy                     `json:"buddy_seed_templates"`
	BuddySeedDecks             map[string][]int64                    `json:"buddy_seed_decks"`
	BuddySeedStackCards        []gamestate.CardStack                 `json:"buddy_seed_stack_cards"`
	SupportDeckSlotUnlockRules []gamestate.SupportDeckSlotUnlockRule `json:"support_deck_slot_unlock_rules"`
}

func LoadCardRuntimeMaster(masterPath string) (CardRuntimeMaster, error) {
	if masterPath == "" {
		return CardRuntimeMaster{}, errors.New("CN card runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return CardRuntimeMaster{}, fmt.Errorf("resolve CN card runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return CardRuntimeMaster{}, fmt.Errorf("open CN card runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return CardRuntimeMaster{}, fmt.Errorf("stat CN card runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCardMasterBytes {
		return CardRuntimeMaster{}, errors.New("CN card runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxCardMasterBytes+1))
	var master CardRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return CardRuntimeMaster{}, fmt.Errorf("decode CN card runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return CardRuntimeMaster{}, err
	}
	if err := validateCardRuntimeMaster(master); err != nil {
		return CardRuntimeMaster{}, err
	}
	return master, nil
}

func validateCardRuntimeMaster(master CardRuntimeMaster) error {
	if err := validateCardCollection(master); err != nil {
		return err
	}
	if err := validateDeckRankPolicy(master.DeckRankPolicy, master.CardTemplates); err != nil {
		return err
	}
	if master.CardProgressionPolicy.FusionGoldPerMaterialPerBaseLevel <= 0 ||
		master.CardProgressionPolicy.MaximumCardMaterialCount != 100 ||
		master.SphereProgressionPolicy.MaterialLevelBonusPermillePerLevel <= 0 ||
		master.BuddyProgressionPolicy.MaximumMaterialCount <= 0 ||
		master.SupportDeckSetCardNum <= 0 ||
		master.SupportDeckSetCardNum > 10 {
		return errors.New("CN card runtime master identity is invalid")
	}
	if len(master.CardTemplates) == 0 || len(master.StackCardTemplates) == 0 ||
		len(master.CardCategoryProfiles) == 0 || len(master.CardGroupProfiles) == 0 ||
		len(master.CardExperienceTables) == 0 ||
		len(master.EvolutionTransitions) == 0 || len(master.SphereDefinitions) == 0 ||
		len(master.SphereExperienceTables) == 0 || len(master.SphereEvolutionPrices) == 0 ||
		len(master.SphereSeedTemplates) == 0 || len(master.SphereSeedDecks) != 4 ||
		len(master.BuddyDefinitions) == 0 || len(master.BuddyExperienceTables) == 0 ||
		len(master.BuddyEvolutionPrices) == 0 || len(master.BuddySeedTemplates) == 0 ||
		len(master.BuddySeedDecks) != 4 || len(master.BuddySeedStackCards) != 2 ||
		len(master.SupportDeckSlotUnlockRules) != master.SupportDeckSetCardNum {
		return errors.New("CN card runtime master is incomplete")
	}
	if err := validateCardGroupProfiles(master); err != nil {
		return err
	}

	if err := validateCardFusionSuccessPolicy(master.CardProgressionPolicy); err != nil {
		return err
	}

	if master.CardDevelopmentPolicy.TimeEveryFameSeconds <= 0 ||
		master.CardDevelopmentPolicy.CoinEveryHour <= 0 ||
		master.CardDevelopmentPolicy.HelpPath == "" || master.CardDevelopmentPolicy.HelpPath[0] != '/' {
		return errors.New("CN card development local policy is invalid")
	}
	for index, rule := range master.SupportDeckSlotUnlockRules {
		if rule.SlotIndex != index+1 || rule.Level <= 0 ||
			rule.CardCollectionNum <= 0 || rule.Gold <= 0 {
			return fmt.Errorf("invalid CN support-deck slot rule %d", index+1)
		}
	}
	cardIDs := make(map[int]struct{}, len(master.CardTemplates))
	for _, card := range master.CardTemplates {
		if card.FusionAttributes > 31 {
			return fmt.Errorf("invalid CN fusion attribute mask for card %d", card.CardID)
		}
		if card.UniqueID != 0 || card.CardID <= 0 || card.Name == "" ||
			card.SameCardID <= 0 || card.SameSupportCardID <= 0 ||
			card.RarityRank < 1 || card.RarityRank > 8 || card.RarityPlus < 0 || card.RarityPlus > 4 ||
			card.Level != 1 || card.LevelMax < 1 || card.ExperienceTableID < 0 ||
			card.Experience != 0 || card.NowLevelExperience != 0 ||
			card.Love != 0 || card.LoveMax < 0 ||
			card.Fame != 1 || card.FameMax < 1 || card.FameMax < card.Fame ||
			card.DevelopmentType < 0 || card.DevelopmentType > 2 ||
			card.DecomposeRadix < 0 || card.DevelopRadix < 0 ||
			(card.DevelopmentType == 2 && card.DevelopRadix == 0) ||
			card.NextLevelExperience < 0 || card.AddExperience <= 0 || card.BaseAddPrice <= 0 ||
			card.SellGold < 0 ||
			card.SkillLevelMax != 1 || len(card.SkillLevels) != 1 || card.SkillLevels[0] != 1 {
			return fmt.Errorf("invalid CN card template %d", card.CardID)
		}
		values, exists := master.CardExperienceTables[card.ExperienceTableID]
		if card.LevelMax > 1 && (!exists || len(values) < card.LevelMax-1) {
			return fmt.Errorf("CN card %d experience table is incomplete", card.CardID)
		}
		normalized, err := normalizeCard(card, card, values, master.CardProgressionPolicy)
		if err != nil || !reflect.DeepEqual(normalized, card) {
			return fmt.Errorf("CN card template %d derived fields are invalid", card.CardID)
		}
		if _, exists := cardIDs[card.CardID]; exists {
			return fmt.Errorf("duplicate CN card template %d", card.CardID)
		}
		cardIDs[card.CardID] = struct{}{}
	}
	stackIDs := make(map[int]gamestate.CardStack, len(master.StackCardTemplates))
	if starter := master.AlchemistStarterDeck; starter != nil {
		seen := make(map[int]bool, 10)
		for _, id := range append(starter.SummonCardIDs[:], starter.SkillCardIDs[:]...) {
			if _, exists := cardIDs[id]; !exists || seen[id] || master.DeckRankPolicy.Cards[id].ArthurType != 5 {
				return fmt.Errorf("invalid alchemist starter card %d", id)
			}
			seen[id] = true
		}
	}
	for _, card := range master.StackCardTemplates {
		if card.CardID <= 0 || card.Num != 0 || card.AddExperience <= 0 || card.BaseAddPrice < 0 || card.MaterialType < 0 || card.MaterialType > 12 {
			return fmt.Errorf("invalid CN stack-card template %d", card.CardID)
		}
		if _, exists := stackIDs[card.CardID]; exists {
			return fmt.Errorf("duplicate CN stack-card template %d", card.CardID)
		}
		stackIDs[card.CardID] = card
	}
	if policy := master.CardSellExtract; policy != nil {
		if policy.ItemID <= 0 || len(policy.CountByBaseRarity) == 0 || len(policy.BaseCardIDs) == 0 || len(policy.BaseRarities) == 0 {
			return errors.New("card sell extract policy is incomplete")
		}
		for rarity, count := range policy.CountByBaseRarity {
			if rarity < 1 || rarity > 8 || count <= 0 {
				return fmt.Errorf("invalid card sell extract rarity %d", rarity)
			}
		}
		for baseID, rarity := range policy.BaseRarities {
			if baseID <= 0 || rarity < 0 || rarity > 10 {
				return fmt.Errorf("invalid card sell extract base rarity %d", baseID)
			}
		}
		for cardID, baseID := range policy.BaseCardIDs {
			if _, exists := policy.BaseRarities[baseID]; cardID <= 0 || baseID <= 0 || !exists {
				return fmt.Errorf("card sell extract source %d has no valid base rarity", cardID)
			}
		}
	}
	for cardID, bonuses := range master.CardProgressionPolicy.FameMaterials {
		if _, exists := stackIDs[cardID]; !exists {
			return fmt.Errorf("unknown CN fame material %d", cardID)
		}
		for _, bonus := range bonuses {
			if bonus < 0 || bonus > math.MaxInt32/master.CardProgressionPolicy.MaximumCardMaterialCount {
				return fmt.Errorf("invalid CN fame material bonus %d", cardID)
			}
		}
	}
	transitions := make(map[[2]int]struct{}, len(master.EvolutionTransitions))
	for _, transition := range master.EvolutionTransitions {
		key := [2]int{transition.FromCardID, transition.ToCardID}
		if transition.Type < 0 || transition.Type > 4 ||
			transition.Gold < 0 || len(transition.Materials) == 0 {
			return fmt.Errorf("invalid CN evolution transition %v", key)
		}
		if _, exists := cardIDs[transition.FromCardID]; !exists {
			return fmt.Errorf("unknown CN evolution source card %d", transition.FromCardID)
		}
		if _, exists := cardIDs[transition.ToCardID]; !exists {
			return fmt.Errorf("unknown CN evolution target card %d", transition.ToCardID)
		}
		if _, exists := transitions[key]; exists {
			return fmt.Errorf("duplicate CN evolution transition %v", key)
		}
		transitions[key] = struct{}{}
		materialIDs := make(map[int]struct{}, len(transition.Materials))
		for _, material := range transition.Materials {
			if material.Num <= 0 || material.Fame < 0 || (transition.Type == 1 && material.Fame == 0) {
				return fmt.Errorf("invalid CN evolution material %d", material.CardID)
			}
			_, isStackCard := stackIDs[material.CardID]
			_, isInstanceCard := cardIDs[material.CardID]
			if material.CardID <= 0 || transition.Type != 4 && !isStackCard && !isInstanceCard {
				return fmt.Errorf("unknown CN evolution material %d", material.CardID)
			}
			if _, exists := materialIDs[material.CardID]; exists {
				return fmt.Errorf("duplicate CN evolution material %d", material.CardID)
			}
			materialIDs[material.CardID] = struct{}{}
		}
	}
	definitions := make(map[int]gamestate.SphereDefinition, len(master.SphereDefinitions))
	for _, definition := range master.SphereDefinitions {
		if definition.SphereID <= 0 || definition.SameSphereID <= 0 || definition.Name == "" ||
			(definition.Type != "NORMAL" && definition.Type != "CHALICE") ||
			definition.MaxLevel <= 0 || definition.EvolutionCount < 0 || definition.Count < 0 ||
			definition.SellGold < 0 || definition.MaterialAddExperience <= 0 || definition.FusionBaseAddPrice <= 0 {
			return fmt.Errorf("invalid CN sphere definition %d", definition.SphereID)
		}
		if _, duplicate := definitions[definition.SphereID]; duplicate {
			return fmt.Errorf("duplicate CN sphere definition %d", definition.SphereID)
		}
		allowed := false
		for _, value := range definition.EquipAllowed {
			allowed = allowed || value
		}
		if !allowed {
			return fmt.Errorf("CN sphere %d has no equipable Arthur", definition.SphereID)
		}
		if definition.MaxLevel > 1 {
			values, exists := master.SphereExperienceTables[definition.ExperienceTableID]
			if !exists || len(values) < definition.MaxLevel-1 {
				return fmt.Errorf("CN sphere %d experience table is incomplete", definition.SphereID)
			}
			for _, value := range values[:definition.MaxLevel-1] {
				if value <= 0 {
					return fmt.Errorf("CN sphere experience table %d has an invalid per-level requirement", definition.ExperienceTableID)
				}
			}
		}
		definitions[definition.SphereID] = definition
	}
	for _, definition := range master.SphereDefinitions {
		if definition.EvolutionID != 0 {
			target, exists := definitions[definition.EvolutionID]
			if !exists || target.SameSphereID != definition.SameSphereID ||
				target.EvolutionCount != definition.EvolutionCount+1 ||
				target.ExperienceTableID != definition.ExperienceTableID ||
				target.MaxLevel < definition.MaxLevel {
				return fmt.Errorf("invalid CN sphere evolution %d -> %d", definition.SphereID, definition.EvolutionID)
			}
		}
	}
	for _, rarity := range []string{"NORMAL", "RARE", "MILLIONRARE"} {
		values, exists := master.SphereEvolutionPrices[rarity]
		if !exists || len(values) != 4 {
			return fmt.Errorf("CN sphere evolution prices for %s are incomplete", rarity)
		}
		for _, value := range values {
			if value < 0 {
				return fmt.Errorf("CN sphere evolution price for %s is negative", rarity)
			}
		}
	}
	seedIDs := make(map[int64]gamestate.Sphere, len(master.SphereSeedTemplates))
	for _, sphere := range master.SphereSeedTemplates {
		definition, exists := definitions[sphere.SphereID]
		if !exists || sphere.UniqueID <= 0 || sphere.Level < 1 || sphere.Level > definition.MaxLevel ||
			sphere.Experience < 0 || sphere.IsLock < 0 || sphere.IsLock > 1 ||
			sphere.BaseAddPrice != definition.FusionBaseAddPrice {
			return fmt.Errorf("invalid CN sphere seed %d", sphere.UniqueID)
		}
		normalized, err := normalizeSphere(
			sphere, definition, master.SphereExperienceTables, master.SphereProgressionPolicy,
		)
		if err != nil || normalized != sphere {
			return fmt.Errorf("CN sphere seed %d derived fields are invalid", sphere.UniqueID)
		}
		if _, duplicate := seedIDs[sphere.UniqueID]; duplicate {
			return fmt.Errorf("duplicate CN sphere seed unique ID %d", sphere.UniqueID)
		}
		seedIDs[sphere.UniqueID] = sphere
	}
	for arthurType := 1; arthurType <= 4; arthurType++ {
		uniqueIDs, exists := master.SphereSeedDecks[fmt.Sprint(arthurType)]
		if !exists || len(uniqueIDs) != 3 {
			return fmt.Errorf("CN sphere seed deck %d is incomplete", arthurType)
		}
		families := make(map[int]struct{}, 3)
		for _, uniqueID := range uniqueIDs {
			sphere, owned := seedIDs[uniqueID]
			definition := definitions[sphere.SphereID]
			if !owned || !definition.EquipAllowed[arthurType-1] {
				return fmt.Errorf("CN sphere seed deck %d references unusable sphere %d", arthurType, uniqueID)
			}
			if _, duplicate := families[definition.SameSphereID]; duplicate {
				return fmt.Errorf("CN sphere seed deck %d repeats sphere family %d", arthurType, definition.SameSphereID)
			}
			families[definition.SameSphereID] = struct{}{}
		}
	}
	buddyDefinitions := make(map[int]gamestate.BuddyDefinition, len(master.BuddyDefinitions))
	for _, definition := range master.BuddyDefinitions {
		if definition.BuddyID <= 0 || definition.SameBuddyID <= 0 || definition.Name == "" ||
			definition.EvolutionCount < 0 || definition.MaxLevel <= 0 ||
			definition.SellGold < 0 || definition.MaterialAddExperience <= 0 ||
			definition.FusionBaseAddPrice <= 0 ||
			(definition.OverlimitItemID == 0) != (definition.OverlimitItemNum == 0) {
			return fmt.Errorf("invalid CN buddy definition %d", definition.BuddyID)
		}
		if _, duplicate := buddyDefinitions[definition.BuddyID]; duplicate {
			return fmt.Errorf("duplicate CN buddy definition %d", definition.BuddyID)
		}
		values, exists := master.BuddyExperienceTables[definition.ExperienceTableID]
		if !exists || len(values) < definition.MaxLevel-1 {
			return fmt.Errorf("CN buddy %d experience table is incomplete", definition.BuddyID)
		}
		for _, value := range values[:definition.MaxLevel-1] {
			if value <= 0 {
				return fmt.Errorf("CN buddy experience table %d is invalid", definition.ExperienceTableID)
			}
		}
		buddyDefinitions[definition.BuddyID] = definition
	}
	for _, definition := range master.BuddyDefinitions {
		if definition.EvolutionID != 0 {
			target, exists := buddyDefinitions[definition.EvolutionID]
			if !exists || target.SameBuddyID != definition.SameBuddyID ||
				target.EvolutionCount != definition.EvolutionCount+1 ||
				target.ExperienceTableID != definition.ExperienceTableID ||
				target.MaxLevel < definition.MaxLevel {
				return fmt.Errorf("invalid CN buddy evolution %d -> %d", definition.BuddyID, definition.EvolutionID)
			}
		}
	}
	for _, rarity := range []string{"NORMAL", "RARE", "MILLIONRARE", "EXRARE", "LEGEND"} {
		values, exists := master.BuddyEvolutionPrices[rarity]
		if !exists || len(values) != 5 {
			return fmt.Errorf("CN buddy evolution prices for %s are incomplete", rarity)
		}
		for _, value := range values {
			if value < 0 {
				return fmt.Errorf("CN buddy evolution price for %s is negative", rarity)
			}
		}
	}
	buddySeedIDs := make(map[int64]gamestate.Buddy, len(master.BuddySeedTemplates))
	for _, buddy := range master.BuddySeedTemplates {
		definition, exists := buddyDefinitions[buddy.BuddyID]
		if !exists || buddy.UniqueID <= 0 || buddy.IsLock < 0 || buddy.IsLock > 1 {
			return fmt.Errorf("invalid CN buddy seed %d", buddy.UniqueID)
		}
		normalized, err := normalizeBuddy(buddy, definition, master.BuddyExperienceTables)
		if err != nil || normalized != buddy {
			return fmt.Errorf("CN buddy seed %d derived fields are invalid", buddy.UniqueID)
		}
		if _, duplicate := buddySeedIDs[buddy.UniqueID]; duplicate {
			return fmt.Errorf("duplicate CN buddy seed unique ID %d", buddy.UniqueID)
		}
		buddySeedIDs[buddy.UniqueID] = buddy
	}
	for arthurType := 1; arthurType <= 4; arthurType++ {
		uniqueIDs, exists := master.BuddySeedDecks[fmt.Sprint(arthurType)]
		if !exists || len(uniqueIDs) != 5 {
			return fmt.Errorf("CN buddy seed deck %d is incomplete", arthurType)
		}
		for _, uniqueID := range uniqueIDs {
			if uniqueID != 0 {
				if _, owned := buddySeedIDs[uniqueID]; !owned {
					return fmt.Errorf("CN buddy seed deck %d references unknown buddy %d", arthurType, uniqueID)
				}
			}
		}
	}
	for _, stack := range master.BuddySeedStackCards {
		template, exists := stackIDs[stack.CardID]
		if !exists || stack.Num <= 0 || stack.MaterialType < 9 || stack.MaterialType > 10 ||
			stack.AddExperience != template.AddExperience || stack.BaseAddPrice != template.BaseAddPrice {
			return fmt.Errorf("invalid CN buddy seed stack card %d", stack.CardID)
		}
	}
	return nil
}

func validateCardFusionSuccessPolicy(policy gamestate.CardProgressionPolicy) error {
	if len(policy.FusionSuccessTypes) != 3 {
		return errors.New("CN card fusion success policy must contain three result types")
	}
	for index, result := range policy.FusionSuccessTypes {
		if result.SuccessType != index || result.Weight <= 0 || result.ExperiencePermille < 1000 ||
			(index == 0 && result.ExperiencePermille != 1000) {
			return fmt.Errorf("CN card fusion success type %d is invalid", index)
		}
	}
	return nil
}

func ApplyCardRuntimeMaster(state *gamestate.State, master CardRuntimeMaster) error {
	state.DeckRankPolicy = master.DeckRankPolicy
	state.AlchemistStarterDeck = master.AlchemistStarterDeck
	state.CardTemplates = append([]gamestate.Card(nil), master.CardTemplates...)
	state.CardSellExtract = master.CardSellExtract
	state.CardCategoryProfiles = append(
		[]gamestate.CardCategoryProfile(nil), master.CardCategoryProfiles...,
	)
	state.CardGroupProfiles = cloneCardGroupProfiles(master.CardGroupProfiles)
	state.CardCollectionPages = make([][10]int, len(master.CardCollectionPages))
	for i, page := range master.CardCollectionPages {
		state.CardCollectionPages[i] = [10]int(page)
	}
	state.CardProgressionPolicy = master.CardProgressionPolicy
	state.CardExperienceTables = cloneCardExperienceTables(master.CardExperienceTables)
	state.SphereProgressionPolicy = master.SphereProgressionPolicy
	state.BuddyProgressionPolicy = master.BuddyProgressionPolicy
	state.CardDevelopmentPolicy = master.CardDevelopmentPolicy
	state.CardActions.EvolutionTransitions = append(
		[]gamestate.EvolutionTransition(nil), master.EvolutionTransitions...,
	)
	state.SupportDeckSetCardNum = master.SupportDeckSetCardNum
	state.SupportDeckSlotUnlockRules = append(
		[]gamestate.SupportDeckSlotUnlockRule(nil),
		master.SupportDeckSlotUnlockRules...,
	)
	cardTemplates := make(map[int]gamestate.Card, len(master.CardTemplates))
	for _, template := range master.CardTemplates {
		cardTemplates[template.CardID] = template
	}
	for _, inventory := range []*[]gamestate.Card{&state.Cards, &state.ContainerCards} {
		for index, card := range *inventory {
			template, exists := cardTemplates[card.CardID]
			if !exists {
				return fmt.Errorf("CN save inventory references unknown card %d", card.CardID)
			}
			values := master.CardExperienceTables[template.ExperienceTableID]
			normalized, err := normalizeCard(
				card, template, values, master.CardProgressionPolicy,
			)
			if err != nil {
				return err
			}
			(*inventory)[index] = normalized
		}
	}
	state.CardActions.FusionGoldPerCard = master.CardProgressionPolicy.FusionGoldPerMaterialPerBaseLevel
	if len(state.SupportDeck.UnlockSlotNums) != 4 {
		return errors.New("CN save support-deck progression must contain four Arthur slots")
	}
	for index, count := range state.SupportDeck.UnlockSlotNums {
		if count < 0 || int(count) > master.SupportDeckSetCardNum {
			return fmt.Errorf("CN save support-deck slot count for Arthur %d is invalid", index+1)
		}
	}
	knownCardIDs := make(map[int]struct{}, len(master.CardTemplates))
	for _, card := range master.CardTemplates {
		knownCardIDs[card.CardID] = struct{}{}
	}
	for _, card := range master.StackCardTemplates {
		knownCardIDs[card.CardID] = struct{}{}
	}
	collected := make(map[int]struct{}, len(state.SupportDeck.CardCollectionIDs)+len(state.Cards)+len(state.ContainerCards))
	for _, cardID := range state.SupportDeck.CardCollectionIDs {
		if _, exists := knownCardIDs[cardID]; !exists {
			return fmt.Errorf("CN save card collection references unknown card %d", cardID)
		}
		collected[cardID] = struct{}{}
	}
	for _, inventory := range [][]gamestate.Card{state.Cards, state.ContainerCards} {
		for _, card := range inventory {
			if _, exists := knownCardIDs[card.CardID]; !exists {
				return fmt.Errorf("CN save inventory references unknown card %d", card.CardID)
			}
			collected[card.CardID] = struct{}{}
		}
	}
	for _, card := range state.StackCards {
		if card.Num > 0 {
			collected[card.CardID] = struct{}{}
		}
	}
	normalizedCollection := make([]int, 0, len(collected))
	for cardID := range collected {
		normalizedCollection = append(normalizedCollection, cardID)
	}
	sort.Ints(normalizedCollection)
	state.SupportDeck.CardCollectionIDs = normalizedCollection
	stackTemplates := make(map[int]gamestate.CardStack, len(master.StackCardTemplates))
	normalizeCollectionLoveHistory(state)
	for _, template := range master.StackCardTemplates {
		stackTemplates[template.CardID] = template
	}
	state.StackCardTemplates = append([]gamestate.CardStack(nil), master.StackCardTemplates...)
	for index, inventory := range state.StackCards {
		template, exists := stackTemplates[inventory.CardID]
		if !exists {
			return fmt.Errorf("CN save references unknown stack card %d", inventory.CardID)
		}
		if inventory.Num < 0 {
			return fmt.Errorf("CN save stack card %d has negative inventory", inventory.CardID)
		}
		template.Num = inventory.Num
		state.StackCards[index] = template
	}
	state.SphereDefinitions = append([]gamestate.SphereDefinition(nil), master.SphereDefinitions...)
	state.SphereExperienceTables = cloneSphereExperienceTables(master.SphereExperienceTables)
	state.SphereEvolutionPrices = cloneSphereEvolutionPrices(master.SphereEvolutionPrices)
	definitions := make(map[int]gamestate.SphereDefinition, len(master.SphereDefinitions))
	for _, definition := range master.SphereDefinitions {
		definitions[definition.SphereID] = definition
	}
	uniqueIDs := make(map[int64]struct{}, len(state.Spheres))
	for index, sphere := range state.Spheres {
		definition, exists := definitions[sphere.SphereID]
		if !exists || sphere.UniqueID <= 0 || sphere.IsLock < 0 || sphere.IsLock > 1 {
			return fmt.Errorf("CN save references invalid sphere %d", sphere.UniqueID)
		}
		if _, duplicate := uniqueIDs[sphere.UniqueID]; duplicate {
			return fmt.Errorf("CN save repeats sphere unique ID %d", sphere.UniqueID)
		}
		uniqueIDs[sphere.UniqueID] = struct{}{}
		normalized, err := normalizeSphere(
			sphere, definition, master.SphereExperienceTables, master.SphereProgressionPolicy,
		)
		if err != nil {
			return err
		}
		state.Spheres[index] = normalized
	}
	state.BuddyDefinitions = append([]gamestate.BuddyDefinition(nil), master.BuddyDefinitions...)
	state.BuddyExperienceTables = cloneBuddyExperienceTables(master.BuddyExperienceTables)
	state.BuddyEvolutionPrices = cloneBuddyEvolutionPrices(master.BuddyEvolutionPrices)
	buddyDefinitions := make(map[int]gamestate.BuddyDefinition, len(master.BuddyDefinitions))
	for _, definition := range master.BuddyDefinitions {
		buddyDefinitions[definition.BuddyID] = definition
	}
	buddyUniqueIDs := make(map[int64]struct{}, len(state.Buddies))
	for index, buddy := range state.Buddies {
		definition, exists := buddyDefinitions[buddy.BuddyID]
		if !exists || buddy.UniqueID <= 0 || buddy.IsLock < 0 || buddy.IsLock > 1 {
			return fmt.Errorf("CN save references invalid buddy %d", buddy.UniqueID)
		}
		if _, duplicate := buddyUniqueIDs[buddy.UniqueID]; duplicate {
			return fmt.Errorf("CN save repeats buddy unique ID %d", buddy.UniqueID)
		}
		buddyUniqueIDs[buddy.UniqueID] = struct{}{}
		normalized, err := normalizeBuddy(buddy, definition, master.BuddyExperienceTables)
		if err != nil {
			return err
		}
		state.Buddies[index] = normalized
	}
	for _, deck := range state.Decks {
		for _, uniqueID := range deck.BuddyUniqueIDs {
			if uniqueID != 0 {
				if _, exists := buddyUniqueIDs[uniqueID]; !exists {
					return fmt.Errorf("CN deck references unknown buddy unique ID %d", uniqueID)
				}
			}
		}
	}
	return nil
}

func validateCardGroupProfiles(master CardRuntimeMaster) error {
	// Dynamic directory keys continue the sourced packaged base inventory.
	// Exact offsets are established by the catalog producer; only continuity
	// and the established minimum base keys belong to this runtime contract.
	categoryOrder := master.CardCategoryProfiles[0].Order
	groupOrder := master.CardGroupProfiles[0].Order
	if categoryOrder < 2 || groupOrder < 10 {
		return errors.New("card directory overlaps packaged base keys")
	}
	categories := make(map[int]struct{}, len(master.CardCategoryProfiles))
	for index, category := range master.CardCategoryProfiles {
		if category.CategoryID <= 0 || category.CategoryID == 1 || category.CategoryID == 1000 ||
			strings.TrimSpace(category.Name) == "" || category.Order != categoryOrder+index ||
			category.ViewType != 0 {
			return errors.New("CN card category profile is invalid")
		}
		if _, duplicate := categories[category.CategoryID]; duplicate {
			return fmt.Errorf("duplicate card category %d", category.CategoryID)
		}
		categories[category.CategoryID] = struct{}{}
	}
	knownCards := make(map[int]struct{}, len(master.CardTemplates)+len(master.StackCardTemplates))
	for _, card := range master.CardTemplates {
		knownCards[card.CardID] = struct{}{}
	}
	for _, card := range master.StackCardTemplates {
		knownCards[card.CardID] = struct{}{}
	}
	groups := make(map[int]struct{}, len(master.CardGroupProfiles))
	for index, group := range master.CardGroupProfiles {
		_, categoryExists := categories[group.CategoryID]
		if group.GroupID <= 0 || !categoryExists || strings.TrimSpace(group.Name) == "" ||
			group.Order != groupOrder+index || group.DeckLimitBossID < 0 ||
			len(group.MemberCardIDs) == 0 {
			return fmt.Errorf("CN card group profile %d is invalid", group.GroupID)
		}
		if _, duplicate := groups[group.GroupID]; duplicate {
			return fmt.Errorf("duplicate card group %d", group.GroupID)
		}
		groups[group.GroupID] = struct{}{}
		previous := 0
		for _, cardID := range group.MemberCardIDs {
			if cardID <= previous {
				return fmt.Errorf("CN card group %d members are not unique and sorted", group.GroupID)
			}
			if _, exists := knownCards[cardID]; !exists {
				return fmt.Errorf("CN card group %d references unknown card %d", group.GroupID, cardID)
			}
			previous = cardID
		}
	}
	return nil
}

func cloneCardGroupProfiles(source []gamestate.CardGroupProfile) []gamestate.CardGroupProfile {
	result := make([]gamestate.CardGroupProfile, len(source))
	for index, group := range source {
		result[index] = group
		result[index].MemberCardIDs = append([]int(nil), group.MemberCardIDs...)
	}
	return result
}

func normalizeCard(
	card gamestate.Card,
	template gamestate.Card,
	experienceTable []int,
	policy gamestate.CardProgressionPolicy,
) (gamestate.Card, error) {
	if card.UniqueID < 0 || card.Level < 1 || card.Level > template.LevelMax ||
		card.Experience < 0 || card.Love < 0 || card.Love > template.LoveMax ||
		card.Fame < 0 || card.Fame > template.FameMax || card.IsLock < 0 || card.IsLock > 1 {
		return gamestate.Card{}, fmt.Errorf("CN card %d mutable state is invalid", card.UniqueID)
	}
	experienceState, err := gamestate.NormalizeCardExperience(template.LevelMax, card.Experience, experienceTable)
	if err != nil {
		return gamestate.Card{}, fmt.Errorf("normalize CN card %d experience: %w", card.UniqueID, err)
	}
	parameters, err := policy.ParametersAt(template, experienceState.Level, card.Love, card.Fame)
	if err != nil {
		return gamestate.Card{}, fmt.Errorf("normalize CN card %d parameters: %w", card.UniqueID, err)
	}
	normalized := template
	normalized.UniqueID = card.UniqueID
	normalized.CreateTime = card.CreateTime
	normalized.Level = experienceState.Level
	normalized.Experience = experienceState.Experience
	normalized.NowLevelExperience = experienceState.NowLevelExperience
	normalized.NextLevelExperience = experienceState.NextLevelExperience
	normalized.Love = card.Love
	normalized.SkillLevels = append([]int16(nil), card.SkillLevels...)
	if len(normalized.SkillLevels) == 0 {
		return gamestate.Card{}, fmt.Errorf("CN card %d has no skill levels", card.UniqueID)
	}
	normalized.HP = parameters.HP
	normalized.Attack = parameters.Attack
	normalized.Magic = parameters.Magic
	normalized.Mind = parameters.Mind
	fusionGold, err := policy.FusionGoldPerMaterial(experienceState.Level)
	if err != nil {
		return gamestate.Card{}, fmt.Errorf("normalize CN card %d fusion gold: %w", card.UniqueID, err)
	}
	normalized.BaseAddPrice = fusionGold
	normalized.IsLock = card.IsLock
	normalized.Fame = card.Fame
	return normalized, nil
}

func cloneCardExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func normalizeSphere(
	sphere gamestate.Sphere,
	definition gamestate.SphereDefinition,
	tables map[int][]int,
	policy gamestate.SphereProgressionPolicy,
) (gamestate.Sphere, error) {
	values, exists := tables[definition.ExperienceTableID]
	if !exists && definition.MaxLevel > 1 {
		return gamestate.Sphere{}, fmt.Errorf("CN sphere %d experience table is unavailable", sphere.UniqueID)
	}
	progression, err := gamestate.NormalizeSphereExperience(definition.MaxLevel, sphere.Experience, values)
	if err != nil || progression.Level != sphere.Level {
		return gamestate.Sphere{}, fmt.Errorf("CN sphere %d experience does not match level %d", sphere.UniqueID, sphere.Level)
	}
	sphere.Experience = progression.Experience
	sphere.NowLevelExperience = progression.NowLevelExperience
	sphere.NextLevelExperience = progression.NextLevelExperience
	addExperience, err := gamestate.SphereMaterialExperience(
		definition.MaterialAddExperience,
		sphere.Level,
		policy.MaterialLevelBonusPermillePerLevel,
	)
	if err != nil {
		return gamestate.Sphere{}, fmt.Errorf("CN sphere %d material experience: %w", sphere.UniqueID, err)
	}
	sphere.AddExperience = addExperience
	sphere.BaseAddPrice = definition.FusionBaseAddPrice
	return sphere, nil
}

func cloneSphereExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func cloneSphereEvolutionPrices(source map[string][]int) map[string][]int {
	result := make(map[string][]int, len(source))
	for rarity, values := range source {
		result[rarity] = append([]int(nil), values...)
	}
	return result
}

func normalizeBuddy(buddy gamestate.Buddy, definition gamestate.BuddyDefinition, tables map[int][]int) (gamestate.Buddy, error) {
	if buddy.Level < 1 || buddy.Level > definition.MaxLevel || buddy.Experience < 0 {
		return gamestate.Buddy{}, fmt.Errorf("CN buddy %d level or experience is invalid", buddy.UniqueID)
	}
	values, exists := tables[definition.ExperienceTableID]
	if !exists || len(values) < definition.MaxLevel-1 {
		return gamestate.Buddy{}, fmt.Errorf("CN buddy %d experience table is unavailable", buddy.UniqueID)
	}
	spent := 0
	for level := 1; level < buddy.Level; level++ {
		spent += values[level-1]
	}
	if buddy.Experience < spent {
		return gamestate.Buddy{}, fmt.Errorf("CN buddy %d experience does not match level %d", buddy.UniqueID, buddy.Level)
	}
	nowExperience, nextExperience := 0, 0
	if buddy.Level < definition.MaxLevel {
		required := values[buddy.Level-1]
		nowExperience = buddy.Experience - spent
		if nowExperience < 0 || nowExperience >= required {
			return gamestate.Buddy{}, fmt.Errorf("CN buddy %d experience does not match level %d", buddy.UniqueID, buddy.Level)
		}
		nextExperience = required - nowExperience
	} else if buddy.Experience != spent {
		return gamestate.Buddy{}, fmt.Errorf("CN buddy %d maximum-level experience is invalid", buddy.UniqueID)
	}
	buddy.NowLevelExperience = nowExperience
	buddy.NextLevelExperience = nextExperience
	buddy.AddExperience = definition.MaterialAddExperience
	buddy.BaseAddPrice = definition.FusionBaseAddPrice
	return buddy, nil
}

func cloneBuddyExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func cloneBuddyEvolutionPrices(source map[string][]int) map[string][]int {
	result := make(map[string][]int, len(source))
	for rarity, values := range source {
		result[rarity] = append([]int(nil), values...)
	}
	return result
}
