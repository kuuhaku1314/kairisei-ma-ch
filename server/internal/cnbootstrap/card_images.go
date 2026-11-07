package cnbootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
)

var cardImagePath = regexp.MustCompile(`^chr51/chr51_[0-9]{8}\.png$`)

type cardImage struct {
	Path       string `json:"path"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
	SourcePath string `json:"-"`
}

// The immutable manifest versions the URL as well as the CDN objects. The CN
// client caches enlarged illustrations by URL, independently of patch bundles.
type cardImages struct {
	Namespace string
	Files     map[string]cardImage
}

func loadCardImages(root string) (cardImages, error) {
	result := cardImages{Files: make(map[string]cardImage)}
	if root == "" {
		return result, fmt.Errorf("card image root is required")
	}
	metadata := filepath.Join(root, "manifest.json")
	info, err := os.Stat(metadata)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 8<<20 {
		return result, fmt.Errorf("invalid card image manifest size")
	}
	content, err := os.ReadFile(metadata)
	if err != nil {
		return result, err
	}
	var manifest struct {
		SchemaVersion int         `json:"schema_version"`
		Files         []cardImage `json:"files"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return result, err
	}
	if manifest.SchemaVersion != 1 || len(manifest.Files) == 0 {
		return result, fmt.Errorf("invalid card image manifest")
	}
	for _, file := range manifest.Files {
		_, duplicate := result.Files[file.Path]
		digest, err := hex.DecodeString(file.SHA256)
		if duplicate || !cardImagePath.MatchString(file.Path) || err != nil || len(digest) != sha256.Size || file.Bytes <= 0 || file.Bytes > 32<<20 {
			return result, fmt.Errorf("invalid card image identity: %s", file.Path)
		}
		file.SourcePath, err = cdnSourcePath(root, file.Path)
		if err != nil {
			return result, err
		}
		stream, err := os.Open(file.SourcePath)
		if err != nil {
			return result, err
		}
		info, statErr := stream.Stat()
		config, decodeErr := png.DecodeConfig(stream)
		_ = stream.Close()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != file.Bytes || decodeErr != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 {
			return result, fmt.Errorf("invalid card image payload: %s", file.Path)
		}
		result.Files[file.Path] = file
	}
	digest := sha256.Sum256(content)
	result.Namespace = hex.EncodeToString(digest[:16])
	return result, nil
}

func (images cardImages) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	name, ok := strings.CutPrefix(chi.URLParam(request, "*"), images.Namespace+"/")
	file, exists := images.Files[name]
	if !ok || !exists {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "image/png")
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writer.Header().Set("ETag", `"`+file.SHA256+`"`)
	allowResourceTransfer(writer, file.Bytes)
	http.ServeFile(writer, request, file.SourcePath)
}
