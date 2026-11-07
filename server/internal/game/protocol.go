package game

type CardInfo struct {
	UniqueID      int64
	CreateTime    int
	CardID        int
	Level         int
	LevelMax      int
	Experience    int
	NowLevelEXP   int
	Love          int
	LoveMax       int
	SkillLevels   []int16
	HP            int
	Attack        int
	Magic         int
	Mind          int
	NextLevelEXP  int
	AddExperience int
	BaseAddPrice  int
	IsLock        int8
	Fame          int
	Slot          int
}

type DeckInfo struct {
	ArthurType           int8    `json:"arthur_type"`
	Index                int8    `json:"idx"`
	JobType              int8    `json:"job_type"`
	LeaderCardIndex      int8    `json:"leader_card_idx"`
	CardUniqueIDs        []int64 `json:"card_uniqid"`
	SupportCardUniqueIDs []int64 `json:"support_card_uniqid"`
	SphereUniqueIDs      []int64 `json:"sphr_uniqid"`
	BuddyUniqueIDs       []int64 `json:"buddy_uniqid"`
	Name                 string  `json:"name"`
	IsActive             int8    `json:"is_active"`
	IsRental             int8    `json:"is_rental"`
	DeckRank             int8    `json:"deck_rank"`
}
