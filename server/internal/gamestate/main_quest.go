package gamestate

// MainQuestSectionDefinition is a shared directory header. Normal quest
// progress and battle identities stay in their existing areas.
type MainQuestSectionDefinition struct {
	SectionID  int    `json:"sectionid"`
	Title      string `json:"title"`
	BGPictID   int    `json:"bg_pictid"`
	IconPictID int    `json:"icon_pictid"`
}

// Reading definitions retain the original directory identities, independently
// of publication and private completion state. Loading them does not open them.
type MainQuestReadingSectionDefinition struct {
	MainQuestSectionDefinition
	BossGroups []MainQuestReadingGroupDefinition `json:"boss_groups"`
}

type MainQuestReadingGroupDefinition struct {
	GroupID int                               `json:"boss_groupid"`
	Name    string                            `json:"name"`
	PictID  int                               `json:"pictid"`
	Stories []MainQuestReadingStoryDefinition `json:"stories"`
}

type MainQuestReadingStoryDefinition struct {
	StoryTeamBattleID int    `json:"story_teambattleid"`
	TalkID            int    `json:"talkid"`
	Title             string `json:"title"`
	ResultCostumeID   int    `json:"result_costumeid"`
}
