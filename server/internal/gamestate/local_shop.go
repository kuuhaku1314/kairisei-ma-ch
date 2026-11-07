package gamestate

// LocalShopState is account data saved with the existing SQLite snapshot. It
// contains no channel SDK orders or real payment credentials.
type LocalShopState struct {
	MonthEndDay     int64          `json:"month_end_day,omitempty"`
	MonthClaimDay   int64          `json:"month_claim_day,omitempty"`
	Forever         bool           `json:"forever,omitempty"`
	ForeverClaimDay int64          `json:"forever_claim_day,omitempty"`
	Orders          map[string]int `json:"orders,omitempty"`
}

type LocalShopProduct struct {
	ID      string
	Name    string
	Crystal int
}

// The monthly amounts match CN settings/str_table.csv 215/1000/1-3.
// Ordinary crystal bundles and free prices are local configuration, not a
// reconstruction of the retired channel's product or payment service.
func LocalShopProducts() []LocalShopProduct {
	return []LocalShopProduct{
		{"1", "月卡（30天）", 250},
		{"2", "无穷卡", 600},
		{"3", "水晶礼包", 60},
		{"4", "水晶礼包", 300},
		{"5", "水晶礼包", 680},
		{"6", "水晶礼包", 1280},
		{"7", "水晶礼包", 3280},
		{"8", "水晶礼包", 6480},
	}
}
