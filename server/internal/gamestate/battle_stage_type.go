package gamestate

// BattleStageType follows JP 8.0.7. CN-only features have independent identities
// in the extension range; a stage value is never interpreted by region at run time.
type BattleStageType int

const (
	BattleStageNormal         BattleStageType = 0
	BattleStageWeekdaySun     BattleStageType = 1
	BattleStageWeekdayMon     BattleStageType = 2
	BattleStageWeekdayTue     BattleStageType = 3
	BattleStageWeekdayWed     BattleStageType = 4
	BattleStageWeekdayThu     BattleStageType = 5
	BattleStageWeekdayFri     BattleStageType = 6
	BattleStageWeekdaySat     BattleStageType = 7
	BattleStageGuerrilla      BattleStageType = 8
	BattleStageEvent          BattleStageType = 9
	BattleStageSolo           BattleStageType = 10
	BattleStageStageQuest     BattleStageType = 11
	BattleStageRookie         BattleStageType = 12
	BattleStageScore          BattleStageType = 13
	BattleStageMainEvent      BattleStageType = 14
	BattleStageMainStageQuest BattleStageType = 15
	BattleStageMainScore      BattleStageType = 16
	BattleStageDeckLimit      BattleStageType = 17
	BattleStageBurstArthur    BattleStageType = 18
	BattleStageAlchemist      BattleStageType = 19
	BattleStageAlchemistSP    BattleStageType = 20
	BattleStageMainQuest      BattleStageType = 21

	// Local CN extension policy, not the historical CN wire values 19 and 20.
	BattleStageCNRecommendCard BattleStageType = 100
	BattleStageCNWorldBoss     BattleStageType = 101
)
