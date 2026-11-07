package gamestate

import "encoding/json"

// Receipt envelopes deliberately ignore unknown fields, even when the editable
// save uses a strict decoder. A retired response-only record retains its key but
// has no snapshot: it cannot replay a response or grant this battle again.
func (receipt *TeamBattleResultReceipt) UnmarshalJSON(data []byte) error {
	type envelope TeamBattleResultReceipt
	return json.Unmarshal(data, (*envelope)(receipt))
}

func (receipt *TeamBattleSoloResultReceipt) UnmarshalJSON(data []byte) error {
	type envelope TeamBattleSoloResultReceipt
	return json.Unmarshal(data, (*envelope)(receipt))
}
