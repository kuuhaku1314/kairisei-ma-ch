package cpk

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

func appendPreloadedCPKCSVRow(manifest *strings.Builder, name string, size int64, downloadType string) {
	cpkType, cpkID := classify(name)
	manifest.WriteString(name)
	manifest.WriteByte(',')
	manifest.WriteString(cpkType)
	manifest.WriteByte(',')
	manifest.WriteString(strconv.Itoa(cpkID))
	manifest.WriteByte(',')
	manifest.WriteString(strconv.FormatFloat(float64(size)/(1024*1024), 'f', 6, 64))
	manifest.WriteByte(',')
	// Omitting this column shifts size_bytes into DOWNLOAD_TYPE and prevents
	// startup audio binding. ALL makes both EASY and FULL sound modes bootstrap
	// a missing file from the local server; already valid device files remain
	// reusable by the original CPK version/size checks.
	manifest.WriteString(downloadType)
	manifest.WriteByte(',')
	manifest.WriteString(strconv.FormatInt(size, 10))
	manifest.WriteByte('\n')
}

func FileList(delivery []File) []byte {
	var manifest strings.Builder
	manifest.WriteString("# cpk_name,type,id,file_size_mb,download_type,size_bytes\n")
	for _, file := range delivery {
		appendPreloadedCPKCSVRow(&manifest, file.Name, file.Size, "ALL")
	}
	return []byte(manifest.String())
}

func classify(name string) (string, int) {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	lower := strings.ToLower(stem)
	switch {
	case lower == "cuesheet_bgm_event":
		return "EVENT_BGM", 0
	case lower == "cuesheet_se_event":
		return "EVENT_SE", 0
	case strings.HasPrefix(lower, "cuesheet_bgm"):
		return "BGM", trailingID(lower, "cuesheet_bgm")
	case strings.HasPrefix(lower, "cuesheet_se"):
		return "SE", trailingID(lower, "cuesheet_se")
	case strings.HasPrefix(lower, "cuesheet_card_"):
		return "CARD_VOICE", trailingID(lower, "cuesheet_card")
	case strings.HasPrefix(lower, "cuesheet_legend_"):
		return "CARD_VOICE", trailingID(lower, "cuesheet_legend")
	case strings.HasPrefix(lower, "cv_arthur_"):
		return "BATTLE_VOICE", trailingID(lower, "cv_arthur")
	case strings.HasPrefix(lower, "cv_tb_"):
		return "TEAMBATTLE_VOICE", trailingID(lower, "cv_tb")
	case strings.HasPrefix(lower, "cv_navi_"):
		return "NAVI_VOICE", trailingID(lower, "cv_navi")
	case strings.HasPrefix(lower, "cv_ep"):
		return "STORY_VOICE", trailingID(lower, "cv_ep")
	case lower == "mov_prologue" || lower == "mov_pv":
		return "MOVIE", 0
	case lower == "mov_op" || lower == "mov_op2":
		return "MOVIE", 1
	case strings.HasPrefix(lower, "mov_"):
		id := trailingID(lower, "mov")
		if id > 0 {
			// Native BattleMgr.playMovie accepts SPHR_MOVIE only. OP and
			// prologue keep the ordinary MOVIE category above.
			return "SPHR_MOVIE", id
		}
		return "MOVIE", 0
	default:
		return "NONE", 0
	}
}

func trailingID(stem string, prefix string) int {
	suffix := strings.TrimPrefix(stem, prefix)
	suffix = strings.TrimPrefix(suffix, "_")
	value, err := strconv.Atoi(suffix)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func PatchState(delivery []File) []byte {
	var state strings.Builder
	for _, file := range delivery {
		fmt.Fprintf(&state, "%s,%d\n", file.Name, file.Version)
	}
	return []byte(state.String())
}
