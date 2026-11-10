package gamestate

const (
	OnboardingStepCount = 9
	// CONFIRMED: GachaMgr recognizes 90000200..90000203 as the one-draw
	// first-gacha family and switches the original scene into its fixed guide.
	OnboardingGachaID = 90000200
	// CONFIRMED: the paired 90000100..90000103 family is the client's later
	// first multi-draw scene. It records completion only for card_num >= 11.
	OnboardingMultiGachaID = 90000100
)

func IsOnboardingGachaID(gachaID int) bool {
	return gachaID == OnboardingGachaID || gachaID == OnboardingMultiGachaID
}
