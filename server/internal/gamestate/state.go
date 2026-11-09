package gamestate

import (
	"encoding/json"
)

// CardCapacityLimit is the local policy shared by new accounts and expansion.
const CardCapacityLimit = 10000

// LOCAL_POLICY: inventory capacity advertised to the original client.
const CardContainerCapacityDefault = 3000
const SphereCapacityDefault = 500
const BuddyCapacityDefault = 500

// Retain CN tutorial bits 0-25; the added main-quest route guide owns bit 26.
const TutorialFlagCount = 27

type User struct {
	UserID              int            `json:"user_id"`
	InviteID            string         `json:"invite_id"`
	Name                string         `json:"name"`
	Comment             string         `json:"comment"`
	ActiveArthurType    int            `json:"active_arthur_type"`
	LeaderCardUniqueID  int64          `json:"leader_card_unique_id"`
	LeaderCardID        int            `json:"leader_card_id"`
	HomeBackgroundID    int            `json:"home_background_id"`
	NaviID              int8           `json:"navi_id"`
	NaviUnlockFlag      int64          `json:"navi_unlock_flag"`
	SelectableNaviIDs   []int8         `json:"selectable_navi_ids"`
	NaviCatalogIDs      []int8         `json:"-"`
	NaviPurchasePrice   int            `json:"-"`
	ArthurRank          int            `json:"arthur_rank"`
	LastHomeDeckRank    int            `json:"last_home_deck_rank,omitempty"`
	Level               int            `json:"level"`
	Experience          int            `json:"experience"`
	NowLevelExperience  int            `json:"now_level_experience"`
	NextLevelExperience int            `json:"next_level_experience"`
	Jobs                []JobParameter `json:"jobs"`
	AP                  int            `json:"ap"`
	APMax               int            `json:"ap_max"`
	BP                  int            `json:"bp"`
	BPMax               int            `json:"bp_max"`
	CardMax             int            `json:"card_max"`
	CardContainerMax    int            `json:"card_container_max"`
	SphereMax           int            `json:"sphere_max"`
	FollowMax           int            `json:"friend_max"`
	BuddyMax            int            `json:"buddy_max"`
	DeckMaxPerArthur    int            `json:"deck_max_per_arthur"`
	DeckMaxAlchemist    int            `json:"deck_max_alchemist"`
	DeckRank            int8           `json:"deck_rank"`
	PVPPoint            int            `json:"pvp_point"`
	Gold                int            `json:"gold"`
	FriendPoint         int            `json:"fp"`
	Coin                int            `json:"coin"`
	CoinFree            int            `json:"coin_free"`
	TutorialFlag        int64          `json:"tutorial_flag"`
	UnlockedFeatureIDs  []uint         `json:"unlocked_feature_ids"`
}

// JobParameter is the account-owned base status for one client job_type slot.
// The original client indexes UserInfo.jobs directly by DeckInfo.job_type and
// adds these values to the card-derived deck parameters.
type JobParameter struct {
	HP     int `json:"hp"`
	Attack int `json:"atkp"`
	Magic  int `json:"intp"`
	Mind   int `json:"mndp"`
}

// PlayerProgressionPolicy is static local publication policy. It is loaded
// from the versioned CN progression runtime JSON and deliberately excluded
// from mutable account snapshots. Current level, EXP, caps and derived job
// parameters remain in User and are persisted in SQLite.
type PlayerProgressionPolicy struct {
	ConfigVersion int `json:"config_version"`
	MaxLevel      int `json:"max_level"`
	Experience    struct {
		Base         int `json:"base"`
		PerLevel     int `json:"per_level"`
		HigherLevels struct {
			FromLevel int `json:"from_level"`
			Base      int `json:"base"`
			PerLevel  int `json:"per_level"`
		} `json:"higher_levels"`
	} `json:"experience_to_next"`
	BattlePoints struct {
		Base           int `json:"base"`
		LevelsPerPoint int `json:"levels_per_point"`
		Maximum        int `json:"maximum"`
	} `json:"battle_points"`
	Friends struct {
		Base                 int `json:"base"`
		IncreaseFromLevel    int `json:"increase_from_level"`
		Maximum              int `json:"maximum"`
		FollowerDisplayLimit int `json:"follower_display_limit"`
		HelperReward         struct {
			OtherPerPartner  int `json:"other_per_partner"`
			FriendPerPartner int `json:"friend_per_partner"`
			MaximumPartners  int `json:"maximum_partners"`
		} `json:"helper_reward"`
	} `json:"friends"`
	JobParameters struct {
		MaxStatusLevel int            `json:"max_status_level"`
		Maximum        []JobParameter `json:"maximum"`
	} `json:"job_parameters"`
}

type Card struct {
	UniqueID                  int64         `json:"unique_id"`
	CreateTime                int           `json:"create_time,omitempty"`
	CardID                    int           `json:"card_id"`
	Name                      string        `json:"name"`
	SameCardID                int           `json:"same_card_id"`
	SameSupportCardID         int           `json:"same_support_card_id"`
	RarityRank                int           `json:"rarity_rank"`
	RarityPlus                int           `json:"rarity_plus,omitempty"`
	FusionAttributes          uint8         `json:"fusion_attributes,omitempty"`
	Level                     int           `json:"level"`
	LevelMax                  int           `json:"level_max"`
	ExperienceTableID         int           `json:"experience_table_id"`
	Experience                int           `json:"experience"`
	NowLevelExperience        int           `json:"now_level_experience"`
	Love                      int           `json:"love"`
	LoveMax                   int           `json:"love_max,omitempty"`
	PremiumRarity             bool          `json:"premium_rarity,omitempty"`
	SkillLevels               []int16       `json:"skill_levels"`
	SkillLevelMax             int           `json:"skill_level_max"`
	HP                        int           `json:"hp"`
	Attack                    int           `json:"attack"`
	Magic                     int           `json:"magic"`
	Mind                      int           `json:"mind"`
	ParameterInitial          CardParameter `json:"parameter_initial"`
	ParameterMaximum          CardParameter `json:"parameter_maximum"`
	ParameterLoveMaximumBonus CardParameter `json:"parameter_love_maximum_bonus"`
	NextLevelExperience       int           `json:"next_level_experience"`
	AddExperience             int           `json:"add_experience"`
	BaseAddPrice              int           `json:"base_add_price"`
	SellGold                  int           `json:"sell_gold"`
	IsLock                    int8          `json:"is_lock"`
	Fame                      int           `json:"fame"`
	FameMax                   int           `json:"fame_max,omitempty"`
	DevelopmentType           int           `json:"development_type,omitempty"`
	DecomposeRadix            int           `json:"decompose_radix,omitempty"`
	DevelopRadix              int           `json:"develop_radix,omitempty"`
	AcquisitionText           string        `json:"acquisition_text,omitempty"`
}

// CardCategoryProfile and CardGroupProfile are immutable protocol-122 data.
// MemberCardIDs is a runtime-master gate only; the HTTP adapter
// deliberately projects the five fields consumed by the original CN DTO.
type CardCategoryProfile struct {
	CategoryID int    `json:"categoryid"`
	Name       string `json:"name"`
	Order      int    `json:"order"`
	ViewType   int    `json:"view_type"`
}

type CardGroupProfile struct {
	DeckLimitBossID int    `json:"deck_limit_bossid"`
	GroupID         int    `json:"groupid"`
	CategoryID      int    `json:"categoryid"`
	Name            string `json:"name"`
	Order           int    `json:"order"`
	MemberCardIDs   []int  `json:"member_cardids"`
}

type CardGroupPolicy struct {
	ConfigVersion int `json:"config_version"`
}

type CardParameter struct {
	HP     int `json:"hp"`
	Attack int `json:"attack"`
	Magic  int `json:"magic"`
	Mind   int `json:"mind"`
}

type CardProgressionPolicy struct {
	ConfigVersion                     int                       `json:"config_version"`
	FusionGoldPerMaterialPerBaseLevel int                       `json:"fusion_gold_per_material_per_base_level"`
	MaximumCardMaterialCount          int                       `json:"maximum_card_material_count"`
	FusionSuccessTypes                []CardFusionSuccessPolicy `json:"fusion_success_types"`
	FameMaterials                     map[int][5]int            `json:"fame_materials,omitempty"`
	FameNormal                        CardParameter             `json:"fame_normal"`
	FamePremium                       CardParameter             `json:"fame_premium"`
}

type CardFusionSuccessPolicy struct {
	SuccessType        int `json:"success_type"`
	Weight             int `json:"weight"`
	ExperiencePermille int `json:"experience_permille"`
}

type CardStack struct {
	CardID        int `json:"cardid"`
	Num           int `json:"num"`
	HP            int `json:"hp"`
	Attack        int `json:"atkp"`
	Magic         int `json:"intp"`
	Mind          int `json:"mndp"`
	AddExperience int `json:"add_exp"`
	BaseAddPrice  int `json:"base_add_price"`
	MaterialType  int `json:"material_type,omitempty"`
}

