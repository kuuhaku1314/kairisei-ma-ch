package cnbootstrap

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// The five slots belong to the original Intro downloader. Optional local
// presentation files live alongside other Web images, not in client bundles.
func newCNIntroHandler(gachaBannerPath string) (http.HandlerFunc, error) {
	directory := filepath.Join(filepath.Dir(gachaBannerPath), "local_intro")
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		return cnBootstrapIntroPlaceholder, nil
	} else if err != nil {
		return nil, err
	}
	var slides [5][]byte
	var tags [5]string
	for index := range slides {
		name := filepath.Join(directory, fmt.Sprintf("Intro_%d.png", index))
		info, err := os.Stat(name)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 4*1024*1024 {
			return nil, fmt.Errorf("invalid Intro image: %s", name)
		}
		content, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		config, err := png.DecodeConfig(bytes.NewReader(content))
		if err != nil || config.Width != 1280 || config.Height != 640 {
			return nil, fmt.Errorf("Intro image must be a 1280x640 PNG: %s", name)
		}
		slides[index] = content
		tags[index] = fmt.Sprintf(`"%x"`, sha256.Sum256(content))
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		index, err := strconv.Atoi(chi.URLParam(request, "index"))
		if err != nil || index < 0 || index >= len(slides) {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "image/png")
		writer.Header().Set("Cache-Control", "no-cache")
		writer.Header().Set("ETag", tags[index])
		writer.Header().Set("X-Kairisei-Presentation", "local-game-guide")
		http.ServeContent(writer, request, fmt.Sprintf("Intro_%d.png", index), time.Time{}, bytes.NewReader(slides[index]))
	}, nil
}
