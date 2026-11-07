package httpapi

import (
	"bytes"
	"encoding/json"

	"kairisei.local/server/internal/clientbridge"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

type questProgress struct {
	QuestID  int `json:"questid"`
	Progress int `json:"progress"`
}

type pvpReset struct {
	StartTime int `json:"start_time"`
	EndTime   int `json:"end_time"`
}

type notification struct {
	IsStoryMain         int8            `json:"is_story_main"`
	IsStoryMainCN       int8            `json:"is_story_main_cn"`
	IsStorySub          int8            `json:"is_story_sub"`
	IsStoryEvent        int8            `json:"is_story_event"`
	IsBossSolo          int8            `json:"is_boss_solo"`
	IsBossMulti         int8            `json:"is_boss_multi"`
	IsMainQuest         int8            `json:"is_main_quest"`
	PresentNum          int16           `json:"present_num"`
	IsPresentLimit      int8            `json:"is_present_limit"`
	FriendRequestNum    int16           `json:"friend_request_num"`
	FriendGachaNum      int             `json:"friend_gacha_num"`
	Events              []int           `json:"events"`
	UnlockFeatureFlag   int64           `json:"unlock_feature_flag"`
	IsInviteCodeInput   int8            `json:"is_invitecode_input"`
	MissionNewReceive   int8            `json:"mission_new_receive"`
	MissionClearReceive int8            `json:"mission_clear_receive"`
	MissionTotal        int             `json:"mission_total"`
	MissionComplete     int             `json:"mission_complete"`
	IsAvatarShopNew     int8            `json:"is_avatar_shop_new"`
	IsGachaNew          int8            `json:"is_gacha_new"`
	RaceGroupID         int             `json:"race_groupid"`
	IsTradeShopNew      int8            `json:"is_trade_shop_new"`
	QuestProgress       []questProgress `json:"quest_progress"`
	IsTowerNew          int8            `json:"is_tower_new"`
	Challenge           int             `json:"challenge"`
	PVPReset            pvpReset        `json:"pvp_reset"`
	IsAlchemistNew      int8            `json:"is_alchemist_new"`
}

type commonResponse struct {
	ResultCode           int                     `json:"res_code"`
	ResultString         string                  `json:"res_str"`
	Notifications        []notification          `json:"notification"`
	Missions             []gamestate.MissionInfo `json:"missions"`
	Revision             int                     `json:"revision"`
	IsAppUpdate          int8                    `json:"is_appupdate"`
	ResultErrorAction    int                     `json:"res_err_action"`
	ResultDeleteSaveData int                     `json:"res_is_del_savedata"`
}

type popupResponse struct {
	Popup []any `json:"popup"`
}

func newCommonResponse(featureIDs []uint) commonResponse {
	var flags int64
	for _, featureID := range featureIDs {
		if featureID < 63 {
			flags |= int64(1) << featureID
		}
	}
	return commonResponse{
		ResultString: "",
		Notifications: []notification{{
			Events:            []int{},
			UnlockFeatureFlag: flags,
			QuestProgress:     []questProgress{},
			PVPReset:          pvpReset{},
		}},
		Missions: []gamestate.MissionInfo{},
	}
}

func marshalProtocolWithPopups(
	common commonResponse,
	method any,
	popups []gamestate.PopupProfile,
) ([]byte, error) {
	segments := make([][]byte, 0, 3)
	for _, value := range []any{
		common,
		method,
		popupResponse{Popup: wirePopupProfiles(popups)},
	} {
		segment, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment)
	}
	return bytes.Join(segments, []byte{'\n'}), nil
}

