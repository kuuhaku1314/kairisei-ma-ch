package cnbootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type cnAssetMapManifest struct {
	Source struct {
		VersionDATSHA256 string `json:"version_dat_sha256"`
	} `json:"source"`
	Bundles       []cnAssetMapBundle `json:"bundles"`
	CatalogAssets []cnAssetMapEntry  `json:"catalog_assets"`
}

type cnAssetMapBundle struct {
	Bundle        string   `json:"bundle"`
	CABName       string   `json:"cab_name"`
	Scrambled     *bool    `json:"scrambled"`
	DeliveryCRC32 string   `json:"delivery_crc32"`
	Dependencies  []string `json:"dependencies"`
}

type cnAssetMapEntry struct {
	Directory string `json:"directory"`
	Name      string `json:"name"`
	BaseDir   string `json:"base_dir"`
	Extension string `json:"extension"`
	Bundle    string `json:"bundle"`
}

type cnCatalogBundleMetadata struct {
	scrambled     bool
	deliveryCRC32 string
	dependencies  []string
}

func loadCNAssetMap(
	assetMapPath string,
	versionDATSHA256 string,
	availableBundles map[string]struct{},
) ([]cnCatalogBundleAsset, map[string]cnCatalogBundleMetadata, error) {
	if assetMapPath == "" {
		return nil, nil, errors.New("official CN asset map path is required")
	}
	absolute, err := filepath.Abs(assetMapPath)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, nil, fmt.Errorf("stat official CN asset map: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return nil, nil, errors.New("official CN asset map is not a non-empty file")
	}
	contents, err := os.ReadFile(absolute)
	if err != nil {
		return nil, nil, fmt.Errorf("read official CN asset map: %w", err)
	}
	var manifest cnAssetMapManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return nil, nil, fmt.Errorf("parse official CN asset map: %w", err)
	}
	if !strings.EqualFold(manifest.Source.VersionDATSHA256, versionDATSHA256) {
		return nil, nil, errors.New("official CN asset map was generated from a different version.dat")
	}
	if len(manifest.Bundles) != len(availableBundles) {
		return nil, nil, errors.New("official CN asset map bundle metadata count differs from available bundles")
	}
	if len(manifest.CatalogAssets) == 0 {
		return nil, nil, errors.New("official CN asset map contains no catalog assets")
	}

	bundleMetadata := make(map[string]cnCatalogBundleMetadata, len(manifest.Bundles))
	for index, bundle := range manifest.Bundles {
		if bundle.Bundle == "" || strings.ContainsAny(bundle.Bundle+bundle.CABName, ",\r\n") ||
			!strings.HasPrefix(strings.ToLower(bundle.CABName), "cab-") || bundle.Scrambled == nil {
			return nil, nil, fmt.Errorf("official CN asset map bundle entry %d is invalid", index)
		}
		if _, available := availableBundles[bundle.Bundle]; !available {
			return nil, nil, fmt.Errorf("official CN asset map bundle %q is not available", bundle.Bundle)
		}
		if _, duplicate := bundleMetadata[bundle.Bundle]; duplicate {
			return nil, nil, fmt.Errorf("official CN asset map repeats bundle metadata %q", bundle.Bundle)
		}
		deliveryCRC32 := strings.ToUpper(strings.TrimSpace(bundle.DeliveryCRC32))
		if deliveryCRC32 != "" {
			if len(deliveryCRC32) != 8 {
				return nil, nil, fmt.Errorf("official CN asset map bundle %q has invalid delivery CRC %q", bundle.Bundle, bundle.DeliveryCRC32)
			}
			if _, parseErr := strconv.ParseUint(deliveryCRC32, 16, 32); parseErr != nil {
				return nil, nil, fmt.Errorf("official CN asset map bundle %q has invalid delivery CRC %q", bundle.Bundle, bundle.DeliveryCRC32)
			}
		}
		seenDependencies := make(map[string]struct{}, len(bundle.Dependencies))
		for _, dependency := range bundle.Dependencies {
			if dependency == bundle.Bundle || strings.ContainsAny(dependency, ",\r\n") {
				return nil, nil, fmt.Errorf("official CN asset map bundle %q has invalid dependency %q", bundle.Bundle, dependency)
			}
			if _, available := availableBundles[dependency]; !available {
				return nil, nil, fmt.Errorf("official CN asset map bundle %q depends on unavailable bundle %q", bundle.Bundle, dependency)
			}
			if _, duplicate := seenDependencies[dependency]; duplicate {
				return nil, nil, fmt.Errorf("official CN asset map bundle %q repeats dependency %q", bundle.Bundle, dependency)
			}
			seenDependencies[dependency] = struct{}{}
		}
		bundleMetadata[bundle.Bundle] = cnCatalogBundleMetadata{
			scrambled:     *bundle.Scrambled,
			deliveryCRC32: deliveryCRC32,
			dependencies:  append([]string(nil), bundle.Dependencies...),
		}

	}

	assets := make([]cnCatalogBundleAsset, 0, len(manifest.CatalogAssets))
	owners := make(map[string]string, len(manifest.CatalogAssets))
	written := make(map[string]struct{}, len(manifest.CatalogAssets))
	for index, entry := range manifest.CatalogAssets {
		if err := validateCNAssetMapEntry(entry, availableBundles); err != nil {
			return nil, nil, fmt.Errorf("official CN asset map entry %d: %w", index, err)
		}
		logicalName := entry.Name
		if entry.Directory != "" {
			logicalName = entry.Directory + "/" + entry.Name
		}
		logicalKey := strings.ToLower(logicalName)
		if previous, exists := owners[logicalKey]; exists && previous != entry.Bundle {
			return nil, nil, fmt.Errorf("official CN asset map logical asset %q has multiple bundle owners", logicalName)
		}
		owners[logicalKey] = entry.Bundle
		baseDir := entry.BaseDir
		if baseDir == "" {
			baseDir = "Assets/Resources/"
		}
		physicalKey := strings.ToLower(baseDir + logicalName + entry.Extension)
		if _, exists := written[physicalKey]; exists {
			return nil, nil, fmt.Errorf("official CN asset map repeats physical asset %q", physicalKey)
		}
		written[physicalKey] = struct{}{}
		// Preserve physical variants and source order. The original FileCatalog
		// accepts both rows and retains the first extension for a logical lookup.
		assets = append(assets, cnCatalogBundleAsset{
			directory: entry.Directory,
			name:      entry.Name,
			baseDir:   entry.BaseDir,
			extension: entry.Extension,
			bundle:    entry.Bundle,
		})
	}
	return assets, bundleMetadata, nil
}

