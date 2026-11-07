package gamestate

// Mission category is editorial metadata, independent of period and evaluator.
type MissionSeries struct {
	SeriesID int                 `json:"series_id"`
	Name     string              `json:"name"`
	Kind     string              `json:"kind"`   // mission or achievement
	Layout   string              `json:"layout"` // independent or chain
	TabType  int                 `json:"tab_type"`
	Daily    bool                `json:"daily"`
	StartsAt int64               `json:"starts_at"`
	EndsAt   int64               `json:"ends_at"`
	Enabled  bool                `json:"enabled"`
	Archived bool                `json:"archived"`
	Tasks    []MissionDefinition `json:"tasks"`
}

type MissionRuleKind string

const (
	MissionCount      MissionRuleKind = "" // Zero value selects event counting.
	MissionStateValue MissionRuleKind = "state"
	MissionTargetSet  MissionRuleKind = "targets"
)

const (
	MissionCardEvolution MissionEventKind = "card_evolution"
	MissionPVPPlayed     MissionEventKind = "pvp_played"
)

type MissionMetric string

const (
	MissionPlayerLevel     MissionMetric = "player_level"
	MissionCollectionCount MissionMetric = "collection_count"
	MissionLoginDays       MissionMetric = "login_days"
	MissionLoginStreak     MissionMetric = "login_streak"
	MissionCardFame        MissionMetric = "card_fame"
	MissionCardOwned       MissionMetric = "card_owned"
	MissionQuestCleared    MissionMetric = "quest_cleared"
	MissionStoryCleared    MissionMetric = "story_cleared"
	MissionPVPRank         MissionMetric = "pvp_rank"
)

// This describes successful role visits, not login-bonus claims or HTTP traffic.
type MissionVisits struct {
	LastDay int64 `json:"last_day"`
	Streak  int   `json:"streak"`
}
