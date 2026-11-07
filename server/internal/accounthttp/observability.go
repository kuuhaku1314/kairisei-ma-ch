package accounthttp

import (
	"sort"
	"time"

	"kairisei.local/server/internal/accountstore"
)

const ActivityWindow = 5 * time.Minute

type PlayerActivity struct {
	UserID        int       `json:"user_id"`
	LastRequest   time.Time `json:"last_request"`
	UnsettledSolo bool      `json:"unsettled_solo"`
}

type requestBucket struct {
	Epoch      int64
	Requests   int64
	Errors     int64
	DurationMS float64
	Slow       int64
}

type ActivitySnapshot struct {
	Players       []PlayerActivity `json:"players"`
	WindowSeconds int              `json:"window_seconds"`
	Requests      int64            `json:"requests"`
	Errors        int64            `json:"http_errors"`
	SlowRequests  int64            `json:"slow_requests"`
	MeanMS        float64          `json:"mean_ms"`
}

func (router *Router) recordActivity(id int, started time.Time, httpFailed, active, solo bool) {
	now := time.Now()
	router.mu.Lock()
	defer router.mu.Unlock()
	if router.activity == nil {
		router.activity = make(map[int]PlayerActivity)
	}
	for id, row := range router.activity {
		if now.Sub(row.LastRequest) >= ActivityWindow {
			delete(router.activity, id)
		}
	}
	if active && id >= accountstore.PrimaryUserID && id < accountstore.SystemPartnerUserIDBase {
		router.activity[id] = PlayerActivity{UserID: id, LastRequest: now, UnsettledSolo: solo}
	}
	epoch := now.Unix() / 30
	bucket := &router.observed[epoch%int64(len(router.observed))]
	if bucket.Epoch != epoch {
		*bucket = requestBucket{Epoch: epoch}
	}
	bucket.Requests++
	if httpFailed {
		bucket.Errors++
	}
	duration := now.Sub(started)
	bucket.DurationMS += float64(duration) / float64(time.Millisecond)
	if duration >= time.Second {
		bucket.Slow++
	}
}

func (router *Router) ActivitySnapshot() ActivitySnapshot {
	now := time.Now()
	router.mu.Lock()
	defer router.mu.Unlock()
	result := ActivitySnapshot{Players: []PlayerActivity{}, WindowSeconds: int(ActivityWindow / time.Second)}
	for id, row := range router.activity {
		if now.Sub(row.LastRequest) >= ActivityWindow {
			delete(router.activity, id)
			continue
		}
		result.Players = append(result.Players, row)
	}
	sort.Slice(result.Players, func(i, j int) bool { return result.Players[i].UserID < result.Players[j].UserID })
	for _, b := range router.observed {
		if b.Epoch <= now.Unix()/30-10 {
			continue
		}
		result.Requests += b.Requests
		result.Errors += b.Errors
		result.SlowRequests += b.Slow
		result.MeanMS += b.DurationMS
	}
	if result.Requests > 0 {
		result.MeanMS /= float64(result.Requests)
	}
	return result
}
