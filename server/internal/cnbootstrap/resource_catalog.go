package cnbootstrap

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"kairisei.local/server/internal/protocol"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func findCNPatchFile(patchRoots []string, relative string) (os.FileInfo, error) {
	for _, root := range patchRoots {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return nil, fmt.Errorf("official CN bundle %q is not a non-empty file", relative)
		}
		// Match select_bundle in the offline source catalog generator:
		// earlier roots own duplicate paths, including different-sized official
		// variants. A shadowed copy must not change the catalog's selected size.
		return info, nil
	}
	return nil, os.ErrNotExist
}

func resolveCNPatchRoots(patchRoots []string) ([]string, error) {
	if len(patchRoots) == 0 {
		return nil, errors.New("at least one official CN patch root is required")
	}
	resolved := make([]string, 0, len(patchRoots))
	seen := make(map[string]struct{}, len(patchRoots))
	var versionNamespace string
	for _, patchRoot := range patchRoots {
		if patchRoot == "" {
			return nil, errors.New("official CN patch root is required")
		}
		absolute, err := filepath.Abs(patchRoot)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(filepath.Clean(absolute))
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("duplicate official CN patch root %q", absolute)
		}
		seen[key] = struct{}{}
		info, err := os.Stat(absolute)
		if err != nil {
			return nil, fmt.Errorf("stat official CN patch root: %w", err)
		}
		if !info.IsDir() {
			return nil, errors.New("official CN patch root is not a directory")
		}
		versionBytes, err := os.ReadFile(filepath.Join(absolute, "version.dat"))
		if err != nil {
			return nil, fmt.Errorf("read official CN version.dat: %w", err)
		}
		namespace, err := cnPatchVersionNamespace(versionBytes)
		if err != nil {
			return nil, fmt.Errorf("official CN patch root %q: %w", absolute, err)
		}
		if versionNamespace == "" {
			versionNamespace = namespace
		} else if versionNamespace != namespace {
			return nil, fmt.Errorf("official CN patch root %q has a different ordered version.dat bundle namespace", absolute)
		}
		resolved = append(resolved, absolute)
	}
	return resolved, nil
}

// Parallel official captures have the same version and ordered bundle namespace
// but different historical CRCs. The first root stays the catalog authority;
// the generated asset map supplies the CRC of the selected delivery bytes.
// Comparing whole version.dat files incorrectly rejects the verified CN variant.
func cnPatchVersionNamespace(encoded []byte) (string, error) {
	decoded := bytes.Clone(encoded)
	for index := range decoded {
		decoded[index] -= cn602ScrambleKey[index%len(cn602ScrambleKey)]
	}
	reader := csv.NewReader(bytes.NewReader(decoded))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return "", fmt.Errorf("parse official CN version.dat: %w", err)
	}
	foundVersion := false
	names := make([]string, 0)
	seen := make(map[string]struct{})
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		switch strings.TrimSpace(row[0]) {
		case "<version>":
			if len(row) != 2 || foundVersion || strings.TrimSpace(row[1]) != strconv.Itoa(protocol.CatalogRevision) {
				return "", errors.New("official CN version.dat has an invalid or duplicate version row")
			}
			foundVersion = true
		case "<bundle_ver>":
			if len(row) != 4 {
				return "", errors.New("official CN version.dat has an invalid bundle row")
			}
			name := strings.TrimSpace(row[1])
			if name != path.Clean(name) || name == "." || strings.HasPrefix(name, "../") || path.IsAbs(name) || strings.ContainsAny(name, "\\:,\r\n\x00") {
				return "", fmt.Errorf("unsafe official CN bundle path %q", name)
			}
			if _, exists := seen[strings.ToLower(name)]; exists {
				return "", fmt.Errorf("duplicate official CN bundle path %q", name)
			}
			if _, err := strconv.ParseUint(strings.TrimSpace(row[3]), 16, 32); err != nil {
				return "", fmt.Errorf("invalid official CN bundle CRC for %q", name)
			}
			seen[strings.ToLower(name)] = struct{}{}
			names = append(names, name)
		}
	}
	if !foundVersion || len(names) == 0 {
		return "", errors.New("official CN version.dat has no version or bundles")
	}
	return strings.Join(names, "\x00"), nil
}

// BuildCN602CatalogForAudit runs the same patch-root and catalog construction
// path used by the server without starting a listener. Operational tooling
// uses it to validate staged overlays before they are eligible for a runtime
// profile.
func BuildCN602CatalogForAudit(patchRoots []string, assetMapPath string) ([]byte, error) {
	resolved, err := resolveCNPatchRoots(patchRoots)
	if err != nil {
		return nil, err
	}
	return buildCN602Catalog(resolved, assetMapPath)
}

