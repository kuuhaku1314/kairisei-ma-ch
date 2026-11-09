package cnbootstrap

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// A stable route needs a content version because clients retain banner bytes on disk.
func gachaBannerVersions(defaultPath string, paths map[string]string) (map[string]string, error) {
	versions := make(map[string]string, len(paths)+1)
	for key, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read CN gacha banner %s version: %w", key, err)
		}
		versions[key] = fmt.Sprintf("%x", sha256.Sum256(content))
	}
	content, err := os.ReadFile(defaultPath)
	if err != nil {
		return nil, fmt.Errorf("read CN default gacha banner version: %w", err)
	}
	versions[""] = fmt.Sprintf("%x", sha256.Sum256(content))
	return versions, nil
}

// Local titles describe the configured pools; archived event artwork retains
// its source filename so it cannot silently serve an unrelated generic pool.
var cn602GachaBannerFiles = map[string]string{
	"historical_duozi":   "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_duozi_png",
	"historical_tianke":  "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_tianke_png",
	"historical_youmo":   "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_youmo_png",
	"historical_youmo10": "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_youmo10_png",

	"local_new_year":        "local_new_year.png",
	"local_standard":        "local_standard.png",
	"local_friend":          "local_friend.png",
	"local_first":           "local_first.png",
	"local_first_multi":     "local_first_multi.png",
	"element_fire":          "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdfire_png",
	"element_ice":           "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdice_png",
	"element_wind":          "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdwindy_png",
	"element_light":         "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuijing_xdlight_png",
	"element_dark":          "https___ma43_gdl_netease_com_web_netease_GONGGAO_20170321_shuixing_xddark_png",
	"rare_ticket_201805":    "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_jixiyou58_png",
	"unowned_ticket_201805": "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_weirushou58_png",
	"lucky_bag_opera":       "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaigeju_png",
	"lucky_bag_skuld":       "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaimo_png",
	"lucky_bag_constantine": "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaidai_png",
	"lucky_bag_merchant":    "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaifu_png",
	"lucky_bag_summer_duo":  "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaiyan_png",
	"lucky_bag_yalin":       "https___ma43_gdl_netease_com_web_netease_GONGGAO_20180409_shuijing_fudaina_png",
}

func resolveCN602BannerPaths(paths map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(cn602GachaBannerFiles)+3)
	for _, key := range []string{"", "five_star_ticket", "home"} {
		if paths[key] == "" {
			return nil, fmt.Errorf("missing resource-set banner %q", key)
		}
		var err error
		result[key], err = resolveBanner(paths[key], key)
		if err != nil {
			return nil, err
		}
	}
	root := filepath.Dir(result[""])
	for key, fileName := range cn602GachaBannerFiles {
		candidate := filepath.Join(root, fileName)
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			return nil, fmt.Errorf("resolve CN gacha banner %s: %w", key, err)
		}
		if filepath.Dir(absolute) != root {
			return nil, fmt.Errorf("CN gacha banner %s escapes the banner root", key)
		}
		if _, err := resolveBanner(absolute, "gacha "+key); err != nil {
			return nil, err
		}
		result[key] = absolute
	}
	return result, nil
}

// Banners are small public images; validate them once during initialization.
func resolveBanner(name, label string) (string, error) {
	const maxBannerBytes = 4 << 20
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", fmt.Errorf("resolve CN %s banner: %w", label, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("stat CN %s banner: %w", label, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBannerBytes {
		return "", fmt.Errorf("CN %s banner must be a non-empty regular file within four MiB", label)
	}
	return absolute, nil
}