// SphereDefinition is immutable master data normalized from the official CN
// sphr.csv. EquipAllowed follows the client's four one-based Arthur slots.
type SphereDefinition struct {
	SphereID              int     `json:"sphrid"`
	SameSphereID          int     `json:"same_sphrid"`
	Name                  string  `json:"name"`
	Text                  string  `json:"text"`
	Type                  string  `json:"type"`
	Rarity                string  `json:"rarity"`
	EvolutionCount        int     `json:"evo_count"`
	MaxLevel              int     `json:"max_level"`
	ExperienceTableID     int     `json:"exp_table_id"`
	EquipAllowed          [4]bool `json:"equip_allowed"`
	SkillID               int     `json:"skill_id"`
	PassiveSkillID        int     `json:"passive_skill_id"`
	CallSkillID           int     `json:"call_skill_id"`
	PictID                int     `json:"pict_id"`
	Count                 int     `json:"count"`
	PlayCondition         string  `json:"play_condition"`
	PlayConditionParam    int     `json:"play_condition_param"`
	WayToUseText          string  `json:"way_to_use_text"`
	SellGold              int     `json:"sell_gold"`
	EvolutionID           int     `json:"evolution_id"`
	MaterialAddExperience int     `json:"material_add_experience"`
	FusionBaseAddPrice    int     `json:"fusion_base_add_price"`
}

// SphereProgressionPolicy contains static server publication policy which is
// not present in the official sphr.csv itself. Mutable Sphere instances stay
// in the account snapshot; the policy is versioned with the runtime master.
type SphereProgressionPolicy struct {
	ConfigVersion                      int `json:"config_version"`
	MaterialLevelBonusPermillePerLevel int `json:"material_level_bonus_permille_per_level"`
}

// Sphere is mutable account-owned inventory. The final five derived fields
// are sent by the server in proto.SphrInfo and are persisted so admin edits
// remain transparent, then normalized against the active master at startup.
type Sphere struct {
	UniqueID            int64 `json:"unique_id"`
	SphereID            int   `json:"sphere_id"`
	Level               int   `json:"level"`
	Experience          int   `json:"experience"`
	NextLevelExperience int   `json:"next_level_experience"`
	NowLevelExperience  int   `json:"now_level_experience"`
	AddExperience       int   `json:"add_experience"`
	BaseAddPrice        int   `json:"base_add_price"`
	IsLock              int8  `json:"is_lock"`
	CreateTime          int   `json:"create_time"`
}

type ExploreFloor struct {
	ExploreFloorID int    `json:"explore_floorid"`
	FloorName      string `json:"floor_name"`
}

type ExploreStage struct {
	ExploreStageID int            `json:"explore_stageid"`
	StageName      string         `json:"stage_name"`
	StageMapID     int            `json:"stage_mapid"`
	StageType      int            `json:"stage_type"`
	StateFlag      int            `json:"state_flag"`
	LimitSeconds   int            `json:"limit_sec"`
	Floors         []ExploreFloor `json:"floors"`
}

type ExploreAvatar struct {
	CostumeID     int   `json:"costumeid"`
	AvatarPartIDs []int `json:"avatar_partsids"`
}

// Avatar is the persisted appearance for one Arthur type. The array position
// in State.Avatars is the CN client contract: Arthur types 1..4 map to indexes
// 0..3 respectively.
type Avatar struct {
	CostumeID     int   `json:"costumeid"`
	AvatarPartIDs []int `json:"avatar_partsids"`
}

// AvatarPartDefinition is immutable presentation/equip metadata normalized
// from the official CN avatar_parts.csv. ArthurMask and SlotMask retain the
// client's bit contracts instead of converting them into local enums.
type AvatarPartDefinition struct {
	PartID     int    `json:"part_id"`
	Name       string `json:"name"`
	ArthurMask int    `json:"arthur_mask"`
	SlotMask   int    `json:"slot_mask"`
	IconPictID int    `json:"icon_pict_id"`
	IsNewList  int8   `json:"is_new_list"`
	SeriesID   int    `json:"series_id"`
}

type AvatarSeriesDefinition struct {
	SeriesID int    `json:"series_id"`
	Name     string `json:"name"`
}

type AvatarCompletionReward struct {
	Type     string `json:"type"`
	Num      int    `json:"num"`
	RewardID int    `json:"reward_id"`
}

type AvatarSeriesCompletion struct {
	CompletionID int                      `json:"completion_id"`
	Name         string                   `json:"name"`
	PartIDs      []int                    `json:"part_ids"`
	Rewards      []AvatarCompletionReward `json:"rewards"`
}

// AvatarShopPolicy is local operating policy. The official client data does
// not contain the historical service lineup price/schedule, so this is kept
// separate from the official definitions and is never described as official.
type AvatarShopPolicy struct {
	PayType      int `json:"pay_type"`
	PayTypeID    int `json:"pay_typeid"`
	Price        int `json:"price"`
	AppearEnd    int `json:"appear_end"`
	NewAppearEnd int `json:"new_appear_end"`
}

type ExploreProgressState struct {
	ActiveRewards      *[]Reward         `json:"active_rewards,omitempty"`
	Stage              ExploreStage      `json:"stage"`
	Stages             []ExploreStage    `json:"stages,omitempty"`
	FloorRarity        int               `json:"floor_rarity"`
	APRecoverySeconds  int               `json:"ap_recovery_seconds"`
	Events             []json.RawMessage `json:"events"`
	Avatar             ExploreAvatar     `json:"avatar"`
	StageCursor        int               `json:"stage_cursor,omitempty"`
	ActiveStageID      int               `json:"active_stageid,omitempty"`
	Active             bool              `json:"active,omitempty"`
	ArthurType         int8              `json:"arthur_type,omitempty"`
	DeckIndex          int8              `json:"deck_idx,omitempty"`
	StartedAtUnix      int64             `json:"started_at_unix,omitempty"`
	APNextRecoveryUnix int64             `json:"ap_next_recovery_unix,omitempty"`
}

type CardStackUse struct {
	CardID int `json:"cardid"`
	Num    int `json:"num"`
}

type EvolutionTransition struct {
	FromCardID int                 `json:"from_cardid"`
	ToCardID   int                 `json:"to_cardid"`
	Type       int                 `json:"evolution_type"`
	Gold       int                 `json:"gold"`
	KeepLevel  bool                `json:"keep_level"`
	Materials  []EvolutionMaterial `json:"materials"`
}

type EvolutionMaterial struct {
	CardID int `json:"cardid"`
	Num    int `json:"num"`
	Fame   int `json:"fame"`
}

// CardDevelopmentPolicy contains values that the original client expects the
// server to supply. They are kept in an explicit local profile because the
// official client and bundled card table do not encode the service schedule.
type CardDevelopmentPolicy struct {
	TimeEveryFameSeconds int    `json:"time_every_fame_seconds"`
	CoinEveryHour        int    `json:"coin_every_hour"`
	HelpPath             string `json:"help_path"`
}

type CardFameTraining struct {
	UniqueID    int64 `json:"unique_id"`
	BeginAtUnix int64 `json:"begin_at_unix"`
	Fame        int   `json:"fame"`
}

type CardDevelopmentState struct {
	Stive    int               `json:"stive"`
	Training *CardFameTraining `json:"training,omitempty"`
}

// EventPageProfile is immutable local publication policy for the original
// client's Home event sub-button and EventPage scene. The DTO contract and
// referenced presentation assets come from the official CN client; the
// schedule and copy are explicitly local because service topology is absent.
type EventPageButton struct {
	Type   int    `json:"type"`
	PictID int    `json:"pictid"`
	MoveTo string `json:"moveto"`
	IsNew  int8   `json:"is_new"`
}

type EventPageProfile struct {
	EventID          int               `json:"eventid"`
	BackgroundPictID int               `json:"bg_pictid"`
	HomeButtonPictID int               `json:"home_button_pictid"`
	EndTime          int               `json:"end_time"`
	InfoURL          string            `json:"info_url"`
	UpdateInfo       string            `json:"update_info"`
	ItemIDs          []int             `json:"itemid"`
	Buttons          []EventPageButton `json:"buttons"`
	IsNewSolo        int8              `json:"is_new_solo"`
	IsNewMulti       int8              `json:"is_new_multi"`
}

// PopupProfile is immutable local publication policy for the original CN
// client's third protocol segment. The wire fields and native information
// popup type come from the official client; the copy and publication ID are
// explicitly local because historical service operation data is unavailable.
type PopupProfile struct {
	PopupID     int    `json:"popupid"`
	PopupType   int    `json:"popup_type"`
	Priority    int    `json:"priority"`
	IsSPView    int8   `json:"is_spview"`
	BannerURL   string `json:"banner_url"`
	OpenURL     string `json:"open_url"`
	TitleText   string `json:"title_text"`
	BodyText    string `json:"body_text"`
	ButtonText  string `json:"button_text"`
	Destination string `json:"destination"`
	IsSystem    int8   `json:"is_system"`
	ItemLineup  []any  `json:"item_lineup"`
	Gacha       []any  `json:"gacha"`
	Mission     []any  `json:"mission"`
	ItemIcon    []any  `json:"item_icon"`
}

// SupportDeckSlotUnlockRule is immutable official CN client master data. SlotIndex
// is one-based in deck_support_slot_unlock.csv; the client exposes exactly one
// additional slot per row, in order.
type SupportDeckSlotUnlockRule struct {
	SlotIndex         int `json:"slot_index"`
	Level             int `json:"level"`
	CardCollectionNum int `json:"card_collection_num"`
	Gold              int `json:"gold"`
}

