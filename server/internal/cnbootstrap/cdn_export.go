package cnbootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"kairisei.local/server/internal/cpk"
)

type CDNObject struct {
	Key        string `json:"key"`
	Source     string `json:"source"`
	SourceRoot string `json:"source_root,omitempty"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
	SourcePath string `json:"-"`
}

type cdnObject = CDNObject

type CDNManifest struct {
	ResourceSetSHA256 string               `json:"resource_set_sha256"`
	Files             []CDNObject          `json:"files"`
	SourceRoots       map[string]string    `json:"source_roots,omitempty"`
	PlatformContents  []CDNPlatformContent `json:"platform_contents,omitempty"`
	Root              string               `json:"-"`
}

type CDNPlatformContent struct {
	Platform      string `json:"platform"`
	UnityVersion  string `json:"unity_version"`
	ContentID     string `json:"content_id"`
	ProfileSHA256 string `json:"profile_sha256"`
}

// ReadOnlyRoots includes the independently built platform artifacts. Neither
// exported mappings nor CDN activation config may overwrite any input root.
func (manifest CDNManifest) ReadOnlyRoots() []string {
	roots := []string{manifest.Root}
	for _, root := range manifest.SourceRoots {
		roots = append(roots, root)
	}
	return roots
}

// ExportCDNManifest uses the very same catalog and CPK version/alias builders as
// login downloads. Only delivery payloads are exported, never account or master
// files. File hashes are inherited from the complete verified resource set;
// the sync tool checks the bytes before uploading a missing object.
func ExportCDNManifest(manifestPath, outputPath string, platformProfiles ...string) error {
	if manifestPath == "" || outputPath == "" {
		return errors.New("CDN export requires -resource-set and -export-cdn-manifest")
	}
	result, err := BuildCDNManifest(manifestPath, platformProfiles...)
	if err != nil {
		return err
	}
	outputAbsolute, err := filepath.Abs(outputPath)
	if err != nil {
		return err
	}
	outputParent, err := filepath.EvalSymlinks(filepath.Dir(outputAbsolute))
	if err != nil {
		return err
	}
	outputAbsolute = filepath.Join(outputParent, filepath.Base(outputAbsolute))
	for _, root := range result.ReadOnlyRoots() {
		if relative, err := filepath.Rel(root, outputAbsolute); err == nil && filepath.IsLocal(relative) {
			return errors.New("CDN manifest output must be outside the read-only resource inputs")
		}
	}
	output, err := os.OpenFile(outputAbsolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return errors.Join(encoder.Encode(result), output.Close())
}

// BuildCDNManifest also supplies resolved local paths for the built-in uploader.
// It does not write files, open a database, or start any network listeners.
func BuildCDNManifest(manifestPath string, platformProfiles ...string) (result CDNManifest, err error) {
	if manifestPath == "" || len(platformProfiles) == 0 {
		return result, errors.New("CDN requires a complete resource-set.json and platform delivery profiles")
	}
	absolute, err := filepath.Abs(manifestPath)
	if err != nil {
		return result, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return result, err
	}
	root := filepath.Dir(absolute)
	info, err := os.Stat(absolute)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 32*1024*1024 {
		return result, errors.New("invalid resource-set manifest size")
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return result, err
	}
	var manifest struct {
		Entrypoints map[string]string `json:"entrypoints"`
		Files       []struct {
			Path   string `json:"path"`
			Bytes  int64  `json:"bytes"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return result, err
	}
	inputs := make(map[string]string)
	inputKeys := []string{"cn-cpk-root", "cn-cpk-aliases", "cn-image-root"}
	for _, key := range inputKeys {
		inputs[key], err = cdnSourcePath(root, manifest.Entrypoints[key])
		if err != nil {
			return result, fmt.Errorf("CDN entrypoint %s: %w", key, err)
		}
	}
	indexed := make(map[string]cdnObject, len(manifest.Files))
	for _, file := range manifest.Files {
		if _, exists := indexed[file.Path]; exists {
			return result, fmt.Errorf("duplicate resource file %q", file.Path)
		}
		indexed[file.Path] = cdnObject{Source: file.Path, Bytes: file.Bytes, SHA256: file.SHA256}
	}
	if err := validateCDNMetadata(root, filepath.Join(inputs["cn-cpk-root"], cpk.VersionsFileName), indexed); err != nil {
		return result, err
	}
	if err := validateCDNMetadata(root, filepath.Join(inputs["cn-image-root"], "manifest.json"), indexed); err != nil {
		return result, err
	}
	objects := make(map[string]cdnObject)
	add := func(key, source string, size int64) error {
		relative, err := filepath.Rel(root, source)
		if err != nil {
			return err
		}
		entry, ok := indexed[filepath.ToSlash(relative)]
		if !ok {
			return fmt.Errorf("CDN source is not registered: %s", source)
		}
		resolved, err := cdnSourcePath(root, entry.Source)
		if err != nil {
			return err
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		digest, err := hex.DecodeString(entry.SHA256)
		if err != nil || len(digest) != sha256.Size || !info.Mode().IsRegular() || entry.Bytes <= 0 || entry.Bytes > 5*1024*1024*1024 || info.Size() != entry.Bytes || entry.Bytes != size {
			return fmt.Errorf("CDN resource identity/size mismatch: %s", entry.Source)
		}
		entry.Key = key
		entry.SourcePath = resolved
		if _, exists := objects[key]; exists {
			return fmt.Errorf("duplicate CDN key %q", key)
		}
		objects[key] = entry
		return nil
	}
	sourceRoots := map[string]string{"resources": root}
	platformContents, err := appendPlatformCDN(objects, sourceRoots, platformProfiles)
	if err != nil {
		return result, err
	}
	cpkRoot := inputs["cn-cpk-root"]
	aliases, err := cpk.LoadAliases(cpkRoot, inputs["cn-cpk-aliases"])
	if err != nil {
		return result, err
	}
	cpkDelivery, err := cpk.Load(cpkRoot, aliases)
	if err != nil {
		return result, err
	}
	for _, file := range cpkDelivery {
		if err := add("cpk/CPK/"+file.Name+".v"+strconv.FormatUint(file.Version, 10), file.SourcePath, file.Size); err != nil {
			return result, err
		}
	}
	images, err := loadCardImages(inputs["cn-image-root"])
	if err != nil {
		return result, err
	}
	for _, file := range images.Files {
		key := "image/" + images.Namespace + "/" + file.Path
		if err := add(key, file.SourcePath, file.Bytes); err != nil {
			return result, err
		}
		if objects[key].SHA256 != file.SHA256 {
			return result, fmt.Errorf("card image manifest identity mismatch: %s", file.Path)
		}
	}
	files := make([]cdnObject, 0, len(objects))
	for _, object := range objects {
		if object.SourceRoot == "" {
			object.SourceRoot = "resources"
		}
		files = append(files, object)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Key < files[j].Key })
	digest := sha256.Sum256(content)
	if len(files) == 0 {
		return result, errors.New("empty CDN delivery manifest")
	}
	return CDNManifest{ResourceSetSHA256: hex.EncodeToString(digest[:]), Files: files,
		SourceRoots: sourceRoots, PlatformContents: platformContents, Root: root}, nil
}

