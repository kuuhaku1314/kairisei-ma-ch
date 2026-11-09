package gamestate

// DeckRankPolicy is immutable data from the CN card/skill/rank TextAssets.
// It is carried by the runtime master, never by a client request or save.
type DeckRankPolicy struct {
	ConfigVersion       int                  `json:"config_version"`
	ParameterThresholds []int                `json:"parameter_thresholds"`
	SkillThresholds     []int                `json:"skill_thresholds"`
	Cards               map[int]CardRankRule `json:"cards"`
}

type CardRankRule struct {
	ArthurType        int8   `json:"arthur_type,omitempty"`
	MaximumParameters [4]int `json:"maximum_parameters"`
	// Native returns zero for an absent skill, without adding the card level.
	// -1 represents that branch; other values include the matching-job bonus.
	SkillPoints [5]int `json:"skill_points"`
}
