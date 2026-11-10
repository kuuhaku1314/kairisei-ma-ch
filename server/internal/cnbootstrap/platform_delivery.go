package cnbootstrap

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Platform profiles are operator-owned build outputs. Their private filesystem
// paths are never sent to the player; requests select only declared bundles.
type platformDeliveryProfile struct {
	Platform     string                   `json:"platform"`
	UnityVersion string                   `json:"unity_version"`
	ContentID    string                   `json:"content_id"`
	ArtifactRoot string                   `json:"artifact_root"`
	Catalog      platformDeliveryCatalog  `json:"catalog"`
	Bundles      []platformDeliveryBundle `json:"bundles"`
	Packs        []platformDeliveryPack   `json:"packs,omitempty"`
	Movies       *platformDeliveryCatalog `json:"movies,omitempty"`
}

type platformDeliveryPack struct {
	Name    string   `json:"name"`
	Bytes   int64    `json:"bytes"`
	SHA256  string   `json:"sha256"`
	Members []string `json:"members"`
}

type platformDeliveryCatalog struct {
	File   string `json:"file"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type platformDeliveryBundle struct {
	Name   string `json:"name"`
	File   string `json:"file,omitempty"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	CRC32  string `json:"crc32"`
}

type platformDeliveryFile struct {
	path string
	size int64
	crc  string
	sha  string
}

func contentHashValid(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

func platformRelativeFile(root, name string) (string, error) {
	if name == "" || name == "." || name == ".." || path.Clean(name) != name || path.IsAbs(name) ||
		strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\:\x00\r\n") {
		return "", fmt.Errorf("invalid platform content path %q", name)
	}
	return filepath.Join(root, filepath.FromSlash(name)), nil
}

func loadPlatformDeliveries(profiles []string) (platformDeliveries, error) {
	if len(profiles) == 0 {
		return nil, errors.New("at least one platform delivery profile is required")
	}
	result := make(platformDeliveries, len(profiles))
	for _, profilePath := range profiles {
		delivery, err := loadPlatformDelivery(profilePath)
		if err != nil {
			return nil, fmt.Errorf("platform delivery %q: %w", profilePath, err)
		}
		key := platformKey(delivery.platform, delivery.unityVersion)
		if result[key] != nil {
			return nil, errors.New("duplicate platform and Unity delivery profile")
		}
		result[key] = delivery
	}
	return result, nil
}

