package admin

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kairisei.local/server/internal/operations"
)

// Home banners have their own content-addressed folder beside the pool covers and follow the same
// image source setting.
const homeBannerFolder = "home-banners"

var homeBannerPathPattern = regexp.MustCompile(`^home-banners/[0-9a-f]{64}\.(png|jpg)$`)

func (a *API) uploadHomeBanner(w http.ResponseWriter, r *http.Request) {
	storeUploadedImage(w, r, a.homeBannerDir, homeBannerFolder, "轮播图片")
}

// ServeHomeBanner serves the admin preview route and the game server's /local/home-banners/ route.
func ServeHomeBanner(dir string) http.HandlerFunc {
	return serveUploadedImage(dir, homeBannerFolder, homeBannerPathPattern)
}

func (a *API) validateHomeBannerImage(path string) error {
	if !homeBannerPathPattern.MatchString(path) {
		return errors.New("轮播图片路径无效，请重新上传")
	}
	if _, err := os.Stat(filepath.Join(a.homeBannerDir, filepath.Base(path))); err != nil {
		return errors.New("轮播图片文件不存在，请重新上传")
	}
	return nil
}

func validateHomeBanners(policy operations.HomeBannerPolicy) error {
	if len(policy.Items) > 20 {
		return errors.New("首页轮播最多配置20张图片")
	}
	for i, row := range policy.Items {
		if strings.TrimSpace(row.Title) == "" || len([]rune(row.Title)) > 80 || !homeBannerPathPattern.MatchString(row.CoverPath) {
			return fmt.Errorf("第%d张轮播须填写1至80字名称并上传图片", i+1)
		}
		if row.StartUnix < 0 || row.EndUnix < 0 || row.EndUnix > 0 && row.EndUnix <= row.StartUnix {
			return fmt.Errorf("第%d张轮播结束时间须晚于开始时间", i+1)
		}
		switch row.Target {
		case "none", "notice":
			if row.TargetID != 0 {
				return errors.New("仅展示和公告跳转不需要目标ID")
			}
		case "gacha_group":
			if row.TargetID <= 0 {
				return fmt.Errorf("第%d张轮播请选择目标卡池", i+1)
			}
		case "boss_score_rank":
			if row.TargetID <= 0 {
				return fmt.Errorf("第%d张轮播请选择目标Boss", i+1)
			}
		default:
			return fmt.Errorf("第%d张轮播跳转类型无效", i+1)
		}
	}
	return nil
}

// Caller holds configMu, as for pool publication and the policy write.
func (a *API) validateHomeBannerAssets(policy operations.HomeBannerPolicy) error {
	groups := a.operations.homeBannerGachaGroupsLocked()
	for i, row := range policy.Items {
		if err := a.validateHomeBannerImage(row.CoverPath); err != nil {
			return fmt.Errorf("第%d张轮播：%w", i+1, err)
		}
		if row.Target == "gacha_group" {
			if _, ok := groups[row.TargetID]; !ok {
				return fmt.Errorf("第%d张轮播目标卡池组不存在或已删除", i+1)
			}
		} else if row.Target == "boss_score_rank" {
			if _, ok := a.scoreRankingBossesLocked()[row.TargetID]; !ok {
				return fmt.Errorf("第%d张轮播目标Boss未发布计分规则", i+1)
			}
		}
	}
	return nil
}

// The caller holds configMu when publishing a banner. The selector uses the
// same source so a saved target cannot refer to a non-score boss. A boss closed
// to battles stays listed: its banner keeps showing the final standings.
func (a *API) scoreRankingBossesLocked() map[int]string {
	result := map[int]string{}
	content := a.operations.content
	if content == nil {
		return result
	}
	state := content.base
	if content.configuration.Revision != 0 {
		state = content.configuration.State
	}
	for _, profile := range state.TeamBattleRewards {
		id := profile.BossID
		if content.rewardBossID(id) != id {
			continue
		}
		if profile.StageQuestAreaID != 0 || profile.TowerID != 0 || profile.ScorePolicy == nil ||
			profile.ScorePolicy.SourceState != "LOCAL_POLICY_DAMAGE_SCORE" {
			continue
		}
		if boss, ok := content.bosses[id]; ok {
			result[id] = boss.Name
			if boss.Difficulty != "" {
				result[id] += " · " + boss.Difficulty
			}
		}
	}
	return result
}

func (a *API) homeBannerScoreBosses() map[int]string {
	a.operations.configMu.RLock()
	defer a.operations.configMu.RUnlock()
	return a.scoreRankingBossesLocked()
}

func (o *Operations) homeBannerGachaGroupsLocked() map[int]string {
	representatives := map[int]int{}
	names := map[int]string{}
	for id, profile := range o.gachaBases {
		group := profile.GroupID
		if group > 0 && !o.customGachas[id].Deleted && (representatives[group] == 0 || id < representatives[group]) {
			representatives[group], names[group] = id, profile.Name
		}
	}
	for _, config := range o.gachaConfigurations {
		profile := config.Profile
		if representatives[profile.GroupID] == profile.GachaID {
			names[profile.GroupID] = profile.Name
		}
	}
	return names
}

func (a *API) homeBannerGachas() map[int]string {
	a.operations.configMu.RLock()
	defer a.operations.configMu.RUnlock()
	return a.operations.homeBannerGachaGroupsLocked()
}
