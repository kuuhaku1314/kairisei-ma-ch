package multiplayer

import "kairisei.local/server/battlecodec"

// Transport adapters retained during core extraction; encoding belongs to the
// shared original-consumer codec, not to an engine instance or its result type.
func encodeBattleResult(result BattleResult) (string, error) {
	return battlecodec.EncodeRow(result)
}
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