func loadPlatformDelivery(profilePath string) (*platformDelivery, error) {
	absolute, err := filepath.Abs(profilePath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 16<<20 {
		return nil, errors.New("platform delivery profile is absent or outside its size bound")
	}
	raw, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	var profile platformDeliveryProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return nil, err
	}
	if (profile.Platform != "StandaloneWindows64" && profile.Platform != "Android") ||
		profile.UnityVersion == "" || strings.ContainsAny(profile.UnityVersion, "/\\:\x00\r\n") ||
		!contentHashValid(profile.ContentID) || profile.ArtifactRoot == "" || len(profile.Bundles) == 0 || len(profile.Bundles) > 16384 {
		return nil, errors.New("invalid platform delivery identity")
	}
	directory := filepath.Dir(absolute)
	artifactRoot := profile.ArtifactRoot
	if !filepath.IsAbs(artifactRoot) {
		artifactRoot = filepath.Join(directory, filepath.FromSlash(artifactRoot))
	}
	artifactRoot, err = filepath.Abs(artifactRoot)
	if err != nil {
		return nil, err
	}
	catalogPath, err := platformRelativeFile(directory, profile.Catalog.File)
	if err != nil {
		return nil, err
	}
	if profile.Catalog.Bytes <= 0 || profile.Catalog.Bytes > 32<<20 || !contentHashValid(profile.Catalog.SHA256) {
		return nil, errors.New("invalid platform catalog identity")
	}
	info, err = os.Stat(catalogPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() != profile.Catalog.Bytes {
		return nil, errors.New("platform catalog size differs")
	}
	catalog, err := os.ReadFile(catalogPath)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(catalog)
	if hex.EncodeToString(digest[:]) != profile.Catalog.SHA256 {
		return nil, errors.New("platform catalog hash differs")
	}
	delivery := &platformDelivery{
		platform: profile.Platform, unityVersion: profile.UnityVersion, contentID: profile.ContentID,
		profileSHA: fmt.Sprintf("%x", sha256.Sum256(raw)),
		objects:    make(map[string]platformContentObject, 1+len(profile.Bundles)+len(profile.Packs)),
	}
	delivery.objects["patch/catalog.dat"] = platformContentObject{
		file:       platformDeliveryFile{path: catalogPath, size: profile.Catalog.Bytes, sha: profile.Catalog.SHA256},
		sourceRoot: "delivery", root: directory, contentType: "application/octet-stream", data: catalog,
	}
	realFiles := make(map[string]platformDeliveryFile, len(profile.Bundles))
	for _, row := range profile.Bundles {
		if _, err := platformRelativeFile(artifactRoot, row.Name); err != nil {
			return nil, err
		}
		if row.Bytes <= 0 || !contentHashValid(row.SHA256) || len(row.CRC32) != 8 || realFiles[row.Name].size != 0 {
			return nil, fmt.Errorf("invalid or duplicate platform bundle %q", row.Name)
		}
		if _, err := strconv.ParseUint(row.CRC32, 16, 32); err != nil {
			return nil, fmt.Errorf("invalid platform bundle CRC %q", row.Name)
		}
		file := platformDeliveryFile{size: row.Bytes, crc: strings.ToUpper(row.CRC32), sha: row.SHA256}
		if row.File != "" {
			file.path, err = platformRelativeFile(artifactRoot, row.File)
			if err != nil {
				return nil, err
			}
			info, err := os.Stat(file.path)
			if err != nil || !info.Mode().IsRegular() || info.Size() != row.Bytes {
				return nil, fmt.Errorf("platform bundle size differs: %q", row.Name)
			}
			delivery.objects["patch/"+row.Name+".v"+file.crc] = platformContentObject{
				file: file, sourceRoot: "artifacts", root: artifactRoot, contentType: "application/octet-stream",
			}
		}
		realFiles[row.Name] = file
	}
	// Build tooling verifies every payload SHA. At server startup the catalog's
	// pinned bytes and exact CRC/size inventory suffice; do not rehash gigabytes
	// of immutable bundles or maintain a second resource snapshot.
	entries, err := cnPatchDeliveryFromCatalog(catalog)
	if err != nil || len(entries) != len(realFiles) {
		return nil, errors.New("platform catalog bundle inventory differs")
	}
	for _, entry := range entries {
		file, exists := realFiles[entry.Name]
		if !exists || file.size != entry.Size || !strings.EqualFold(file.crc, entry.CRC) {
			return nil, fmt.Errorf("platform catalog bundle metadata differs: %q", entry.Name)
		}
	}
	wirePacks, err := platformPacksFromCatalog(catalog)
	if err != nil || len(wirePacks) != len(profile.Packs) {
		return nil, errors.New("platform catalog pack inventory differs")
	}
	members := make(map[string]bool)
	for _, row := range profile.Packs {
		wire, exists := wirePacks[row.Name]
		if !exists || wire.Bytes != row.Bytes || wire.SHA256 != row.SHA256 || !slices.Equal(wire.Members, row.Members) || delivery.objects["patch/"+row.Name].file.path != "" ||
			!strings.HasPrefix(row.Name, "packs/") || !strings.HasSuffix(row.Name, "-"+row.SHA256+".zip") || !contentHashValid(row.SHA256) || len(row.Members) < 2 {
			return nil, fmt.Errorf("invalid platform pack metadata %q", row.Name)
		}
		var memberBytes int64
		for _, name := range row.Members {
			file, exists := realFiles[name]
			// Packing thresholds belong to build policy, not a second runtime
			// classification of otherwise valid catalog owners.
			if !exists || members[name] || file.path != "" {
				return nil, fmt.Errorf("platform owner is unknown, multiply packed or also standalone: %q", name)
			}
			members[name] = true
			memberBytes += file.size
		}
		file, err := platformRelativeFile(directory, row.Name)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(file)
		if err != nil || !info.Mode().IsRegular() || info.Size() != row.Bytes || row.Bytes <= memberBytes {
			return nil, fmt.Errorf("platform pack size differs: %q", row.Name)
		}
		if err := verifyPlatformPackInventory(file, row.Members, realFiles); err != nil {
			return nil, fmt.Errorf("platform pack %q: %w", row.Name, err)
		}
		delivery.objects["patch/"+row.Name] = platformContentObject{
			file:       platformDeliveryFile{path: file, size: row.Bytes, sha: row.SHA256},
			sourceRoot: "delivery", root: directory, contentType: "application/octet-stream",
		}
	}
	for name, file := range realFiles {
		if file.path == "" && !members[name] {
			return nil, fmt.Errorf("platform owner has no delivery source: %q", name)
		}
	}
	if profile.Movies != nil {
		movies, err := loadPlatformMovies(directory, *profile.Movies)
		if err != nil {
			return nil, err
		}
		expected, err := platformMovieContentID(profile, movies)
		if err != nil {
			return nil, err
		}
		if expected != profile.ContentID {
			return nil, errors.New("platform content ID does not bind its movie index")
		}
		delivery.registerMovies(movies)
	}
	delivery.bundles = realFiles
	return delivery, nil
}

// Read the ZIP directory, not all payloads, to ensure no undeclared duplicate
// or stale file is hidden in a transport container. Full SHA is verified by
// the builder/uploader and client before extraction.
func verifyPlatformPackInventory(filename string, members []string, files map[string]platformDeliveryFile) error {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer archive.Close()
	if len(archive.File) != len(members) {
		return errors.New("archive and declared member sets differ")
	}
	expected := make(map[string]bool, len(members))
	for _, name := range members {
		expected[name] = true
	}
	for _, entry := range archive.File {
		file, exists := files[entry.Name]
		if !exists || !expected[entry.Name] || entry.Method != zip.Store || entry.Flags&1 != 0 ||
			entry.UncompressedSize64 != uint64(file.size) || entry.CompressedSize64 != entry.UncompressedSize64 ||
			fmt.Sprintf("%08X", entry.CRC32) != file.crc {
			return fmt.Errorf("archive member differs from real-file manifest: %q", entry.Name)
		}
		delete(expected, entry.Name)
	}
	return nil
}

func platformPacksFromCatalog(catalog []byte) (map[string]platformDeliveryPack, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(catalog))
	if err != nil {
		return nil, err
	}
	defer compressed.Close()
	reader := csv.NewReader(compressed)
	reader.FieldsPerRecord = -1
	packs := make(map[string]platformDeliveryPack)
	for {
		row, err := reader.Read()
		if err == io.EOF {
			return packs, nil
		}
		if err != nil {
			return nil, err
		}
		if len(row) == 0 || row[0] != "<p>" {
			continue
		}
		if len(row) < 6 || packs[row[1]].Name != "" {
			return nil, errors.New("invalid or duplicate platform pack catalog row")
		}
		size, err := strconv.ParseInt(row[3], 10, 64)
		if err != nil || size <= 0 {
			return nil, errors.New("invalid platform pack catalog size")
		}
		packs[row[1]] = platformDeliveryPack{Name: row[1], SHA256: row[2], Bytes: size, Members: row[4:]}
	}
}
