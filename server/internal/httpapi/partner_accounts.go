package httpapi

import (
	"errors"
	"sort"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

type friendPointPartnerView struct {
	IsBurst         int8
	LastLoginUnix   int64
	InviteID        string
	UserID          int
	Name            string
	Comment         string
	Level           int
	PVPPoint        int
	ArthurType      int8
	Cards           map[int64]game.CardInfo
	CardAttributes  map[int]uint8
	CardKinds       map[int]int
	Decks           []game.DeckInfo
	Avatar          gamestate.Avatar
	Spheres         map[int64]gamestate.Sphere
	Buddies         map[int64]gamestate.Buddy
	SupportUnlocked int8
	Jobs            []gamestate.JobParameter
	HonorIDs        []int
	FriendState     int8
	System          bool
}

func friendPointPartnerViewFromState(state gamestate.State) (friendPointPartnerView, bool) {
	return partnerViewFromState(state, 0)
}

// An explicit profession is a player's own selected deck. Zero selects the
// public rental profession, which may differ from the owner's active one.
func partnerViewFromState(state gamestate.State, arthurType int8) (friendPointPartnerView, bool) {
	useRental := arthurType == 0
	if useRental {
		arthurType = int8(state.User.ActiveArthurType)
	}
	if state.User.UserID <= 0 || state.User.Name == "" || arthurType < 1 || arthurType > 4 ||
		len(state.Avatars) != 4 || len(state.SupportDeck.UnlockSlotNums) != 4 {
		return friendPointPartnerView{}, false
	}
	attributes := make(map[int]uint8, len(state.Cards))
	for _, card := range state.Cards {
		attributes[card.CardID] = card.FusionAttributes
	}
	cards := game.CardsFromState(state.Cards, 0)
	cardByUniqueID := make(map[int64]game.CardInfo, len(cards))
	for _, card := range cards {
		cardByUniqueID[card.UniqueID] = card
	}
	decks := game.DecksFromState(state.Decks)
	if useRental {
		rentalDeck, found := selectRentalPartnerDeck(decks, arthurType, cardByUniqueID)
		if !found {
			return friendPointPartnerView{}, false
		}
		arthurType = rentalDeck.ArthurType
	}
	buddies := make(map[int64]gamestate.Buddy, len(state.Buddies))
	for _, buddy := range state.Buddies {
		buddies[buddy.UniqueID] = buddy
	}
	spheres := make(map[int64]gamestate.Sphere, len(state.Spheres))
	for _, sphere := range state.Spheres {
		spheres[sphere.UniqueID] = sphere
	}
	honorIDs := make([]int, 4)
	copy(honorIDs, state.Honors.DeckHonorIDs)
	return friendPointPartnerView{
		IsBurst:         gamestate.ArthurBurstUnlocked(state.User.UnlockedFeatureIDs, arthurType),
		InviteID:        state.User.InviteID,
		LastLoginUnix:   state.LastLoginUnix,
		UserID:          state.User.UserID,
		Name:            state.User.Name,
		Comment:         state.User.Comment,
		Level:           state.User.Level,
		PVPPoint:        state.User.PVPPoint,
		ArthurType:      arthurType,
		Cards:           cardByUniqueID,
		CardAttributes:  attributes,
		Decks:           decks,
		Avatar:          state.Avatars[int(arthurType)-1],
		Spheres:         spheres,
		Buddies:         buddies,
		SupportUnlocked: state.SupportDeck.UnlockSlotNums[int(arthurType)-1],
		Jobs:            append([]gamestate.JobParameter(nil), state.User.Jobs...),
		HonorIDs:        honorIDs,
	}, true
}

func (view friendPointPartnerView) job(jobType int8) gamestate.JobParameter {
	index := int(jobType)
	if index < 0 || index >= len(view.Jobs) {
		return gamestate.JobParameter{}
	}
	return view.Jobs[index]
}

func (view friendPointPartnerView) activeDeck() (game.DeckInfo, bool) {
	return selectRentalPartnerDeck(view.Decks, view.ArthurType, view.Cards)
}

// The native deck editor marks the public rental decks independently of the
// Arthur currently used by the owner. Never substitute an active battle deck
// when the owner has explicitly selected rental decks.
func selectRentalPartnerDeck(decks []game.DeckInfo, fallbackArthur int8, cards map[int64]game.CardInfo) (game.DeckInfo, bool) {
	hasRental := false
	for _, deck := range decks {
		if deck.IsRental == 0 {
			continue
		}
		hasRental = true
		if complete, ok := selectCompletePartnerDeck([]game.DeckInfo{deck}, deck.ArthurType, cards); ok {
			return complete, true
		}
	}
	if hasRental {
		return game.DeckInfo{}, false
	}
	return selectCompletePartnerDeck(decks, fallbackArthur, cards)
}

func (view friendPointPartnerView) listWire(friendPoint int, friendState int8) (map[string]any, error) {
	deck, found := view.activeDeck()
	if !found {
		return nil, errors.New("friend-point partner active deck is unavailable")
	}
	leader, found := game.PartnerLeaderCard(deck, view.Cards)
	if !found {
		return nil, errors.New("friend-point partner leader is unavailable")
	}
	hp, attack, magic, mind := game.PartnerDeckStats(deck, view.Cards, view.job(deck.JobType))
	return map[string]any{
		"userid":        view.UserID,
		"name":          view.Name,
		"lv":            view.Level,
		"arthur_type":   view.ArthurType,
		"is_burst":      view.IsBurst,
		"job_type":      deck.JobType,
		"hp":            hp,
		"atkp":          attack,
		"intp":          magic,
		"mndp":          mind,
		"deck_rank":     deck.DeckRank,
		"leader_card":   partnerCardWire(leader),
		"get_fp":        friendPoint,
		"comment":       view.Comment,
		"friend_state":  friendState,
		"rookie_type":   0,
		"deck_name":     deck.Name,
		"rental_idx":    deck.Index,
		"play_log_turn": 0,
		"attr_nums":     view.deckAttributeCounts(deck),
		"kind_nums":     view.deckKindCounts(deck),
		"deck_honorids": append([]int(nil), view.HonorIDs...),
	}, nil
}

func (view friendPointPartnerView) deckWire(deckIndex int8) (map[string]any, error) {
	deck, found := game.ExactDeck(view.Decks, view.ArthurType, deckIndex)
	if !found {
		return nil, errors.New("friend-point partner deck is unavailable")
	}
	return teamBattlePartnerDeckWire(
		view.UserID,
		view.ArthurType,
		deck,
		view.Cards,
		view.job(deck.JobType),
		view.Avatar,
		view.Spheres,
		view.Buddies,
		view.SupportUnlocked,
		view.IsBurst,
		nil,
	)
}

func (view friendPointPartnerView) rentalDeckWires() ([]any, error) {
	selected := make([]game.DeckInfo, 0)
	hasRental := false
	for _, deck := range view.Decks {
		hasRental = hasRental || deck.IsRental != 0
	}
	for _, deck := range view.Decks {
		if deck.ArthurType != view.ArthurType || (hasRental && deck.IsRental == 0) {
			continue
		}
		if _, complete := selectCompletePartnerDeck([]game.DeckInfo{deck}, view.ArthurType, view.Cards); complete {
			selected = append(selected, deck)
		}
	}
	sort.Slice(selected, func(left, right int) bool {
		return selected[left].Index < selected[right].Index
	})
	result := make([]any, 0, len(selected))
	for _, deck := range selected {
		wire, err := view.deckWire(deck.Index)
		if err != nil {
			return nil, err
		}
		result = append(result, wire)
	}
	if len(result) == 0 {
		return nil, errors.New("friend-point partner has no complete rental deck")
	}
	return result, nil
}

func (a *API) friendPointPartnerViews() ([]friendPointPartnerView, error) {
	if a.friendPointAccounts == nil {
		return nil, nil
	}
	relations, err := a.friendPointAccounts.ListFriendPointPartnerRecommendations(a.initialState.User.UserID)
	if err != nil {
		return nil, err
	}
	return a.partnerViewsFromRelations(relations), nil
}

func (a *API) friendPointPartnerViewsForUsers(userIDs []int) ([]friendPointPartnerView, error) {
	if a.friendPointAccounts == nil {
		return nil, nil
	}
	relations, err := a.friendPointAccounts.LoadFriendPointAccountRelations(a.initialState.User.UserID, userIDs)
	if err != nil {
		return nil, err
	}
	return a.partnerViewsFromRelations(relations), nil
}

func (a *API) partnerViewsFromRelations(relations []game.FriendPointAccountRelation) []friendPointPartnerView {
	views := make([]friendPointPartnerView, 0, len(relations))
	for _, relation := range relations {
		if view, ok := friendPointPartnerViewFromState(relation.State); ok {
			// Public account projections omit master fields; resolve attributes from
			// the shared catalog rather than enlarging persisted account summaries.
			view.CardAttributes = a.account.ResolveCardAttributes(view.Cards, view.CardAttributes)
			cardIDs := make([]int, 0, len(view.Cards))
			for _, card := range view.Cards {
				cardIDs = append(cardIDs, card.CardID)
			}
			view.CardKinds = a.multiplayer.PartnerCardKinds(cardIDs)
			view.FriendState = relation.FriendState
			view.System = relation.System
			views = append(views, view)
		}
	}
	sort.Slice(views, func(left, right int) bool {
		return views[left].UserID < views[right].UserID
	})
	return views
}

func (view friendPointPartnerView) deckKindCounts(deck game.DeckInfo) []int {
	counts := make([]int, 8)
	for _, id := range deck.CardUniqueIDs {
		card, ok := view.Cards[id]
		if !ok {
			continue
		}
		if kind, known := view.CardKinds[card.CardID]; known && kind >= 0 && kind < len(counts) {
			counts[kind]++
		}
	}
	return counts
}

func friendPointAccountFriend(relation game.FriendPointAccountRelation) (gamestate.Friend, bool) {
	view, ok := friendPointPartnerViewFromState(relation.State)
	if !ok {
		return gamestate.Friend{}, false
	}
	deck, ok := view.activeDeck()
	if !ok {
		return gamestate.Friend{}, false
	}
	leader, ok := game.PartnerLeaderCard(deck, view.Cards)
	if !ok {
		return gamestate.Friend{}, false
	}
	hp, attack, magic, mind := game.PartnerDeckStats(deck, view.Cards, view.job(deck.JobType))
	return gamestate.Friend{
		InviteID:        view.InviteID,
		UserID:          view.UserID,
		Name:            view.Name,
		ArthurType:      view.ArthurType,
		IsBurst:         view.IsBurst,
		JobType:         deck.JobType,
		Level:           view.Level,
		DeckRank:        deck.DeckRank,
		FriendState:     relation.FriendState,
		LeaderCardID:    leader.CardID,
		LeaderCardLevel: leader.Level,
		LeaderCardFame:  leader.Fame,
		LastLoginTime:   int(relation.State.LastLoginUnix),
		Comment:         view.Comment,
		PVPPoint:        view.PVPPoint,
		DeckHonorIDs:    append([]int(nil), view.HonorIDs...),
		HP:              hp,
		Attack:          attack,
		Magic:           magic,
		Mind:            mind,
	}, true
}

func (a *API) friendPointAccountStates(targetUserIDs []int) (map[int]int8, error) {
	states := make(map[int]int8)
	if a.friendPointAccounts == nil || len(targetUserIDs) == 0 {
		return states, nil
	}
	return a.friendPointAccounts.FriendPointAccountStates(
		a.initialState.User.UserID,
		targetUserIDs,
	)
}

func findFriendPointPartnerView(views []friendPointPartnerView, userID int) (friendPointPartnerView, bool) {
	for _, view := range views {
		if view.UserID == userID {
			return view, true
		}
	}
	return friendPointPartnerView{}, false
}

// Native ATTR indexes: NULL=0, FIRE=1, ICE=2, WIND=3, LIGHT=4, DARK=5.
// Multi-attribute cards count once in each of their attributes; support cards
// and the leader's duplicate display are not additional main-deck slots.
func (view friendPointPartnerView) deckAttributeCounts(deck game.DeckInfo) []int {
	counts := make([]int, 6)
	for _, id := range deck.CardUniqueIDs {
		card, ok := view.Cards[id]
		if !ok {
			continue
		}
		mask, known := view.CardAttributes[card.CardID]
		if !known {
			continue
		}
		if mask == 0 {
			counts[0]++
			continue
		}
		for bit := 0; bit < 5; bit++ {
			if mask&(1<<bit) != 0 {
				counts[bit+1]++
			}
		}
	}
	return counts
}