func appendPlatformCDN(objects map[string]cdnObject, roots map[string]string, profiles []string) ([]CDNPlatformContent, error) {
	deliveries, err := loadPlatformDeliveries(profiles)
	if err != nil {
		return nil, err
	}
	contents := make([]CDNPlatformContent, 0, len(deliveries))
	for _, delivery := range deliveries {
		identity := delivery.platform + "/" + delivery.unityVersion + "/" + delivery.contentID
		contents = append(contents, CDNPlatformContent{delivery.platform, delivery.unityVersion, delivery.contentID, delivery.profileSHA})
		for name, object := range delivery.objects {
			key := "content/" + identity + "/" + name
			if _, exists := objects[key]; exists {
				return nil, fmt.Errorf("duplicate platform CDN key %q", key)
			}
			sourceRoot := identity + "/" + object.sourceRoot
			roots[sourceRoot] = object.root
			file := object.file
			relative, err := filepath.Rel(object.root, file.path)
			if err != nil || !filepath.IsLocal(relative) {
				return nil, fmt.Errorf("platform CDN artifact escapes its source root: %q", name)
			}
			objects[key] = cdnObject{Key: key, Source: filepath.ToSlash(relative), SourceRoot: sourceRoot, SourcePath: file.path, Bytes: file.size, SHA256: file.sha}
		}
	}
	sort.Slice(contents, func(i, j int) bool {
		return platformKey(contents[i].Platform, contents[i].UnityVersion) < platformKey(contents[j].Platform, contents[j].UnityVersion)
	})
	return contents, nil
}

// Download identities include metadata even when it is not itself downloaded.
func validateCDNMetadata(root, metadata string, indexed map[string]cdnObject) error {
	info, err := os.Stat(metadata)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 8<<20 {
		return errors.New("invalid CDN metadata size")
	}
	relative, err := filepath.Rel(root, metadata)
	if err != nil {
		return err
	}
	entry, exists := indexed[filepath.ToSlash(relative)]
	if !exists {
		return fmt.Errorf("CDN metadata is not registered: %s", metadata)
	}
	resolved, err := cdnSourcePath(root, entry.Source)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(content)
	if int64(len(content)) != entry.Bytes || hex.EncodeToString(digest[:]) != entry.SHA256 {
		return fmt.Errorf("CDN metadata identity mismatch: %s", entry.Source)
	}
	return nil
}

func cdnSourcePath(root, relative string) (string, error) {
	if relative == "" || relative == "." || !filepath.IsLocal(relative) || path.Clean(relative) != relative || strings.ContainsAny(relative, "\\:\r\n") {
		return "", fmt.Errorf("unsafe resource path %q", relative)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("resource escapes root: %q", relative)
	}
	return resolved, nil
}