// SupportDeckState is account-owned progression. UnlockSlotNums indexes Arthur
// types 1..4; CardCollectionIDs is the durable discovery history used by the
// client's CardCollectionShow.find_card_num gate.
type SupportDeckState struct {
	UnlockSlotNums           []int8 `json:"unlock_slot_nums"`
	CardCollectionIDs        []int  `json:"card_collection_ids"`
	CardCollectionLoveMaxIDs []int  `json:"card_collection_love_max_ids,omitempty"`
}

type CardActionState struct {
	StackCards           []CardStackUse        `json:"stack_cards"`
	MaxLevelCardIDs      []int                 `json:"max_level_cardids"`
	FusionGoldPerCard    int                   `json:"fusion_gold_per_card"`
	EvolutionTransitions []EvolutionTransition `json:"evolution_transitions"`
}

// CardSellExtractPolicy is static master data for the JP sale reward. BaseCardIDs
// comes from the client's evolution index and BaseRarities from its card table;
// absence of a source card mapping means no extract reward.
type CardSellExtractPolicy struct {
	ItemID            int         `json:"item_id"`
	CountByBaseRarity map[int]int `json:"count_by_base_rarity"`
	BaseCardIDs       map[int]int `json:"base_card_ids"`
	BaseRarities      map[int]int `json:"base_rarities"`
}

type Deck struct {
	ArthurType           int8    `json:"arthur_type"`
	Index                int8    `json:"index"`
	JobType              int8    `json:"job_type"`
	LeaderCardIndex      int8    `json:"leader_card_index"`
	CardUniqueIDs        []int64 `json:"card_unique_ids"`
	SupportCardUniqueIDs []int64 `json:"support_card_unique_ids"`
	SphereUniqueIDs      []int64 `json:"sphere_unique_ids"`
	BuddyUniqueIDs       []int64 `json:"buddy_unique_ids"`
	Name                 string  `json:"name"`
	IsActive             int8    `json:"is_active"`
	IsRental             int8    `json:"is_rental"`
	DeckRank             int8    `json:"deck_rank"`
}

type Buddy struct {
	UniqueID            int64 `json:"unique_id"`
	BuddyID             int   `json:"buddy_id"`
	Level               int   `json:"level"`
	Experience          int   `json:"experience"`
	NextLevelExperience int   `json:"next_level_experience"`
	NowLevelExperience  int   `json:"now_level_experience"`
	AddExperience       int   `json:"add_experience"`
	BaseAddPrice        int   `json:"base_add_price"`
	IsLock              int8  `json:"is_lock"`
	CreateTime          int   `json:"create_time"`
}

// BuddyDefinition is immutable master data normalized from the official CN
// buddy.csv. Buddy is the separate mutable, per-account inventory instance.
type BuddyDefinition struct {
	BuddyID               int    `json:"buddyid"`
	SameBuddyID           int    `json:"same_buddyid"`
	Name                  string `json:"name"`
	Rarity                string `json:"rarity"`
	EvolutionCount        int    `json:"evolution_count"`
	MaxLevel              int    `json:"max_level"`
	PictID                int    `json:"pict_id"`
	ExperienceTableID     int    `json:"experience_table_id"`
	SellGold              int    `json:"sell_gold"`
	EvolutionID           int    `json:"evolution_id"`
	OverlimitItemID       int    `json:"overlimit_item_id"`
	OverlimitItemNum      int    `json:"overlimit_item_num"`
	MaterialAddExperience int    `json:"material_add_experience"`
	FusionBaseAddPrice    int    `json:"fusion_base_add_price"`
}

// BuddyProgressionPolicy contains server-published Buddy fusion and evolution
// policy which is absent from the official static Buddy rows. The managed CN
// client directly consumes add_exp/base_add_price from DTOs; mutable ownership
// and progression remain in the account snapshot.
type BuddyProgressionPolicy struct {
	ConfigVersion        int `json:"config_version"`
	MaximumMaterialCount int `json:"maximum_material_count"`
}

type Friend struct {
	InviteID        string `json:"inviteid"`
	UserID          int    `json:"userid"`
	Name            string `json:"name"`
	ArthurType      int8   `json:"arthur_type"`
	IsBurst         int8   `json:"is_burst"`
	JobType         int8   `json:"job_type"`
	Level           int    `json:"lv"`
	DeckRank        int8   `json:"deck_rank"`
	FriendState     int8   `json:"state"`
	LeaderCardID    int    `json:"leader_cardid"`
	LeaderCardLevel int    `json:"leader_card_lv"`
	LeaderCardFame  int    `json:"leader_card_fame"`
	LastLoginTime   int    `json:"last_login_time"`
	Comment         string `json:"comment"`
	PVPPoint        int    `json:"pvp_point"`
	IsFirstMatching int8   `json:"is_first_matching"`
	IsRookieBonus   int8   `json:"is_rookie_bonus"`
	DeckHonorIDs    []int  `json:"deck_honorids"`
	HP              int    `json:"hp"`
	Attack          int    `json:"atkp"`
	Magic           int    `json:"intp"`
	Mind            int    `json:"mndp"`
}

type FriendCollectionState struct {
	Users []Friend `json:"users"`
}

type StampCollectionState struct {
	StampIDs     []int `json:"stampids"`
	DeckStampIDs []int `json:"deck_stampids"`
}

type HonorCollectionState struct {
	DeckHonorIDs []int `json:"deck_honorids"`
	HonorIDs     []int `json:"honorids"`
}

type StoryMain struct {
	StoryMainID    int    `json:"story_mainid"`
	StateFlag      int    `json:"state_flag"`
	Title          string `json:"title"`
	UnlockText     string `json:"unlock_text"`
	RewardTypeFlag int    `json:"reward_type_flag"`
}

type StoryMainSection struct {
	StoryMainSectionID int         `json:"story_main_sectionid"`
	SectionTitle       string      `json:"section_title"`
	Stories            []StoryMain `json:"stories"`
}

type StoryMainPart struct {
	StoryMainPartID int                `json:"story_main_partid"`
	Name            string             `json:"name"`
	Sections        []StoryMainSection `json:"sections"`
}

type StorySub struct {
	StorySubID    int    `json:"story_subid"`
	StateFlag     int    `json:"state_flag"`
	Title         string `json:"title"`
	UnlockText    string `json:"unlock_text"`
	FeatureReward Reward `json:"feature_reward"`
}

type StorySubSection struct {
	StorySubSectionID   int        `json:"story_sub_sectionid"`
	StorySubSectionType int        `json:"story_sub_section_type"`
	SectionTitle        string     `json:"section_title"`
	PictID              int        `json:"pictid"`
	Stories             []StorySub `json:"stories"`
}

type StorySubCharacter struct {
	StoryMainCharacterID int               `json:"story_main_charaid"`
	Name                 string            `json:"name"`
	PictID               int               `json:"pictid"`
	Sections             []StorySubSection `json:"sections"`
}

type StoryUnlockMaterial struct {
	PayType   int `json:"pay_type"`
	PayTypeID int `json:"pay_typeid"`
	Price     int `json:"price"`
}

type StorySubEvent struct {
	Sub       StorySub              `json:"sub"`
	Materials []StoryUnlockMaterial `json:"materials"`
}

type StoryEvent struct {
	StoryEventID int             `json:"story_eventid"`
	Name         string          `json:"name"`
	PictID       int             `json:"pictid"`
	Stories      []StorySubEvent `json:"stories"`
}

// StoryRewardPolicy is the local first-clear economy published with the
// normalized story catalog. The supplied CN assets prove the DTO and story
// topology but do not contain the retired service reward schedule, so these
// values remain explicit operation policy rather than account state.
type StoryRewardPolicy struct {
	ConfigVersion   int    `json:"config_version"`
	State           string `json:"state"`
	MainFirstClear  Reward `json:"main_first_clear"`
	SubFirstClear   Reward `json:"sub_first_clear"`
	EventFirstClear Reward `json:"event_first_clear"`
}

type StoryBattleReference struct {
	StoryKind      string `json:"story_kind"`
	StoryID        int    `json:"story_id"`
	StoryBattleIDs []int  `json:"story_battle_ids"`
}

type StoryCatalogState struct {
	QuestClearedIDs       []int                  `json:"quest_cleared_ids,omitempty"`
	MainParts             []StoryMainPart        `json:"main_parts"`
	CNMainParts           []StoryMainPart        `json:"cn_main_parts,omitempty"`
	SubCharacters         []StorySubCharacter    `json:"sub_characters"`
	Events                []StoryEvent           `json:"events"`
	StoryBattleIDs        []int                  `json:"-"`
	StoryBattleReferences []StoryBattleReference `json:"-"`
}

// Reward is the named reward object consumed by the CN 6.0.2 client. Keep all
// fields present even for non-card rewards: ProtoGen reads every scalar and
// treats card_skill_lv as an optional array.
type Reward struct {
	Type            int     `json:"type"`
	Num             int     `json:"num"`
	RewardTypeID    int     `json:"reward_typeid"`
	CardLevel       int16   `json:"card_lv"`
	CardFame        int16   `json:"card_fame"`
	CardLove        int     `json:"card_love"`
	CardSkillLevels []int16 `json:"card_skill_lv"`
}

// LoginBonusPolicy is static local operation policy. The original CN client
// proves the HomeShow DTO and popup flow, while the retired service schedule is
// absent from the supplied data; configured rewards therefore remain explicit
// local policy rather than player-save data.
type LoginBonusPolicy struct {
	ConfigVersion            int             `json:"config_version"`
	DayBoundaryOffsetMinutes int             `json:"day_boundary_offset_minutes"`
	Cycle                    []LoginBonusDay `json:"cycle"`
	Beginner                 []LoginBonusDay `json:"beginner"`
	TotalMilestones          []LoginBonusDay `json:"total_milestones"`
}

