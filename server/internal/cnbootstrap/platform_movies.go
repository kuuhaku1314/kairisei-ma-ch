package cnbootstrap

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
)

// Portable movies are independently pinned build outputs, not Unity bundles.
// Only public names enter content identity; private file locations may move.
type platformMovieProfile struct {
	Format         int                     `json:"format"`
	TargetPlatform string                  `json:"target_platform"`
	ArtifactRoot   string                  `json:"artifact_root"`
	Index          platformDeliveryCatalog `json:"index"`
	Files          []platformMovieArtifact `json:"files"`
}
type platformMovieArtifact struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type platformMovieIndex struct {
	Format         int    `json:"format"`
	TargetPlatform string `json:"target_platform"`
	Movies         []struct {
		LogicalName  string `json:"logical_name"`
		SourceBytes  int64  `json:"source_bytes"`
		SourceSHA256 string `json:"source_sha256"`
		USMSHA256    string `json:"usm_sha256"`
		MovieFile    string `json:"movie_file"`
		MovieBytes   int64  `json:"movie_bytes"`
		MovieSHA256  string `json:"movie_sha256"`
	} `json:"movies"`
}
type platformMovieDelivery struct {
	index        []byte
	indexSHA     string
	indexPath    string
	indexRoot    string
	artifactRoot string
	files        map[string]platformDeliveryFile
	identity     map[string]any
}

func pinnedPlatformMovieFile(directory string, ref platformDeliveryCatalog, maximum int64) ([]byte, string, error) {
	filename := ref.File
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(directory, filepath.FromSlash(filename))
	}
	if ref.File == "" || ref.Bytes <= 0 || ref.Bytes > maximum || !contentHashValid(ref.SHA256) {
		return nil, "", errors.New("invalid movie file pin")
	}
	info, err := os.Stat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() != ref.Bytes {
		return nil, "", errors.New("movie file size differs")
	}
	body, err := os.ReadFile(filename)
	if err != nil {
		return nil, "", err
	}
	if fmt.Sprintf("%x", sha256.Sum256(body)) != ref.SHA256 {
		return nil, "", errors.New("movie file hash differs")
	}
	return body, filename, nil
}

func loadPlatformMovies(directory string, ref platformDeliveryCatalog) (*platformMovieDelivery, error) {
	body, filename, err := pinnedPlatformMovieFile(directory, ref, 16<<20)
	if err != nil {
		return nil, err
	}
	var profile platformMovieProfile
	if err = json.Unmarshal(body, &profile); err != nil {
		return nil, err
	}
	if profile.Format != 1 || profile.TargetPlatform != "Portable" || profile.ArtifactRoot == "" || len(profile.Files) == 0 {
		return nil, errors.New("invalid portable movie profile")
	}
	directory = filepath.Dir(filename)
	index, indexPath, err := pinnedPlatformMovieFile(directory, profile.Index, 1<<20)
	if err != nil {
		return nil, err
	}
	var wire platformMovieIndex
	if err = json.Unmarshal(index, &wire); err != nil {
		return nil, err
	}
	if wire.Format != 1 || wire.TargetPlatform != "Portable" || len(wire.Movies) == 0 {
		return nil, errors.New("invalid portable movie index")
	}
	artifactRoot := profile.ArtifactRoot
	if !filepath.IsAbs(artifactRoot) {
		artifactRoot = filepath.Join(directory, filepath.FromSlash(artifactRoot))
	}
	delivery := &platformMovieDelivery{index: index, indexSHA: profile.Index.SHA256, indexPath: indexPath, indexRoot: filepath.Dir(indexPath), artifactRoot: artifactRoot, files: make(map[string]platformDeliveryFile, len(profile.Files))}
	for _, row := range profile.Files {
		if !contentHashValid(row.SHA256) || row.Name != "movies/"+row.SHA256+".mp4" || row.Bytes <= 0 || delivery.files[row.Name].path != "" {
			return nil, errors.New("invalid or duplicate movie registration")
		}
		physical, err := platformRelativeFile(artifactRoot, row.File)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(physical)
		if err != nil || !info.Mode().IsRegular() || info.Size() != row.Bytes {
			return nil, fmt.Errorf("movie artifact size differs: %s", row.Name)
		}
		delivery.files[row.Name] = platformDeliveryFile{path: physical, size: row.Bytes, sha: row.SHA256}
	}
	logical := make(map[string]bool, len(wire.Movies))
	referenced := make(map[string]bool, len(profile.Files))
	for _, row := range wire.Movies {
		file, exists := delivery.files[row.MovieFile]
		if path.Base(row.LogicalName) != row.LogicalName || strings.Contains(row.LogicalName, "\\") || !strings.HasSuffix(row.LogicalName, ".cpk") || logical[row.LogicalName] || row.SourceBytes <= 0 || !contentHashValid(row.SourceSHA256) || !contentHashValid(row.USMSHA256) || !exists || file.size != row.MovieBytes || file.sha != row.MovieSHA256 {
			return nil, errors.New("movie index and registered identities differ")
		}
		logical[row.LogicalName] = true
		referenced[row.MovieFile] = true
	}
	if len(referenced) != len(delivery.files) {
		return nil, errors.New("movie profile has unreferenced artifacts")
	}
	sort.Slice(profile.Files, func(i, j int) bool { return profile.Files[i].Name < profile.Files[j].Name })
	files := make([]map[string]any, 0, len(profile.Files))
	for _, row := range profile.Files {
		files = append(files, map[string]any{"name": row.Name, "bytes": row.Bytes, "sha256": row.SHA256})
	}
	delivery.identity = map[string]any{"index_bytes": profile.Index.Bytes, "index_sha256": profile.Index.SHA256, "files": files}
	return delivery, nil
}

func platformMovieContentID(profile platformDeliveryProfile, movies *platformMovieDelivery) (string, error) {
	bundles := make([]map[string]any, 0, len(profile.Bundles))
	for _, row := range profile.Bundles {
		bundles = append(bundles, map[string]any{"name": row.Name, "bytes": row.Bytes, "sha256": row.SHA256})
	}
	identity := map[string]any{"platform": profile.Platform, "unity_version": profile.UnityVersion, "catalog_sha256": profile.Catalog.SHA256, "bundles": bundles, "movies": movies.identity}
	// Match Python json.dumps(sort_keys=True,separators=(',',':')) exactly,
	// including ensure_ascii, without Go's additional HTML escaping.
	var utf8 bytes.Buffer
	encoder := json.NewEncoder(&utf8)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(identity); err != nil {
		return "", err
	}
	var canonical bytes.Buffer
	for _, value := range strings.TrimSuffix(utf8.String(), "\n") {
		if value < 0x7f {
			canonical.WriteByte(byte(value))
		} else if value <= 0xffff {
			fmt.Fprintf(&canonical, "\\u%04x", value)
		} else {
			hi, lo := utf16.EncodeRune(value)
			fmt.Fprintf(&canonical, "\\u%04x\\u%04x", hi, lo)
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256(canonical.Bytes())), nil
}
