package multiplayer

import "kairisei.local/server/battlecodec"

func encodeBattleResults(results []BattleResult) (string, error) {
	return battlecodec.Encode(results)
}
func encodeOptionalBattleResults(results []BattleResult) (string, error) {
	return battlecodec.EncodeOptional(results)
}
func cardPlayPlanResult(engine *BattleEngine, memberType int, submission cardPlaySubmission) (string, error) {
	results, err := engine.CardPlayPlanResults(memberType, submission)
	if err != nil {
		return "", err
	}
	return battlecodec.Encode(results)
}