type LoginBonusDay struct {
	Day     int    `json:"day"`
	Comment string `json:"comment"`
	Reward  Reward `json:"reward"`
}

// LoginBonusState is the per-account dynamic cursor. LastClaimDay is the
// YYYY-MM-DD calendar key in the configured fixed offset, not host-local time.
type LoginBonusState struct {
	ConfigVersion int    `json:"config_version"`
	LastClaimDay  string `json:"last_claim_day"`
	CycleDay      int    `json:"cycle_day"`
	BeginnerDay   int    `json:"beginner_day"`
	TotalClaims   int    `json:"total_claims"`
}

type MissionInfo struct {
	MissionID      int      `json:"missionid"`
	TabType        int      `json:"tab_type"`
	ViewPriority   int      `json:"view_prio"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Rewards        []Reward `json:"rewards"`
	State          int      `json:"state"`
	ProgressShow   int      `json:"progress_show_type"`
	ProgressNow    int      `json:"progress_now"`
	ProgressMax    int      `json:"progress_max"`
	ClearLimitTime int      `json:"clear_limit_time"`
	OpenURL        string   `json:"open_url"`
}

// MissionDefinition is shared publication data. It never enters account SQL
// metadata; MissionInfo is projected from this definition and private progress.
type MissionDefinition struct {
	SeriesID       int          `json:"series_id,omitempty"`
	StartsAt       int64        `json:"starts_at,omitempty"`
	EndsAt         int64        `json:"ends_at,omitempty"`
	MissionID      int          `json:"missionid"`
	Published      bool         `json:"published"`
	PrerequisiteID int          `json:"prerequisite_id,omitempty"`
	TabType        int          `json:"tab_type"`
	ViewPriority   int          `json:"view_prio"`
	Title          string       `json:"title"`
	Description    string       `json:"description"`
	Rewards        []Reward     `json:"rewards"`
	ProgressShow   int          `json:"progress_show_type"`
	ProgressMax    int          `json:"progress_max"`
	OpenURL        string       `json:"open_url"`
	Rule           *MissionRule `json:"rule,omitempty"`
	Daily          bool         `json:"daily,omitempty"`
}

// A rule is explicit publication data, never inferred from a title or ID.
// Target is separate from ProgressMax, whose zero value is valid display data.
type MissionEventKind string

const (
	MissionCardFusion   MissionEventKind = "card_fusion"
	MissionExploreClear MissionEventKind = "explore_clear"
	MissionQuestClear   MissionEventKind = "quest_clear"
)

type MissionQuestScope string

const (
	MissionQuestAny         MissionQuestScope = ""
	MissionQuestSolo        MissionQuestScope = "solo"
	MissionQuestMultiplayer MissionQuestScope = "multiplayer"
	MissionQuestHost        MissionQuestScope = "host"
	MissionQuestAlchemist   MissionQuestScope = "alchemist"
)

type MissionRule struct {
	Kind         MissionRuleKind   `json:"kind,omitempty"`
	Event        MissionEventKind  `json:"event"`
	Target       int               `json:"target"`
	QuestScope   MissionQuestScope `json:"quest_scope,omitempty"`
	Metric       MissionMetric     `json:"metric,omitempty"`
	SubjectID    int               `json:"subject_id,omitempty"`
	Targets      []int             `json:"targets,omitempty"`
	Minimum      int               `json:"minimum,omitempty"`
	BossIDs      []int             `json:"boss_ids,omitempty"`
	ArthurType   int8              `json:"arthur_type,omitempty"`
	OwnDecksOnly bool              `json:"own_decks_only,omitempty"`
	MinPlayers   int               `json:"min_players,omitempty"`
	ScoreGrade   int               `json:"score_grade,omitempty"`
	IncludeSkip  bool              `json:"include_skip,omitempty"`
}

type MissionProgress struct {
	MissionID      int   `json:"missionid"`
	State          int   `json:"state"`
	ProgressNow    int   `json:"progress_now"`
	ClearLimitTime int   `json:"clear_limit_time"`
	MatchedTargets []int `json:"matched_targets,omitempty"`
}

// Mission contains only durable progress. Keep the existing info object shape;
// old cached display fields and preallocated reward mail have no runtime owner.
type Mission struct {
	Info MissionProgress `json:"info"`
}

// Present mirrors proto.PresentBoxInfo. reward0..reward2 are intentionally
// concrete objects because the CN parser dereferences all three unconditionally.
// PresentStateAdminDeleted is a local history tombstone, never sent to clients.
const PresentStateAdminDeleted int8 = -1

type Present struct {
	PresentID      int64  `json:"presentid"`
	IssuedAtUnix   int64  `json:"issued_at_unix,omitempty"` // persistence only; client receives add_elapsed_sec
	AddElapsedSec  uint32 `json:"add_elapsed_sec"`
	LimitTime      uint32 `json:"limit_tm"`
	Reward         Reward `json:"reward"`
	Reward0        Reward `json:"reward0"`
	Reward1        Reward `json:"reward1"`
	Reward2        Reward `json:"reward2"`
	Comment        string `json:"comment"`
	Reason         int    `json:"reason"`
	State          int8   `json:"state"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	AutoFusionUsed int8   `json:"auto_fusion_used"`
	AutoLoveupUsed int8   `json:"auto_loveup_used"`
	// AdminIdempotencyKey is local persistence metadata. It is never projected
	// into the CN client DTO; it lets loopback-only GM mail retries resolve to
	// the already-created present without adding a second reward.
	AdminIdempotencyKey string `json:"admin_idempotency_key,omitempty"`
}

// Deleted gifts are kept as a bounded history. Once it exceeds
// PresentHistoryLimit the oldest PresentHistoryTrim entries are dropped in one
// step (any older excess too), so trimming runs every 50 deletions, not each one.
const (
	PresentHistoryLimit = 100
	PresentHistoryTrim  = 50
)

// TrimPresentHistories returns histories (oldest first) within the bound. The
// newest entries always remain, so a retried deletion still finds its row.
func TrimPresentHistories(histories []Present) []Present {
	if len(histories) <= PresentHistoryLimit {
		return histories
	}
	drop := max(PresentHistoryTrim, len(histories)-PresentHistoryLimit)
	return append([]Present(nil), histories[drop:]...)
}

type EngagementState struct {
	Visits       MissionVisits `json:"visits,omitempty"`
	Initialized  bool          `json:"initialized"`
	Missions     []Mission     `json:"missions"`
	Presents     []Present     `json:"presents"`
	Histories    []Present     `json:"histories"`
	PopupReadIDs []int         `json:"popup_read_ids,omitempty"`
}

type OptionState struct {
	GameEnableFlag int `json:"game_enable_flag"`
	PushEnableFlag int `json:"push_enable_flag"`
}

// ItemDefinition is normalized from the complete official CN item.csv. It is
// immutable client master data and is deliberately excluded from the player
// save; the local shop catalog references it by ItemID.
type ItemDefinition struct {
	ItemID        int    `json:"item_id"`
	Name          string `json:"name"`
	PictID        int    `json:"pict_id"`
	ItemType      string `json:"item_type"`
	Function      string `json:"function"`
	FunctionValue int    `json:"function_value"`
	LoveUpPrice   int    `json:"love_up_price"`
	MaxOwned      int    `json:"max_owned"`
	DailyLimited  int8   `json:"daily_limited"`
	Description   string `json:"description"`
}

type Item struct {
	ItemID    int `json:"itemid"`
	Num       int `json:"num"`
	LimitTime int `json:"limit_time"`
}

// UserBuffProfile is the immutable key cost and duration consumed by the
// original TeamSlSt boss-unlock flow. Boss publication owns the separate
// user_buff_id references; this profile deliberately does not invent them.
type UserBuffProfile struct {
	UserBuffID         int    `json:"user_buff_id"`
	ItemID             int    `json:"item_id"`
	RequiredNum        int    `json:"required_num"`
	NotEnoughErrorText string `json:"not_enough_error_text"`
	DurationSeconds    int    `json:"duration_seconds"`
}

type ItemShopInterior struct {
	BuyType   int `json:"buy_type"`
	BuyTypeID int `json:"buy_typeid"`
	Num       int `json:"num"`
}

type ItemShopLineup struct {
	LineupID    int                `json:"item_shop_lineupid"`
	PictID      int                `json:"pictid"`
	LineupName  string             `json:"lineup_name"`
	PayType     int                `json:"pay_type"`
	PayTypeID   int                `json:"pay_typeid"`
	Price       int                `json:"price"`
	StockNum    int                `json:"stock_num"`
	StockRemain int                `json:"stock_remain_num"`
	StockType   int8               `json:"stock_type"`
	AppearEnd   int                `json:"appear_end"`
	Note        string             `json:"note"`
	BuyNumMax   int                `json:"buy_num_max"`
	Interiors   []ItemShopInterior `json:"interiors"`
	Hidden      bool               `json:"hidden,omitempty"`
	Disabled    bool               `json:"disabled,omitempty"`
}

type ItemShopTab struct {
	TabType int              `json:"tab_type"`
	Lineup  []ItemShopLineup `json:"lineup"`
}

