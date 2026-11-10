package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kairisei.local/server/internal/cnbootstrap"
)

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("value must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func digest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func writeEvidence(path string, projectRoot string, payload []byte) error {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return err
	}
	evidenceRoot := filepath.Join(root, "_local", "evidence")
	output, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(evidenceRoot, output)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("output must be a JSON file under %s", evidenceRoot)
	}
	if !strings.EqualFold(filepath.Ext(output), ".json") {
		return errors.New("output must have a .json extension")
	}
	if _, err := os.Stat(output); err == nil {
		return fmt.Errorf("output already exists: %s", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(output), filepath.Base(output)+".*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, output)
}

func run() error {
	flags := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var patchRoots stringList
	var requiredBundles stringList
	var assetMap string
	var projectRoot string
	var output string
	flags.Var(&patchRoots, "cn-patch-root", "ordered CN patch root; repeat for overlays")
	flags.Var(&requiredBundles, "require-bundle", "bundle which must have both catalog bundle and asset rows")
	flags.StringVar(&assetMap, "cn-asset-map", "", "generated CN logical asset map")
	flags.StringVar(&projectRoot, "project-root", "", "repository root; required with -output")
	flags.StringVar(&output, "output", "", "optional JSON evidence path under _local/evidence")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if len(patchRoots) == 0 {
		return errors.New("at least one -cn-patch-root is required")
	}
	if strings.TrimSpace(assetMap) == "" {
		return errors.New("-cn-asset-map is required")
	}
	if output != "" && strings.TrimSpace(projectRoot) == "" {
		return errors.New("-project-root is required with -output")
	}

	compressed, err := cnbootstrap.BuildCN602CatalogForAudit([]string(patchRoots), assetMap)
	if err != nil {
		return err
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("open generated catalog: %w", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read generated catalog: %w", err)
	}
	if err := reader.Close(); err != nil {
		return fmt.Errorf("close generated catalog: %w", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(plain), "\n"), "\n")
	bundleCount := 0
	assetCount := 0
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "<b>,"):
			bundleCount++
		case strings.HasPrefix(line, "<a>,"):
			assetCount++
		}
	}
	requirements := make(map[string]bool, len(requiredBundles))
	for _, bundle := range requiredBundles {
		hasBundle := bytes.Contains(plain, []byte("<b>,"+bundle+","))
		hasAsset := bytes.Contains(plain, []byte(","+bundle+"\n"))
		requirements[bundle] = hasBundle && hasAsset
		if !requirements[bundle] {
			return fmt.Errorf("required bundle is not closed in generated catalog: %s", bundle)
		}
	}
	result := struct {
		State              string          `json:"state"`
		Evidence           string          `json:"evidence"`
		GeneratedUTC       string          `json:"generated_utc"`
		ReadOnly           bool            `json:"read_only"`
		PatchRootCount     int             `json:"patch_root_count"`
		BundleCount        int             `json:"bundle_count"`
		AssetCount         int             `json:"asset_count"`
		CompressedBytes    int             `json:"compressed_bytes"`
		CompressedSHA256   string          `json:"compressed_sha256"`
		UncompressedBytes  int             `json:"uncompressed_bytes"`
		UncompressedSHA256 string          `json:"uncompressed_sha256"`
		RequiredBundles    map[string]bool `json:"required_bundles"`
	}{
		State:              "PASS",
		Evidence:           "CONFIRMED",
		GeneratedUTC:       time.Now().UTC().Format(time.RFC3339Nano),
		ReadOnly:           true,
		PatchRootCount:     len(patchRoots),
		BundleCount:        bundleCount,
		AssetCount:         assetCount,
		CompressedBytes:    len(compressed),
		CompressedSHA256:   digest(compressed),
		UncompressedBytes:  len(plain),
		UncompressedSHA256: digest(plain),
		RequiredBundles:    requirements,
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if output != "" {
		if err := writeEvidence(output, projectRoot, payload); err != nil {
			return err
		}
	}
	_, err = os.Stdout.Write(payload)
	return err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
