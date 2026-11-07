package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"kairisei.local/server/internal/operations"
)

func (h *accountBusinessHandler) ApplyHomeBannerConfiguration(config operations.HomeBannerConfiguration) {
	// Publication is immutable and already owns a copy of its row slice.
	h.api.homeBanners = config
}

func (a *API) configuredHomeBanners(request *http.Request, now time.Time) []any {
	result := []any{}
	if a.account.OnboardingInProgress() {
		return result
	}
	visibleGroups := make(map[int]int)
	if a.account.FeatureUnlocked(20) {
		for _, profile := range a.account.GachaState() {
			if int64(profile.EndTime) > now.Unix() && gachaPublished(request, profile.GachaID) &&
				(visibleGroups[profile.GroupID] == 0 || profile.GachaID < visibleGroups[profile.GroupID]) {
				visibleGroups[profile.GroupID] = profile.GachaID
			}
		}
	}
	for _, row := range a.homeBanners.Policy.Items {
		if !row.Enabled || row.StartUnix > now.Unix() || row.EndUnix > 0 && now.Unix() >= row.EndUnix {
			continue
		}
		command := "none"
		switch row.Target {
		case "gacha_group":
			gachaID := visibleGroups[row.TargetID]
			if gachaID == 0 {
				continue
			}
			command = "gacha:" + strconv.Itoa(gachaID)
		case "boss_score_rank":
			if !a.scoreRankingBossAvailable(row.TargetID) {
				continue
			}
			command = "boss_score_rank:" + strconv.Itoa(row.TargetID)
		case "notice":
			command = a.baseURL + "/disabled/web"
		case "none":
		default:
			continue
		}
		base := a.baseURL + "/local/"
		if a.homeBanners.ImageBaseURL != "" {
			base = strings.TrimRight(a.homeBanners.ImageBaseURL, "/") + "/"
		}
		result = append(result, map[string]any{
			"home_banner_type": 12, "image_url": base + row.CoverPath, "open_url": command,
			"infomation": []string{}, "eventid": 0,
		})
	}
	return result
}