// Calendar counters remain independent of the currently selected shop limit.
type ItemShopPeriodCounts struct {
	Day        string `json:"day"`
	DayCount   int    `json:"day_count"`
	Week       string `json:"week"`
	WeekCount  int    `json:"week_count"`
	Month      string `json:"month"`
	MonthCount int    `json:"month_count"`
}

// GachaProfile is the local player-save configuration consumed by the
// official CN gacha scene. Card IDs refer only to the normalized official CN
// card master. BannerKey selects an explicitly published loopback presentation
// asset; the empty value retains the official cached default banner.
type GachaProfile struct {
	GachaID        int    `json:"gachaid"`
	Name           string `json:"gacha_name"`
	BuyMessage     string `json:"buymsg"`
	OddsMessage    string `json:"odds_msg,omitempty"`
	SubMessage     string `json:"submsg"`
	CategoryNum    int    `json:"category_num"`
	CategoryPictID int    `json:"category_pictid"`
	OrderNum       int    `json:"order_num"`
	GroupID        int    `json:"groupid"`
	GachaType      int8   `json:"gacha_type"`
	ArthurType     int8   `json:"arthur_type"`
	PayType        int    `json:"pay_type"`
	PayTypeID      int    `json:"pay_typeid"`
	Price          int    `json:"price"`
	CardNum        int    `json:"card_num"`
	CardNumMax     int    `json:"card_num_max"`
	UserSelectMax  int    `json:"user_select_max,omitempty"`
	DailyFirstFree bool   `json:"daily_first_free,omitempty"`

	DailyFirstAvailable bool   `json:"-"`
	EndTime             int    `json:"end_time"`
	PlayCount           int    `json:"play_count"`
	PlayCountMax        int    `json:"play_count_max,omitempty"`
	GroupPlayCount      int    `json:"-"` // Derived from successful plays across the publication group.
	DailyPlayMax        int    `json:"play_count_1day_max,omitempty"`
	DailyGroupPlays     int    `json:"-"`
	BannerKey           string `json:"banner_key,omitempty"`
	PublicationKey      string `json:"publication_key,omitempty"`

	GuaranteedRarityRank int `json:"guaranteed_rarity_rank,omitempty"`
	GuaranteedCount      int `json:"guaranteed_count,omitempty"`
	RemainderRarityRank  int `json:"remainder_rarity_rank,omitempty"`

	CardIDs     []int            `json:"cardids"`
	CardWeights []int            `json:"card_weights"`
	RewardPool  []WeightedReward `json:"reward_pool,omitempty"`
	Steps       []GachaStep      `json:"steps,omitempty"`
	Gifts       []GachaGiftRule  `json:"gift_rules,omitempty"`
	UnownedOnly bool             `json:"unowned_only,omitempty"`
	// CoverPath is an operator-uploaded cover suffix such as "gacha-covers/<sha256>.png" (LOCAL_POLICY); the
	// client URL is built from the configured cover base. Empty keeps the banner resolved from BannerKey.
	CoverPath string `json:"cover_path,omitempty"`
	// CardFames optionally sets the fame of a drawn card (card ID → fame, LOCAL_POLICY); missing means fame 1.
	// It is server-side only: client gacha DTOs are built field by field.
	CardFames map[int]int `json:"card_fames,omitempty"`

	RandomGifts []GachaRandomGiftRule `json:"random_gifts,omitempty"`
}

// WeightedReward is shared by banner draws and inventory gift boxes.
type WeightedReward struct {
	Reward Reward `json:"reward"`
	Weight int    `json:"weight"`
}

// Steps advance only on a committed draw; the final step repeats.
type GachaStep struct {
	PayType    int              `json:"pay_type,omitempty"` // Zero inherits the draw method's payment.
	PayTypeID  int              `json:"pay_typeid,omitempty"`
	Price      int              `json:"price"`
	RewardPool []WeightedReward `json:"reward_pool"`
}

// Gift bounds use one-based successful play counts; zero ToPlay is unlimited.
type GachaGiftRule struct {
	FromPlay int      `json:"from_play"`
	ToPlay   int      `json:"to_play"`
	Rewards  []Reward `json:"rewards"`
}

// A matching rule rolls once per successful draw request. EmptyWeight is an
// optional outcome that grants nothing.
type GachaRandomGiftRule struct {
	FromPlay    int              `json:"from_play"`
	ToPlay      int              `json:"to_play,omitempty"`
	Pool        []WeightedReward `json:"pool"`
	EmptyWeight int              `json:"empty_weight,omitempty"`
}

// GachaSelection is account-owned state for the original client's optional
// user-select gacha flow. Candidate pools remain in GachaProfile; only the
// exact rewards submitted on the last successful play are persisted here.
type GachaSelection struct {
	GachaID int      `json:"gachaid"`
	Rewards []Reward `json:"rewards"`
}

// GachaDailyClaim keeps one business day's history per draw method. A legacy
// record without PlayCount represents one successful free draw.
type GachaDailyClaim struct {
	GachaID     int    `json:"gachaid"`
	Day         string `json:"day"`
	PlayCount   int    `json:"play_count,omitempty"`
	FreeClaimed bool   `json:"free_claimed,omitempty"`
}

// ItemGachaProfile is the static reward mapping used by the
// original client's inventory GACHA/GACHA_EXEC item flow. It is separate from
// GachaProfile because these items do not consume crystals or gacha tickets.
type ItemGachaProfile struct {
	ItemID        int              `json:"item_id"`
	FunctionValue int              `json:"function_value"`
	Rewards       []Reward         `json:"rewards"`
	RewardPool    []WeightedReward `json:"reward_pool,omitempty"`
}

// ItemExchangeProfile is immutable publication metadata for the original
// inventory exchange picker. The official item/card identities may constrain
// a local policy, but service-owned event IDs and costs remain explicitly
// versioned outside handlers.
type ItemExchangeProfile struct {
	ItemID        int    `json:"item_id"`
	IsAppearEvent int8   `json:"is_appear_event"`
	EventID       int    `json:"eventid"`
	NeedNum       int    `json:"need_num"`
	Reward        Reward `json:"reward"`
}

// ItemLackTipProfile is immutable text shown by the original shortage dialog.
// Navigation entries remain explicit because the client executes their URL
// through CommandExecutor and can otherwise leave the local network boundary.
type ItemLackTipProfile struct {
	Index       int               `json:"idx"`
	Title       string            `json:"title"`
	Description string            `json:"desc"`
	Way         string            `json:"way"`
	TextURLs    []ItemLackTipLink `json:"txt_url_info"`
}

type ItemLackTipLink struct {
	Text string `json:"txt"`
	URL  string `json:"url"`
}

// EventShopProfile is service-owned publication data for the original
// WebView event shop. The CN package contains the bridge and DTOs but no
// lineup/reward table, so an empty profile set is the normal dormant state.
type EventShopProfile struct {
	EventID     int                      `json:"eventid"`
	PointItemID int                      `json:"point_itemid"`
	Lineups     []EventShopLineupProfile `json:"lineups"`
}

type EventShopLineupProfile struct {
	LineupID  int    `json:"event_shop_lineupid"`
	Name      string `json:"lineup_name"`
	ImageURL  string `json:"image_url"`
	Price     int    `json:"price"`
	StockNum  int    `json:"stock_num"`
	StockType int8   `json:"stock_type"`
	IsFeature int8   `json:"is_feature"`
	Reward    Reward `json:"reward"`
}

// EventShopPurchase is account-owned stock consumption. Publication and
// rewards remain in EventShopProfile; only successful purchase counts persist.
type EventShopPurchase struct {
	LineupID int `json:"event_shop_lineupid"`
	Count    int `json:"count"`
}

// TradeShopProfile is the static catalog consumed by the original client's
// CardExchange scene.  Point ownership and remaining stock are projected from
// account state at request time.
type TradeShopProfile struct {
	Disabled    bool   `json:"disabled,omitempty"`
	TradeShopID int    `json:"trade_shopid"`
	Name        string `json:"name"`
	Text        string `json:"text"`
	ShopType    int    `json:"shop_type"`
	TabType     int    `json:"tab_type"`
	EndTime     int    `json:"end_time"`
	// StartTime is an optional LOCAL_POLICY opening time checked per request; it is not sent to the client.
	StartTime int                      `json:"start_time,omitempty"`
	IsNew     int                      `json:"is_new"`
	PictID    int                      `json:"pictid"`
	Lineups   []TradeShopLineupProfile `json:"lineups"`
}

type TradeShopLineupProfile struct {
	Disabled    bool                    `json:"disabled,omitempty"`
	LineupID    int                     `json:"lineupid"`
	LineupName  string                  `json:"lineup_name"`
	StockNum    int                     `json:"stock_num"`
	IsLineupNew int8                    `json:"is_lineup_new"`
	IsLineupOld int8                    `json:"is_lineup_old"`
	PictID      int                     `json:"pictid"`
	Prices      []TradeShopPointProfile `json:"prices"`
	Rewards     []Reward                `json:"rewards"`
}

type TradeShopPointProfile struct {
	Type               int                           `json:"type"`
	ID                 int                           `json:"id"`
	Num                int                           `json:"num"`
	PointCardCondition []TradeShopPointCardCondition `json:"point_card_condition"`
}

type TradeShopPointCardCondition struct {
	RarityPlus int `json:"rarity_plus"`
}

type TradeShopPurchase struct {
	LineupID int `json:"lineupid"`
	Count    int `json:"count"`
}

