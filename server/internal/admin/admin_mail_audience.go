package admin

import "kairisei.local/server/internal/accountstore"

// The normal allocator uses MAX(real-player UID)+1 and does not delete accounts.
// A fixed inclusive range excludes future registrations and system partners.
// Only existing rows in the range become recipients; gaps are never invented.
type mailRecipientRange struct {
	StartUID int `json:"start_uid"`
	EndUID   int `json:"end_uid"`
	Count    int `json:"count"`
}

func (r mailRecipientRange) valid() bool {
	return r.StartUID >= accountstore.PrimaryUserID && r.EndUID < accountstore.SystemPartnerUserIDBase && r.StartUID <= r.EndUID && r.Count > 0 && r.Count <= r.EndUID-r.StartUID+1
}