func buildCN602Catalog(patchRoots []string, assetMapPath string) ([]byte, error) {
	versionBytes, err := os.ReadFile(filepath.Join(patchRoots[0], "version.dat"))
	if err != nil {
		return nil, fmt.Errorf("read official CN version.dat: %w", err)
	}
	versionDATDigest := sha256.Sum256(versionBytes)
	versionDATSHA256 := hex.EncodeToString(versionDATDigest[:])
	for index := range versionBytes {
		versionBytes[index] -= cn602ScrambleKey[index%len(cn602ScrambleKey)]
	}
	versionReader := csv.NewReader(bytes.NewReader(versionBytes))
	versionReader.FieldsPerRecord = -1
	rows, err := versionReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse official CN version.dat: %w", err)
	}

	var catalog strings.Builder
	fmt.Fprintf(&catalog, "<v>,%d\n", protocol.CatalogRevision)
	foundVersion := false
	foundMenu := false
	bundleCount := 0
	availableBundles := make(map[string]struct{})
	bundleRows := make([]cnCatalogBundleRow, 0)
	for _, row := range rows {
		if len(row) == 2 && strings.TrimSpace(row[0]) == "<version>" {
			version, parseErr := strconv.Atoi(strings.TrimSpace(row[1]))
			if parseErr != nil || version != protocol.CatalogRevision {
				return nil, fmt.Errorf("official CN version.dat has unexpected version %q", row[1])
			}
			foundVersion = true
			continue
		}
		if len(row) != 4 || strings.TrimSpace(row[0]) != "<bundle_ver>" {
			continue
		}
		bundleName := strings.TrimSpace(row[1])
		cleanName := path.Clean(bundleName)
		if cleanName != bundleName || cleanName == "." || strings.HasPrefix(cleanName, "../") || path.IsAbs(cleanName) || strings.ContainsAny(cleanName, ",\r\n") {
			return nil, fmt.Errorf("unsafe official CN bundle path %q", bundleName)
		}
		patchCRC := strings.ToUpper(strings.TrimSpace(row[3]))
		if _, parseErr := strconv.ParseUint(patchCRC, 16, 32); parseErr != nil {
			return nil, fmt.Errorf("invalid official CN bundle CRC %q for %q", patchCRC, bundleName)
		}
		bundleInfo, statErr := findCNPatchFile(patchRoots, bundleName)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return nil, fmt.Errorf("stat official CN bundle %q: %w", bundleName, statErr)
		}
		if !bundleInfo.Mode().IsRegular() || bundleInfo.Size() <= 0 {
			return nil, fmt.Errorf("official CN bundle %q is not a non-empty file", bundleName)
		}
		bundleRows = append(bundleRows, cnCatalogBundleRow{
			name:     bundleName,
			patchCRC: patchCRC,
			size:     bundleInfo.Size(),
		})
		bundleCount++
		availableBundles[bundleName] = struct{}{}
		if strings.EqualFold(bundleName, cn602MenuBundle) {
			foundMenu = true
		}
	}
	if !foundVersion {
		return nil, errors.New("official CN version.dat has no version row")
	}
	if bundleCount == 0 {
		return nil, errors.New("official CN patch root has no versioned bundle files")
	}
	if !foundMenu {
		return nil, errors.New("official CN patch root is missing the Menu bundle")
	}
	assetMap, bundleMetadata, err := loadCNAssetMap(assetMapPath, versionDATSHA256, availableBundles)
	if err != nil {
		return nil, err
	}
	for _, bundle := range bundleRows {
		metadata := bundleMetadata[bundle.name]
		deliveryCRC32 := bundle.patchCRC
		if metadata.deliveryCRC32 != "" {
			deliveryCRC32 = metadata.deliveryCRC32
		}
		storageFlag := 0
		if metadata.scrambled {
			storageFlag = 1
		}
		// BundleInfo.bundle_crc is parsed but never consumed by the CN 6.0.2
		// managed client. The delivery CRC is authoritative for local-file identity
		// and update URLs. Official bundles inherit it from version.dat; a validated
		// overlay may declare the CRC of its replacement delivery bytes in the asset
		// map. Field 8 selects the original client's storage
		// path: scrambled files are decoded and loaded from memory, while ordinary
		// UnityFS/UnityRaw/UnityWeb files must be loaded directly from disk.
		//
		// Field 6 is BundleSettings.category.  Category 0 is synchronously fetched
		// by Title.onRecvPatchList before Connect and has no DownloadFirst UI;
		// category 1 is handed to IntroMgr.downloadC0, where the original Intro
		// scene owns the sound choice, total-size prompt and both progress bars.
		// The CN source version.dat does not preserve the retired service's category
		// table (its third column is 0 for every row), so the local full-data profile
		// deliberately publishes all official bundles as initial-download category
		// 1.  This preserves the original clean-install state machine instead of
		// silently transferring the whole data set behind the Title waiting layer.
		fmt.Fprintf(
			&catalog,
			"<b>,%s,%s,00000000,%d,N,1,0,%d",
			bundle.name,
			deliveryCRC32,
			bundle.size,
			storageFlag,
		)
		for _, dependency := range metadata.dependencies {
			fmt.Fprintf(&catalog, ",%s", dependency)
		}
		catalog.WriteByte('\n')
	}
	// The generated official container table is authoritative for extension and
	// bundle ownership. Its logical names are lowercased by the CN bundles; the
	// targeted FileCatalog compatibility patch handles caller casing. There are
	// no handwritten catalog rows: every asset owner comes from this manifest.
	for _, asset := range assetMap {
		fmt.Fprintf(
			&catalog,
			"<a>,%s,%s,%s,%s,%s\n",
			asset.directory,
			asset.name,
			asset.baseDir,
			asset.extension,
			asset.bundle,
		)
	}

	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	_, writeErr := zipper.Write([]byte(catalog.String()))
	closeErr := zipper.Close()
	if writeErr != nil || closeErr != nil {
		return nil, errors.New("compress CN catalog")
	}
	return compressed.Bytes(), nil
}

type cnCatalogBundleAsset struct {
	directory string
	name      string
	baseDir   string
	extension string
	bundle    string
}

type cnCatalogBundleRow struct {
	name     string
	patchCRC string
	size     int64
}