// TeamBattleReplayBattle is one original-client solo battle segment. The
// client allocates its input-command and enemy-dead-bit report arrays from the
// number of entries returned in TeamBattleSoloStart.battles and advances
// enemy_partyid by battle_idx.
type TeamBattleReplayBattle struct {
	EnemyPartyID int  `json:"enemy_party_id"`
	EnemyType    int8 `json:"enemy_type"`
}

type TeamBattleReplay struct {
	BossID       int  `json:"boss_id"`
	EnemyPartyID int  `json:"enemy_party_id"`
	EnemyType    int8 `json:"enemy_type"`
	// Battles is the active multi-segment contract. An empty slice retains the
	// historical one-segment EnemyPartyID/EnemyType projection for old SQLite
	// snapshots and generated event profiles.
	Battles           []TeamBattleReplayBattle `json:"battles,omitempty"`
	Seed              int                      `json:"seed"`
	CostInitial       int                      `json:"cost_initial"`
	BurstGaugeInitial int                      `json:"burst_gauge_initial"`
	HoldMax           int                      `json:"hold_max"`
	EndTurn           int                      `json:"end_turn"`
}

// TeamBattleRecommendation is static publication metadata consumed by the
// original client's automatic-deck picker. RecommendIDs contains one official
// deck_auto_allotment ID per Arthur type; zero asks the client to use its
// package-resident per-Arthur default.
type TeamBattleRecommendation struct {
	BossID       int    `json:"bossid"`
	Name         string `json:"name"`
	Attr         int    `json:"attr"`
	RecommendIDs []int  `json:"recommendid"`
}

// TeamBattleRewardProfile keeps local settlement policy outside the HTTP
// adapter. StageQuestAreaID/StageQuestStageID are zero for the standalone
// training entry and identify the route when the same official boss replay is
// entered through StageQuest.
type TeamBattleRewardProfile struct {
	// nil uses the existing drop-derived default; [] disables fame boxes.
	FameRewards []Reward               `json:"fame_rewards"`
	ScorePolicy *TeamBattleScorePolicy `json:"score_policy,omitempty"`
	// nil retains the aggregate legacy policy; an explicit empty table means
	// no tangible drops. Preserve this distinction on JSON round trips.
	EnemyDrops        []TeamBattleEnemyDrop `json:"enemy_drops"`
	BossID            int                   `json:"boss_id"`
	StageQuestAreaID  int                   `json:"stage_quest_area_id"`
	StageQuestStageID int                   `json:"stage_quest_stage_id"`
	TowerID           int                   `json:"tower_id,omitempty"`
	TowerFloor        int                   `json:"tower_floor,omitempty"`
	ResultRewards     []Reward              `json:"result_rewards"`
	FirstClearRewards []Reward              `json:"first_clear_rewards"`
}

// TeamBattleEnemyDrop is an explicitly configured per-wave/per-enemy reward.
// A nil chance preserves guaranteed configured rewards; a supplied chance is
// per million, including zero. Selected start plans clear ChancePerMillion.
// No official probability table is inferred from enemy/card identity alone.
type TeamBattleEnemyDrop struct {
	BattleIndex      int    `json:"battle_index"`
	EnemyIndex       int    `json:"enemy_index"`
	Reward           Reward `json:"reward"`
	ChancePerMillion *int   `json:"chance_per_million,omitempty"`
}

// TeamBattleFameBonusPolicy is the versioned local publication policy for the
// original result scene's fame-bonus boxes. The managed client owns the DTO and
// presentation contract, while the retired per-quest fame-drop table is not in
// the CN package; RewardSource therefore remains explicit instead of being
// hidden in an HTTP handler.
type TeamBattleFameBonusPolicy struct {
	ConfigVersion           int    `json:"config_version"`
	ChanceMaximum           int    `json:"chance_maximum"`
	FullFameThreshold       int    `json:"full_fame_threshold"`
	FullFameRewardCount     int    `json:"full_fame_reward_count"`
	BonusFameAdd            int    `json:"bonus_fame_add"`
	RewardSource            string `json:"reward_source"`
	EligibleRewardTypes     []int  `json:"eligible_reward_types"`
	SoloMemberPolicy        string `json:"solo_member_policy"`
	MultiplayerMemberPolicy string `json:"multiplayer_member_policy"`
	RollPolicy              string `json:"roll_policy"`
}

// TeamBattleHostBonusPolicy is the local publication policy for the original
// multiplayer result scene's single host_rewards slot. The CN client proves
// the DTO and presentation contract, but the retired per-quest host pool is
// absent from the package, so the selected reward source remains explicit.
type TeamBattleHostBonusPolicy struct {
	ConfigVersion          int    `json:"config_version"`
	RewardCount            int    `json:"reward_count"`
	RewardKind             int    `json:"reward_kind"`
	RewardSource           string `json:"reward_source"`
	EligibleRewardTypes    []int  `json:"eligible_reward_types"`
	RecipientPolicy        string `json:"recipient_policy"`
	BattlePointPayerPolicy string `json:"battle_point_payer_policy"`
}

// TowerQuestProfile is an immutable tower definition, separate from account
// progress. Operator publication controls runtime admission by tower ID.
type TowerQuestProfile struct {
	StageType    BattleStageType          `json:"stage_type"`
	TowerID      int                      `json:"tower_id"`
	Name         string                   `json:"name"`
	LoseCountMax int                      `json:"lose_count_max"`
	AllClearText string                   `json:"all_clear_text"`
	Ranks        []TowerQuestRankProfile  `json:"ranks"`
	Floors       []TowerQuestFloorProfile `json:"floors"`
}

type TowerQuestRankProfile struct {
	Rank     int    `json:"rank"`
	RankName string `json:"rank_name"`
}

type TowerQuestFloorProfile struct {
	Rank         int                    `json:"rank"`
	Floor        int                    `json:"floor"`
	Boss         json.RawMessage        `json:"boss"`
	Story        TowerQuestStoryProfile `json:"story"`
	ClearRewards []Reward               `json:"clear_rewards"`
	BPUse        int                    `json:"bp_use"`
	ItemID       int                    `json:"item_id"`
	ItemUse      int                    `json:"item_use"`
}

func (floor TowerQuestFloorProfile) ValidEntryCost() bool {
	return floor.BPUse >= 0 && floor.ItemID >= 0 && floor.ItemUse >= 0 &&
		(floor.ItemUse == 0 || floor.ItemID > 0)
}

type TowerQuestStoryProfile struct {
	TalkID int    `json:"talkid"`
	Title  string `json:"title"`
}

// TowerQuestProgress is mutable account state persisted by the SQLite-backed
// CN save snapshot. LastResult is a one-shot entry rendition consumed by the
// next TowerQuestShow response. ClearRatings holds earned flags by floor;
// shared floor definitions contain no account progression.
type TowerQuestProgress struct {
	TowerID             int         `json:"tower_id"`
	Floor               int         `json:"floor"`
	LastBattleFloor     int         `json:"last_battle_floor"`
	LoseCount           int         `json:"lose_count"`
	LastResult          string      `json:"last_result,omitempty"`
	LastRankUp          bool        `json:"last_rank_up,omitempty"`
	LastResultLoseCount int         `json:"last_result_lose_count,omitempty"`
	ClearedFloors       []int       `json:"cleared_floors,omitempty"`
	ClearRatings        map[int]int `json:"clear_ratings,omitempty"`
	BestClearTurns      map[int]int `json:"best_clear_turns,omitempty"`
}

// BattlePointState is the local persistence contract for the official client's
// stamina countdown. RecoverySeconds is configuration; NextRecoveryUnix is
// mutable player state and remains zero while BP is full.
type BattlePointState struct {
	RecoverySeconds  int   `json:"recovery_seconds"`
	NextRecoveryUnix int64 `json:"next_recovery_unix,omitempty"`
}

// PVPDeckSelection is the persistent four-Arthur deck identity submitted by
// the official CN client. Card attributes are resolved from the owning
// account snapshot when an opponent is selected, so later inventory changes
// cannot silently replace the selected deck indexes.
type PVPDeckSelection struct {
	ArthurType           int8    `json:"arthur_type"`
	JobType              int8    `json:"job_type"`
	DeckIndex            int8    `json:"deck_idx"`
	LeaderCardIndex      int     `json:"leader_card_idx"`
	CardUniqueIDs        []int64 `json:"card_uniqid"`
	SupportCardUniqueIDs []int64 `json:"support_card_uniqid"`
	SphereUniqueIDs      []int64 `json:"sphr_uniqid"`
	BuddyUniqueIDs       []int64 `json:"buddy_uniqid"`
}

type PVPMatch struct {
	BattleID        int   `json:"battle_id"`
	BattleType      int   `json:"battle_type"`
	OpponentUserID  int   `json:"opponent_user_id"`
	ExpectedWin     bool  `json:"expected_win"`
	Retired         bool  `json:"retired,omitempty"`
	PointBefore     int   `json:"point_before"`
	PointAfter      int   `json:"point_after"`
	StartedAtUnix   int64 `json:"started_at_unix"`
	CompletedAtUnix int64 `json:"completed_at_unix,omitempty"`
}

// PVPPlayerState is mutable account data. Static fields, rank thresholds and
// settlement policy remain in the separately loaded CN PVP runtime config.
type PVPPlayerState struct {
	Challenge            int                `json:"challenge"`
	ChallengeDay         int64              `json:"challenge_day,omitempty"`
	NextBattleID         int                `json:"next_battle_id"`
	DefenseUpdatedAtUnix int64              `json:"defense_updated_at_unix,omitempty"`
	DefenseDecks         []PVPDeckSelection `json:"defense_decks"`
	ActiveMatch          *PVPMatch          `json:"active_match,omitempty"`
	History              []PVPMatch         `json:"history"`
}