func wirePopupProfiles(profiles []gamestate.PopupProfile) []any {
	result := make([]any, len(profiles))
	for index, profile := range profiles {
		result[index] = map[string]any{
			"popupid":     profile.PopupID,
			"popup_type":  profile.PopupType,
			"priority":    profile.Priority,
			"is_spview":   profile.IsSPView,
			"banner_url":  profile.BannerURL,
			"open_url":    profile.OpenURL,
			"title_text":  profile.TitleText,
			"body_text":   profile.BodyText,
			"button_text": profile.ButtonText,
			"destination": profile.Destination,
			"is_system":   profile.IsSystem,
			"item_lineup": append([]any{}, profile.ItemLineup...),
			"gacha":       append([]any{}, profile.Gacha...),
			"mission":     append([]any{}, profile.Mission...),
			"item_icon":   append([]any{}, profile.ItemIcon...),
		}
	}
	return result
}

type wireCardInfo struct {
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
	AlchemistParam []clientbridge.CardParameterResult   `json:"11"`
	SummonsParam   []clientbridge.SummonParameterResult `json:"12"`
	NextLevelEXP   int                                  `json:"13"`
	NowLevelEXP    int                                  `json:"14"`
	AddEXP         int                                  `json:"15"`
	BaseAddPrice   int                                  `json:"16"`
	IsLock         int8                                 `json:"17"`
	CreateTime     int                                  `json:"18"`
	Fame           int                                  `json:"19"`
	Slot           int                                  `json:"20"`
	Passive        []any                                `json:"21"`
	parameters     *clientbridge.CardParameterCatalog
}

// Only the response view acquires modern derived parameters. The account's
// mutable card record and its persisted ordinary attributes remain untouched.
func (card wireCardInfo) WithParameters() (wireCardInfo, error) {
	families, err := card.parameters.Resolve(card.CardID, card.Level, card.Love, card.Fame)
	if err != nil {
		return card, err
	}
	if families.Alchemist != nil {
		card.AlchemistParam = families.Alchemist
	}
	if families.Summons != nil {
		card.SummonsParam = families.Summons
	}
	return card, nil
}

func (card wireCardInfo) MarshalJSON() ([]byte, error) {
	resolved, err := card.WithParameters()
	if err != nil {
		return nil, err
	}
	type fields wireCardInfo
	return json.Marshal(fields(resolved))
}

type wireDeckInfo struct {
	ArthurType           int8    `json:"0"`
	Index                int8    `json:"1"`
	JobType              int8    `json:"2"`
	LeaderCardIndex      int8    `json:"3"`
	CardUniqueIDs        []int64 `json:"4"`
	SupportCardUniqueIDs []int64 `json:"5"`
	SphereUniqueIDs      []int64 `json:"6"`
	BuddyUniqueIDs       []int64 `json:"7"`
	Name                 string  `json:"8"`
	IsActive             int8    `json:"9"`
	IsRental             int8    `json:"10"`
	DeckRank             int8    `json:"11"`
}

type wireSupportSlot struct {
	ArthurType    int8 `json:"0"`
	UnlockSlotNum int8 `json:"1"`
}

type wireAvatarInfo struct {
	CostumeID     int   `json:"0"`
	AvatarPartIDs []int `json:"1"`
}

type wireBuddyInfo struct {
	UniqueID     int64 `json:"0"`
	BuddyID      int   `json:"1"`
	Level        int   `json:"2"`
	Experience   int   `json:"3"`
	NextLevelEXP int   `json:"4"`
	NowLevelEXP  int   `json:"5"`
	AddEXP       int   `json:"6"`
	BaseAddPrice int   `json:"7"`
	IsLock       int8  `json:"8"`
	CreateTime   int   `json:"9"`
}

// CardShowView is the typed response boundary used by client protocol adapters.
type CardShowView = wireCardShow
type CardView = wireCardInfo

type wireCardShow struct {
	Cards             []wireCardInfo      `json:"0"`
	StackCards        []wireCardStackInfo `json:"1"`
	Decks             []wireDeckInfo      `json:"2"`
	Avatars           []wireAvatarInfo    `json:"3"`
	SupportSlots      []wireSupportSlot   `json:"4"`
	CardCollectionNum int                 `json:"5"`
}

type wireCardContainerShow struct {
	Cards []wireCardInfo `json:"0"`
}

type wireCardMove struct {
	UpdatedCards []wireCardInfo `json:"0"`
	UpdatedDecks []wireDeckInfo `json:"1"`
}