func validateCNAssetMapEntry(entry cnAssetMapEntry, availableBundles map[string]struct{}) error {
	if entry.Directory != "" {
		if path.Clean(entry.Directory) != entry.Directory || path.IsAbs(entry.Directory) || strings.HasPrefix(entry.Directory, "../") {
			return fmt.Errorf("unsafe directory %q", entry.Directory)
		}
	}
	if entry.Name == "" || strings.Contains(entry.Name, "/") {
		return fmt.Errorf("unsafe asset name %q", entry.Name)
	}
	if entry.BaseDir != "" {
		trimmed := strings.TrimSuffix(entry.BaseDir, "/")
		if !strings.HasSuffix(entry.BaseDir, "/") || path.Clean(trimmed) != trimmed ||
			path.IsAbs(trimmed) || strings.HasPrefix(trimmed, "../") ||
			!strings.HasPrefix(strings.ToLower(trimmed), "assets/") {
			return fmt.Errorf("unsafe asset base directory %q", entry.BaseDir)
		}
	}
	if !strings.HasPrefix(entry.Extension, ".") || len(entry.Extension) < 2 || strings.Contains(entry.Extension, "/") {
		return fmt.Errorf("unsafe extension %q", entry.Extension)
	}
	if strings.ContainsAny(entry.Directory+entry.Name+entry.BaseDir+entry.Extension, ",\r\n") {
		return errors.New("asset path contains catalog delimiters")
	}
	if _, available := availableBundles[entry.Bundle]; !available {
		return fmt.Errorf("bundle %q is not available in official patch roots", entry.Bundle)
	}
	return nil
}