// TeamBattleResultReceipt is an account-owned idempotency record for the
// client's TeamBattleResult retry. Snapshot stores the granted business result
// committed with the account mutation, independently of the response protocol.
type TeamBattleResultReceipt struct {
	RoomID        int64           `json:"room_id"`
	ClaimedAtUnix int64           `json:"claimed_at_unix"`
	Snapshot      json.RawMessage `json:"snapshot,omitempty"`
}

// TeamBattleSoloResultReceipt is the durable acknowledgement for an original
// client's retried TeamBattleSoloEnd report. The client keeps an unacknowledged
// native report across title/login, so the canonical request digest and granted
// result must outlive the active battle itself. A key without a snapshot marks
// a retired receipt and must not settle that report again.
type TeamBattleSoloResultReceipt struct {
	RequestSHA256 string          `json:"request_sha256"`
	BossID        int             `json:"boss_id"`
	ClaimedAtUnix int64           `json:"claimed_at_unix"`
	Snapshot      json.RawMessage `json:"snapshot,omitempty"`
}

// ExploreResultReceipt preserves the last committed ExploreEnd method response.
// ExploreEnd has no request identity in the official client contract, so an
// active exploration always takes precedence and replaces this receipt when it
// settles; without an active exploration, Comeback retries receive this exact
// response instead of an empty result.
type ExploreResultReceipt struct {
	StartedAtUnix int64           `json:"started_at_unix"`
	ClaimedAtUnix int64           `json:"claimed_at_unix"`
	Response      json.RawMessage `json:"response"`
}

// PVPResultReceipt is the account-owned durable acknowledgement for a retried
// PvpEnd report. BattleID owns the settlement identity and RequestSHA256 freezes
// the canonical result flags and native command for that battle.
type PVPResultReceipt struct {
	BattleID      int             `json:"battle_id"`
	RequestSHA256 string          `json:"request_sha256"`
	ClaimedAtUnix int64           `json:"claimed_at_unix"`
	Response      json.RawMessage `json:"response"`
}

// TeamBattleStartReceipt durably identifies the room's already paid entry cost.
type TeamBattleStartReceipt struct {
	RoomID      int64 `json:"room_id"`
	BossID      int   `json:"boss_id"`
	BPUse       int   `json:"bp_use"`
	MedalItemID int   `json:"medal_item_id,omitempty"`
	MedalUse    int   `json:"medal_use,omitempty"`
}

// TeamBattleActiveState is an in-memory unsettled session. Account reloads in
// this process may restore it; it is not included in SQLite account metadata.
type TeamBattleActiveState struct {
	ScoreBossID               int                            `json:"score_boss_id,omitempty"`
	StageType                 BattleStageType                `json:"stage_type"`
	EntryRole                 BattleEntryRole                `json:"entry_role"`
	ClearDecks                []BattleClearDeck              `json:"-"` // In-memory solo session; only successful clears enter statistics.
	ScorePolicy               *TeamBattleScorePolicy         `json:"score_policy,omitempty"`
	Seed                      int                            `json:"seed"`
	DropPlanSet               bool                           `json:"drop_plan_set,omitempty"`
	DropPlan                  []TeamBattleEnemyDrop          `json:"drop_plan,omitempty"`
	BossID                    int                            `json:"boss_id"`
	BattleEnemyTypes          []int8                         `json:"battle_enemy_types"`
	StageQuestAreaID          int                            `json:"stage_quest_area_id,omitempty"`
	StageQuestStageID         int                            `json:"stage_quest_stage_id,omitempty"`
	TowerID                   int                            `json:"tower_id,omitempty"`
	TowerFloor                int                            `json:"tower_floor,omitempty"`
	ItemID                    int                            `json:"item_id,omitempty"`
	ItemUse                   int                            `json:"item_use,omitempty"`
	BPUse                     int                            `json:"bp_use"`
	PrepaidRoomID             int64                          `json:"prepaid_room_id,omitempty"`
	FameSeed                  string                         `json:"fame_seed"`
	StartTime                 int                            `json:"start_time,omitempty"`
	SoloRequestKey            string                         `json:"solo_request_key,omitempty"`
	FameRewardsSet            bool                           `json:"fame_rewards_set,omitempty"`
	FameRewards               []Reward                       `json:"fame_rewards,omitempty"`
	FameSources               []TeamBattleFameSourceState    `json:"fame_sources"`
	HostBonusMemberType       int                            `json:"host_bonus_member_type,omitempty"`
	FriendPointPartners       int                            `json:"friend_point_partners"`
	FriendPointReward         int                            `json:"friend_point_reward"`
	FriendPointRentalCredits  []TeamBattleRentalCreditState  `json:"friend_point_rental_credits,omitempty"`
	FriendPointRentalEventKey string                         `json:"friend_point_rental_event_key,omitempty"`
	SelectedPartners          []TeamBattleResultPartnerState `json:"selected_partners,omitempty"`
	ContinueAllowed           bool                           `json:"continue_allowed,omitempty"`
	ContinueReceipts          []string                       `json:"continue_receipts,omitempty"`
}

type TeamBattleFameSourceState struct {
	MemberType int `json:"member_type"`
	LeaderFame int `json:"leader_fame"`
}

type TeamBattleRentalCreditState struct {
	OwnerUserID int `json:"owner_user_id"`
	FriendPoint int `json:"friend_point"`
}

type TeamBattleResultPartnerState struct {
	IsBurst       int8   `json:"is_burst"`
	LastLoginUnix int64  `json:"last_login_unix,omitempty"`
	UserID        int    `json:"user_id"`
	IsSelf        bool   `json:"is_self,omitempty"`
	Name          string `json:"name"`
	ArthurType    int8   `json:"arthur_type"`
	Level         int    `json:"level"`
	DeckRank      int8   `json:"deck_rank"`
	LeaderCardID  int    `json:"leader_card_id"`
	LeaderLevel   int    `json:"leader_level"`
	LeaderFame    int    `json:"leader_fame"`
	Comment       string `json:"comment"`
	PVPPoint      int    `json:"pvp_point"`
	HonorIDs      []int  `json:"honor_ids"`
}

// TeamBattleScheduleState stores the original client's per-mode reminder
// choices. The local profile restores the list and toggle semantics only; it
// does not deliver operating-system push notifications.
type TeamBattleScheduleState struct {
	SoloPushGroupIDs  []int `json:"solo_push_group_ids,omitempty"`
	MultiPushGroupIDs []int `json:"multi_push_group_ids,omitempty"`
}

// OnboardingState is the persisted server-side half of the original CN
// tutorial. Presentation, hand cursors and scene transitions remain owned by
// the unmodified client; this state only publishes the matching quest/reward
// sequence and unlocks features after the corresponding server event.
type OnboardingState struct {
	ConfigVersion       int  `json:"config_version"`
	Step                int  `json:"step"`
	CurrentAnnounced    bool `json:"current_announced"`
	PendingClearQuestID int  `json:"pending_clear_questid,omitempty"`
}

// AlchemistStarterDeck is shared content configuration. The existing feature
// unlock owns completion; no separate per-player grant record is needed.
type AlchemistStarterDeck struct {
	SummonCardIDs [3]int `json:"summon_card_ids"`
	SkillCardIDs  [7]int `json:"skill_card_ids"`
}

// NavigationState keeps server-selected context across requests and cache reloads.
// It contains no public story or dungeon definitions.
type NavigationState struct {
	MainStoryID        int  `json:"main_story_id,omitempty"`
	MainStoryCN        bool `json:"main_story_cn,omitempty"`
	SubStoryID         int  `json:"sub_story_id,omitempty"`
	SubStoryTowerID    int  `json:"sub_story_tower_id,omitempty"`
	SubStoryTowerFloor int  `json:"sub_story_tower_floor,omitempty"`
	StageAreaID        int  `json:"stage_area_id,omitempty"`
}

