package httpapi

import (
	"testing"
	"time"
)

func TestTeamBattleScheduleAnchorUsesServiceDay(t *testing.T) {
	saved := time.Local
	time.Local = time.UTC
	defer func() { time.Local = saved }()
	for _, sample := range []struct{ now, midnight string }{
		{"2026-11-03T15:59:59Z", "2026-11-02T16:00:00Z"},
		{"2026-11-03T16:00:00Z", "2026-11-03T16:00:00Z"},
		{"2026-11-03T23:59:59Z", "2026-11-03T16:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, sample.now)
		want, _ := time.Parse(time.RFC3339, sample.midnight)
		for _, zone := range []*time.Location{time.UTC, time.FixedZone("device", -7*3600), time.FixedZone("service", 8*3600)} {
			if got := teamBattleScheduleAnchorUnix(now.In(zone)); got != want.Unix() {
				t.Fatalf("%s in %s: anchor=%s want=%s", sample.now, zone, time.Unix(got, 0).UTC(), want)
			}
		}
	}
}