type wireCardStackInfo struct {
	CardID        int `json:"0"`
	Num           int `json:"1"`
	HP            int `json:"2"`
	Attack        int `json:"3"`
	Magic         int `json:"4"`
	Mind          int `json:"5"`
	AddExperience int `json:"6"`
	BaseAddPrice  int `json:"7"`
}

type wireDeckSet struct {
	Cards []wireCardInfo `json:"0"`
	Decks []wireDeckInfo `json:"1"`
}

type wireSphereShow struct {
	Spheres []wireSphereInfo `json:"0"`
}

type wireSphereInfo struct {
	UniqueID     int64 `json:"0"`
	SphereID     int   `json:"1"`
	Level        int   `json:"2"`
	Experience   int   `json:"3"`
	NextLevelEXP int   `json:"4"`
	NowLevelEXP  int   `json:"5"`
	AddEXP       int   `json:"6"`
	BaseAddPrice int   `json:"7"`
	IsLock       int8  `json:"8"`
	CreateTime   int   `json:"9"`
}

type wireSphereFusion struct {
	SuccessType    int                 `json:"0"`
	BaseSphere     wireSphereInfo      `json:"1"`
	Gold           int                 `json:"2"`
	SphereNum      int                 `json:"3"`
	Decks          []wireDeckInfo      `json:"4"`
	BackUniqueIDs  []int64             `json:"5"`
	BackStackCards []wireCardStackInfo `json:"6"`
}

type wireSphereEvolution struct {
	BaseSphere wireSphereInfo `json:"0"`
	Gold       int            `json:"1"`
	SphereNum  int            `json:"2"`
	Decks      []wireDeckInfo `json:"3"`
	Reward     any            `json:"4"`
}

type wireSphereSell struct {
	SphereNum int            `json:"0"`
	GetGold   int            `json:"1"`
	Gold      int            `json:"2"`
	Decks     []wireDeckInfo `json:"3"`
}

type wireBuddyShow struct {
	Buddies []wireBuddyInfo `json:"0"`
}

type wireBuddyFusion struct {
	SuccessType    int                 `json:"0"`
	BaseBuddy      wireBuddyInfo       `json:"1"`
	Gold           int                 `json:"2"`
	BuddyNum       int                 `json:"3"`
	Decks          []wireDeckInfo      `json:"4"`
	BackUniqueIDs  []int64             `json:"5"`
	BackStackCards []wireCardStackInfo `json:"6"`
}

type wireBuddyEvolution struct {
	BaseBuddy wireBuddyInfo  `json:"0"`
	Gold      int            `json:"1"`
	BuddyNum  int            `json:"2"`
	Decks     []wireDeckInfo `json:"3"`
	Reward    any            `json:"4"`
}

type wireBuddySell struct {
	BuddyNum int            `json:"0"`
	GetGold  int            `json:"1"`
	Gold     int            `json:"2"`
	Decks    []wireDeckInfo `json:"3"`
}

func toWireBuddy(buddy gamestate.Buddy) wireBuddyInfo {
	return wireBuddyInfo{
		UniqueID: buddy.UniqueID, BuddyID: buddy.BuddyID, Level: buddy.Level,
		Experience: buddy.Experience, NextLevelEXP: buddy.NextLevelExperience,
		NowLevelEXP: buddy.NowLevelExperience, AddEXP: buddy.AddExperience,
		BaseAddPrice: buddy.BaseAddPrice, IsLock: buddy.IsLock, CreateTime: buddy.CreateTime,
	}
}

func toWireBuddies(buddies []gamestate.Buddy) []wireBuddyInfo {
	result := make([]wireBuddyInfo, len(buddies))
	for index, buddy := range buddies {
		result[index] = toWireBuddy(buddy)
	}
	return result
}