type State struct {
	TeamBattleOwnDeckSources  map[int]int                     `json:"-"`
	DisabledTeamBattleBossIDs map[int]bool                    `json:"-"`
	BurstProgress             [4]uint8                        `json:"burst_progress"`
	StoryTeamBattleSession    StoryTeamBattleSession          `json:"-"`
	Navigation                NavigationState                 `json:"navigation,omitempty"`
	LastLoginUnix             int64                           `json:"-"` // read from the account login table, not a duplicated snapshot field
	TeamBattleScores          map[int]TeamBattleScoreProgress `json:"team_battle_scores,omitempty"`
	TeamBattleSoloScores      map[int]TeamBattleScoreProgress `json:"team_battle_solo_scores,omitempty"`
	LocalShop                 LocalShopState                  `json:"local_shop,omitempty"`
	SchemaVersion             int                             `json:"schema_version"`
	ReleaseID                 string                          `json:"release_id"`
	CatalogVersion            int                             `json:"catalog_version"`

	User                           User                     `json:"user"`
	PlayerProgressionConfigVersion int                      `json:"player_progression_config_version"`
	PlayerProgressionPolicy        PlayerProgressionPolicy  `json:"-"`
	CardProgressionConfigVersion   int                      `json:"card_progression_config_version"`
	CardProgressionPolicy          CardProgressionPolicy    `json:"-"`
	DeckRankPolicy                 DeckRankPolicy           `json:"-"`
	AlchemistStarterDeck           *AlchemistStarterDeck    `json:"-"`
	CardExperienceTables           map[int][]int            `json:"-"`
	FeatureUnlockConfigVersion     int                      `json:"feature_unlock_config_version"`
	Onboarding                     OnboardingState          `json:"onboarding"`
	ProfileConfigVersion           int                      `json:"profile_config_version"`
	CurrencyConfigVersion          int                      `json:"currency_config_version"`
	BattleLoadoutConfigVersion     int                      `json:"battle_loadout_config_version"`
	BattleLoadoutCardUniqueIDs     []int64                  `json:"battle_loadout_card_unique_ids,omitempty"`
	BattlePointConfigVersion       int                      `json:"battle_point_config_version"`
	BattlePoint                    BattlePointState         `json:"battle_point"`
	PVPConfigVersion               int                      `json:"pvp_config_version"`
	PVP                            PVPPlayerState           `json:"pvp"`
	SphereConfigVersion            int                      `json:"sphere_config_version"`
	SphereProgressionPolicy        SphereProgressionPolicy  `json:"-"`
	Spheres                        []Sphere                 `json:"spheres"`
	SphereDefinitions              []SphereDefinition       `json:"-"`
	SphereExperienceTables         map[int][]int            `json:"-"`
	SphereEvolutionPrices          map[string][]int         `json:"-"`
	Cards                          []Card                   `json:"cards"`
	ContainerCards                 []Card                   `json:"container_cards,omitempty"`
	CardTemplates                  []Card                   `json:"card_templates,omitempty"`
	CardSellExtract                *CardSellExtractPolicy   `json:"-"`
	CardCategoryProfiles           []CardCategoryProfile    `json:"-"`
	CardGroupProfiles              []CardGroupProfile       `json:"-"`
	CardCollectionPages            [][10]int                `json:"-"`
	StackCardTemplates             []CardStack              `json:"-"`
	StackCards                     []CardStack              `json:"stack_cards"`
	Decks                          []Deck                   `json:"decks"`
	Avatars                        []Avatar                 `json:"avatars"`
	AvatarConfigVersion            int                      `json:"avatar_config_version"`
	AvatarParts                    []int                    `json:"avatar_parts"`
	AvatarPartDefinitions          []AvatarPartDefinition   `json:"-"`
	AvatarShopPartIDs              []int                    `json:"-"`
	AvatarDefaultDecks             [][]int                  `json:"-"`
	AvatarSeries                   []AvatarSeriesDefinition `json:"-"`
	AvatarSeriesCompletions        []AvatarSeriesCompletion `json:"-"`
	AvatarShopPolicy               AvatarShopPolicy         `json:"-"`
	Buddy                          Buddy                    `json:"buddy"`
	BuddyConfigVersion             int                      `json:"buddy_config_version"`
	BuddyProgressionPolicy         BuddyProgressionPolicy   `json:"-"`
	Buddies                        []Buddy                  `json:"buddies"`
	BuddyDefinitions               []BuddyDefinition        `json:"-"`
	BuddyExperienceTables          map[int][]int            `json:"-"`
	BuddyEvolutionPrices           map[string][]int         `json:"-"`
	Profiles                       []string                 `json:"profiles"`
	Friends                        FriendCollectionState    `json:"friends"`
	Stamps                         StampCollectionState     `json:"stamps"`
	Honors                         HonorCollectionState     `json:"honors"`
	Story                          StoryCatalogState        `json:"story"`
	StoryRewardPolicy              StoryRewardPolicy        `json:"-"`
	EventPageProfile               EventPageProfile         `json:"-"`
	PopupProfile                   PopupProfile             `json:"-"`
	ExploreConfigVersion           int                      `json:"explore_config_version"`
	Explore                        ExploreProgressState     `json:"explore"`
	Engagement                     EngagementState          `json:"engagement"`
	Options                        OptionState              `json:"options"`
	LoginBonus                     LoginBonusState          `json:"login_bonus"`
	LoginBonusPolicy               LoginBonusPolicy         `json:"-"`
	LocalAccountConfigVersion      int                      `json:"local_account_config_version"`
	TeamBattleMedalItemID          int                      `json:"-"`
	BossCoinItemID                 int                      `json:"-"`
	ItemShopConfigVersion          int                      `json:"item_shop_config_version"`
	Items                          []Item                   `json:"items"`
	ItemShopTabs                   []ItemShopTab            `json:"item_shop_tabs"`
	ItemDefinitions                []ItemDefinition         `json:"-"`
	UserBuffProfiles               []UserBuffProfile        `json:"-"`
	ItemGachaProfiles              []ItemGachaProfile       `json:"-"`
	ItemExchangeProfiles           []ItemExchangeProfile    `json:"-"`
	ItemLackTipProfiles            []ItemLackTipProfile     `json:"-"`
	EventShopProfiles              []EventShopProfile       `json:"-"`
	EventShopPurchases             []EventShopPurchase      `json:"event_shop_purchases,omitempty"`
	TradeShopProfiles              []TradeShopProfile       `json:"-"`
	TradeShopPurchases             []TradeShopPurchase      `json:"trade_shop_purchases,omitempty"`
	GachaConfigVersion             int                      `json:"gacha_config_version"`
	Gachas                         []GachaProfile           `json:"gachas"`
	// OperatorGachaPlays keeps play counts of operator-created pools that are not in the catalog list;
	// the account appends those pools when their configuration is applied. Persisted through progress only.
	OperatorGachaPlays           map[int]int                         `json:"-"`
	ItemShopPurchases            map[int]int                         `json:"-"`
	ItemShopPeriods              map[int]ItemShopPeriodCounts        `json:"-"`
	GachaSelections              []GachaSelection                    `json:"gacha_selections,omitempty"`
	GachaDailyClaims             []GachaDailyClaim                   `json:"gacha_daily_claims,omitempty"`
	FriendPointInboxCursor       int64                               `json:"friend_point_inbox_cursor,omitempty"`
	CardActions                  CardActionState                     `json:"card_actions"`
	CardDevelopment              CardDevelopmentState                `json:"card_development"`
	CardDevelopmentPolicy        CardDevelopmentPolicy               `json:"-"`
	SupportDeckConfigVersion     int                                 `json:"support_deck_config_version"`
	SupportDeck                  SupportDeckState                    `json:"support_deck"`
	SupportDeckSetCardNum        int                                 `json:"-"`
	SupportDeckSlotUnlockRules   []SupportDeckSlotUnlockRule         `json:"-"`
	StageQuestConfigVersion      int                                 `json:"stage_quest_config_version"`
	NormalQuestSection           *MainQuestSectionDefinition         `json:"-"`
	MainQuestReadingSections     []MainQuestReadingSectionDefinition `json:"-"`
	MainQuest                    json.RawMessage                     `json:"main_quest"`
	StageQuestAreas              []json.RawMessage                   `json:"stage_quest_areas,omitempty"`
	TeamBattleConfigVersion      int                                 `json:"team_battle_config_version"`
	MissionDefinitions           map[int]MissionDefinition           `json:"-"`
	TeamBattleSolo               json.RawMessage                     `json:"team_battle_solo_show"`
	TeamBattleReplays            []TeamBattleReplay                  `json:"team_battle_replays"`
	TeamBattleRewards            []TeamBattleRewardProfile           `json:"team_battle_rewards"`
	TeamBattleFameBonusPolicy    TeamBattleFameBonusPolicy           `json:"-"`
	TeamBattleHostBonusPolicy    TeamBattleHostBonusPolicy           `json:"-"`
	TeamBattleRecommendations    []TeamBattleRecommendation          `json:"-"`
	TeamBattlePastBossGroups     []json.RawMessage                   `json:"-"`
	TeamBattleScheduleGroupIDs   []int                               `json:"-"`
	TowerQuestConfigVersion      int                                 `json:"tower_quest_config_version"`
	TowerQuestProfiles           []TowerQuestProfile                 `json:"-"`
	TowerQuestProgress           []TowerQuestProgress                `json:"tower_quest_progress,omitempty"`
	TeamBattleSchedule           TeamBattleScheduleState             `json:"team_battle_schedule,omitempty"`
	TeamBattleResultReceipts     []TeamBattleResultReceipt           `json:"team_battle_result_receipts,omitempty"`
	TeamBattleStartReceipts      []TeamBattleStartReceipt            `json:"team_battle_start_receipts,omitempty"`
	TeamBattleContinueReceipts   []TeamBattleContinueReceipt         `json:"team_battle_continue_receipts,omitempty"`
	TeamBattleSoloResultReceipts []TeamBattleSoloResultReceipt       `json:"team_battle_solo_result_receipts,omitempty"`
	ExploreResultReceipt         *ExploreResultReceipt               `json:"explore_result_receipt,omitempty"`
	PVPResultReceipts            []PVPResultReceipt                  `json:"pvp_result_receipts,omitempty"`
	ActiveTeamBattle             *TeamBattleActiveState              `json:"active_team_battle,omitempty"`
	Costume                      json.RawMessage                     `json:"costume"`
	CollectionRewards            []CollectionRewardDefinition        `json:"-"`
	InventorySequence            InventorySequenceState              `json:"inventory_sequence,omitempty"`
	State                        string                              `json:"state"`
}
