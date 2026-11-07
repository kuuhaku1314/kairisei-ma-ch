package gamestate

// DeckLeaderParameter is the existing CN leader-card projection, shared by
// server deck totals and the local client ABI. Truncate each stat separately.
func DeckLeaderParameter(value int) int { return value * 150 / 100 }

// DeckSupportParameter preserves the existing order of double-precision
// arithmetic in PartnerDeckStats. Rate is supplied by card_param_bonus.csv;
// lovePercent has already passed the original integer getLovePer conversion.
func DeckSupportParameter(value, rate, lovePercent int) int {
	scale := float64(lovePercent)*90/100 + 10
	return int((float64(rate) * scale / 100) * float64(value) / 100)
}