func (a *API) toWireCard(card game.CardInfo) wireCardInfo {
	return wireCardInfo{
		UniqueID:       card.UniqueID,
		CreateTime:     card.CreateTime,
		CardID:         card.CardID,
		Level:          card.Level,
		LevelMax:       card.LevelMax,
		Experience:     card.Experience,
		Love:           card.Love,
		SkillLevels:    append([]int16(nil), card.SkillLevels...),
		HP:             card.HP,
		Attack:         card.Attack,
		Magic:          card.Magic,
		Mind:           card.Mind,
		AlchemistParam: []clientbridge.CardParameterResult{},
		SummonsParam:   []clientbridge.SummonParameterResult{},
		NextLevelEXP:   card.NextLevelEXP,
		NowLevelEXP:    card.NowLevelEXP,
		AddEXP:         card.AddExperience,
		BaseAddPrice:   card.BaseAddPrice,
		IsLock:         card.IsLock,
		Fame:           card.Fame,
		Slot:           card.Slot,
		Passive:        []any{},
		parameters:     a.cardParameters,
	}
}

func toWireSphere(sphere gamestate.Sphere) wireSphereInfo {
	return wireSphereInfo{
		UniqueID: sphere.UniqueID, SphereID: sphere.SphereID, Level: sphere.Level,
		Experience: sphere.Experience, NextLevelEXP: sphere.NextLevelExperience,
		NowLevelEXP: sphere.NowLevelExperience, AddEXP: sphere.AddExperience,
		BaseAddPrice: sphere.BaseAddPrice, IsLock: sphere.IsLock, CreateTime: sphere.CreateTime,
	}
}

func toWireSpheres(spheres []gamestate.Sphere) []wireSphereInfo {
	result := make([]wireSphereInfo, len(spheres))
	for index, sphere := range spheres {
		result[index] = toWireSphere(sphere)
	}
	return result
}

func toWireStackCards(cards []gamestate.CardStack) []wireCardStackInfo {
	result := make([]wireCardStackInfo, 0, len(cards))
	for _, card := range cards {
		// Native CardMgr adds every returned row to its selectable inventory.
		// Depleted SQLite rows must not recreate a material after it was used up.
		if card.Num <= 0 {
			continue
		}
		result = append(result, wireCardStackInfo{
			CardID:        card.CardID,
			Num:           card.Num,
			HP:            card.HP,
			Attack:        card.Attack,
			Magic:         card.Magic,
			Mind:          card.Mind,
			AddExperience: card.AddExperience,
			BaseAddPrice:  card.BaseAddPrice,
		})
	}
	return result
}

func (a *API) toWireCards(cards []game.CardInfo) []wireCardInfo {
	result := make([]wireCardInfo, len(cards))
	for index, card := range cards {
		result[index] = a.toWireCard(card)
	}
	return result
}

func toWireDeck(deck game.DeckInfo) wireDeckInfo {
	return wireDeckInfo{
		ArthurType:           deck.ArthurType,
		Index:                deck.Index,
		JobType:              deck.JobType,
		LeaderCardIndex:      deck.LeaderCardIndex,
		CardUniqueIDs:        cloneWireInt64s(deck.CardUniqueIDs),
		SupportCardUniqueIDs: cloneWireInt64s(deck.SupportCardUniqueIDs),
		SphereUniqueIDs:      cloneWireInt64s(deck.SphereUniqueIDs),
		BuddyUniqueIDs:       cloneWireInt64s(deck.BuddyUniqueIDs),
		Name:                 deck.Name,
		IsActive:             deck.IsActive,
		IsRental:             deck.IsRental,
		DeckRank:             deck.DeckRank,
	}
}

// The CN client distinguishes an empty protocol list from JSON null while
// validating whether Deck can leave the scene. Always preserve the wire shape
// as an array, including for an empty source slice.
func cloneWireInt64s(values []int64) []int64 {
	result := make([]int64, len(values))
	copy(result, values)
	return result
}

func toWireDecks(decks []game.DeckInfo) []wireDeckInfo {
	result := make([]wireDeckInfo, len(decks))
	for index, deck := range decks {
		result[index] = toWireDeck(deck)
	}
	return result
}
