package admin

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/objectstore"
)

// 运营设置 › 上传图片 › 同步到存储容器 copies the uploaded home-banner and pool-cover images that the current
// configuration references to the storage container saved on the same page. Each object keeps its
// relative path (home-banners/<sha256>.png, gacha-covers/<sha256>.jpg) below the container address,
// so switching the image source changes no reference. Existing objects are skipped, a different object under the
// same name is never overwritten, and nothing in storage is deleted.

//go:embed web/admin_image_sync.js
var adminImageSyncJS []byte

const imageSyncKey = "image-sync"

type imageSyncUse struct {
	Label string `json:"label"`
	Note  string `json:"note"`
	View  string `json:"view"`
}

type imageSyncImage struct {
	Path  string         `json:"path"`
	Kind  string         `json:"kind"` // "home" or "gacha"
	Bytes int64          `json:"bytes"`
	Local bool           `json:"local"`
	Uses  []imageSyncUse `json:"uses"`
}

// Remote is present, missing, conflict, uploaded or error; Public is ok or failed once the object
// is in storage.
type imageSyncResult struct {
	Remote string `json:"remote"`
	Public string `json:"public,omitempty"`
	Error  string `json:"error,omitempty"`
}

type imageSyncJob struct {
	Mode        string                     `json:"mode"` // "check" or "sync"
	State       string                     `json:"state"`
	Digest      string                     `json:"digest"`
	BaseURL     string                     `json:"base_url"`
	Total       int                        `json:"total"`
	Done        int                        `json:"done"`
	StartedUTC  string                     `json:"started_utc"`
	FinishedUTC string                     `json:"finished_utc,omitempty"`
	AuditError  string                     `json:"audit_error,omitempty"`
	Results     map[string]imageSyncResult `json:"results"`
}

// One check or sync runs at a time; the last job stays in memory for the page and is lost on restart.
type imageSyncState struct {
	mu     sync.Mutex
	job    *imageSyncJob
	cancel context.CancelFunc
	done   chan struct{}
	closed bool
}

func (s *imageSyncState) snapshot() *imageSyncJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job == nil {
		return nil
	}
	job := *s.job
	job.Results = make(map[string]imageSyncResult, len(s.job.Results))
	for key, result := range s.job.Results {
		job.Results[key] = result
	}
	return &job
}

// ready reports whether the job proved every configured image readable at the public address.
func (job *imageSyncJob) ready(digest, base string) bool {
	if job == nil || job.State != "complete" || job.Digest != digest || job.BaseURL != base || len(job.Results) != job.Total {
		return false
	}
	for _, result := range job.Results {
		if (result.Remote != "present" && result.Remote != "uploaded") || result.Public != "ok" {
			return false
		}
	}
	return true
}

// configuredImagesLocked lists every uploaded image the saved configuration refers to: all home
// banner rows, and the covers of published and draft pools that are not in the recycle bin. The
// caller holds configMu.
func (a *API) configuredImagesLocked() ([]imageSyncImage, error) {
	o := a.operations
	images := []imageSyncImage{}
	index := map[string]int{}
	add := func(kind, file string, use imageSyncUse) {
		pattern, dir := homeBannerPathPattern, a.homeBannerDir
		if kind == "gacha" {
			pattern, dir = gachaCoverPathPattern, a.gachaCoverDir
		}
		if !pattern.MatchString(file) {
			return
		}
		i, ok := index[file]
		if !ok {
			i, index[file] = len(images), len(images)
			image := imageSyncImage{Path: file, Kind: kind}
			if info, err := os.Stat(filepath.Join(dir, filepath.Base(file))); err == nil && info.Mode().IsRegular() {
				image.Local, image.Bytes = true, info.Size()
			}
			images = append(images, image)
		}
		for _, existing := range images[i].Uses {
			if existing == use {
				return
			}
		}
		images[i].Uses = append(images[i].Uses, use)
	}
	now := time.Now().Unix()
	if p := o.playerPolicy.Load(); p != nil {
		banners := p.Value.HomeBanners
		for _, row := range banners.Items {
			note := "展示中"
			switch {
			case !banners.Custom:
				note = "未启用自定义轮播"
			case !row.Enabled:
				note = "已停用"
			case row.StartUnix > now:
				note = "未到开始时间"
			case row.EndUnix > 0 && row.EndUnix <= now:
				note = "已结束"
			}
			add("home", row.CoverPath, imageSyncUse{Label: "首页轮播 · " + row.Title, Note: note, View: "player-policy"})
		}
	}
	live := make(map[int]game.GachaConfiguration, len(o.gachaConfigurations))
	for _, config := range o.gachaConfigurations {
		live[config.Profile.GachaID] = config
	}
	ids := make([]int, 0, len(o.gachaBases))
	for id := range o.gachaBases {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if o.customGachas[id].Deleted {
			continue
		}
		if config, ok := live[id]; ok && config.Profile.CoverPath != "" {
			note := "已发布"
			if config.Disabled {
				note = "已发布 · 停用"
			}
			add("gacha", config.Profile.CoverPath, imageSyncUse{Label: "卡池 · " + config.Profile.Name, Note: note, View: "pool-editor"})
		}
		draft, err := o.storage.ReadDocument("gacha-draft:" + strconv.Itoa(id))
		if err != nil {
			return nil, err
		}
		if !gachaDraftExists(draft) {
			continue
		}
		var config AdminGachaConfig
		if err := json.Unmarshal(draft.Payload, &config); err != nil {
			return nil, err
		}
		if config.CoverPath != "" {
			add("gacha", config.CoverPath, imageSyncUse{Label: "卡池 · " + config.Name, Note: "草稿", View: "pool-editor"})
		}
	}
	return images, nil
}

// imageSyncTarget derives the object key prefix from the container address path, so the address
// plus each relative path is exactly the uploaded object.
func (a *API) imageSyncTarget(base string) (prefix string, err error) {
	if base == "" {
		return "", errors.New("运营设置里还没有填写存储容器地址")
	}
	u, err := url.Parse(base)
	if err != nil || u.RawPath != "" {
		return "", errors.New("存储容器地址的路径只能使用普通字符，不能含 % 转义")
	}
	host := strings.ToLower(u.Hostname())
	if c := a.imageStorage.Credentials; c != nil && c.EndpointURL != "" {
		if endpoint, err := url.Parse(c.EndpointURL); err == nil && strings.EqualFold(endpoint.Hostname(), host) {
			return "", errors.New("存储容器地址填的是上传接口地址；请改为桶的公开下载地址")
		}
	}
	if strings.HasSuffix(host, ".r2.cloudflarestorage.com") {
		return "", errors.New("存储容器地址填的是 R2 上传接口；请改为 r2.dev 公开地址或绑定的自定义域名")
	}
	prefix = strings.Trim(u.Path, "/")
	if prefix == "" {
		return "", nil
	}
	if path.Clean(prefix) != prefix || prefix == "." || prefix == ".." || strings.HasPrefix(prefix, "../") || strings.ContainsAny(prefix, "\\:?#% \r\n\t") {
		return "", errors.New("存储容器地址的路径只能是普通目录名，不能含 ..、空白或转义字符")
	}
	return prefix + "/", nil
}

// The digest binds a run to the images, address and bucket the operator saw.
func (a *API) imageSyncDigest(images []imageSyncImage, base string) string {
	lines := []string{base}
	if c := a.imageStorage.Credentials; c != nil {
		lines = append(lines, c.EndpointURL, c.Bucket)
	}
	for _, image := range images {
		lines = append(lines, image.Path)
	}
	digest := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(digest[:])
}

type imageSyncView struct {
	images   []imageSyncImage
	base     string
	source   string
	prefix   string
	problem  string
	digest   string
	revision int
}

func (a *API) imageSyncViewLocked() (imageSyncView, error) {
	images, err := a.configuredImagesLocked()
	if err != nil {
		return imageSyncView{}, err
	}
	settings := a.operations.runtimeSettings
	view := imageSyncView{images: images, base: settings.GachaCoverBaseURL, source: "server", revision: a.operations.runtimeSettingsRevision}
	if settings.GachaCoverSource == "storage" {
		view.source = "storage"
	}
	view.prefix, err = a.imageSyncTarget(view.base)
	if err != nil {
		view.problem = err.Error()
	}
	view.digest = a.imageSyncDigest(images, view.base)
	return view, nil
}

func (a *API) imageStorageView() map[string]any {
	setting := a.imageStorage
	result := map[string]any{"configured": setting.Credentials != nil, "file": setting.File, "problem": setting.Problem}
	if c := setting.Credentials; c != nil {
		endpoint := "AWS S3 · " + c.Region
		if u, err := url.Parse(c.EndpointURL); err == nil && c.EndpointURL != "" {
			endpoint = u.Host
		}
		hint := "已填写"
		if id := c.AccessKeyID; len(id) >= 10 {
			hint = id[:4] + "…" + id[len(id)-4:]
		}
		result["endpoint"], result["bucket"], result["key_hint"] = endpoint, c.Bucket, hint
	}
	return result
}

func (a *API) imageSyncStatus(w http.ResponseWriter, _ *http.Request) {
	a.operations.configMu.RLock()
	view, err := a.imageSyncViewLocked()
	a.operations.configMu.RUnlock()
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	WriteAdminJSON(w, 200, map[string]any{
		"state": "PASS", "storage": a.imageStorageView(), "images": view.images, "digest": view.digest,
		"source": view.source, "base_url": view.base, "object_prefix": view.prefix, "base_problem": view.problem,
		"settings_revision": view.revision, "job": a.imageSync.snapshot(),
	})
}

func (a *API) startImageSync(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Mode   string `json:"mode"`
		Digest string `json:"digest"`
	}
	if decodeAdminJSON(r, &body) != nil || (body.Mode != "check" && body.Mode != "sync") {
		WriteAdminError(w, 400, "请先检查，再同步")
		return
	}
	credentials := a.imageStorage.Credentials
	if credentials == nil {
		WriteAdminError(w, 409, "未配置上传凭据：请在 cdn-sync.json 填写存储连接后重启服务")
		return
	}
	a.operations.configMu.RLock()
	view, err := a.imageSyncViewLocked()
	a.operations.configMu.RUnlock()
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	if view.problem != "" {
		WriteAdminError(w, 409, view.problem)
		return
	}
	if body.Digest != view.digest {
		WriteAdminError(w, 409, "图片配置或存储容器地址已变化，请刷新后重新检查")
		return
	}
	if len(view.images) == 0 {
		WriteAdminError(w, 409, "当前配置没有引用上传的图片")
		return
	}
	job := &imageSyncJob{Mode: body.Mode, State: "running", Digest: view.digest, BaseURL: view.base, Total: len(view.images),
		StartedUTC: time.Now().UTC().Format(time.RFC3339), Results: map[string]imageSyncResult{}}
	a.imageSync.mu.Lock()
	if a.imageSync.closed {
		a.imageSync.mu.Unlock()
		WriteAdminError(w, http.StatusServiceUnavailable, "服务正在关闭，请稍后重试")
		return
	}
	if a.imageSync.job != nil && a.imageSync.job.State == "running" {
		a.imageSync.mu.Unlock()
		WriteAdminError(w, 409, "上一次检查或同步还在进行，请稍候")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	done := make(chan struct{})
	a.imageSync.job, a.imageSync.cancel, a.imageSync.done = job, cancel, done
	a.imageSync.mu.Unlock()
	go func() {
		defer close(done)
		defer cancel()
		a.runImageSync(ctx, job, view.images, view.prefix, *credentials)
	}()
	WriteAdminJSON(w, http.StatusAccepted, map[string]any{"state": "PASS", "job": a.imageSync.snapshot()})
}

// runImageSync never holds configMu or an admin mutation slot while it talks to storage; only the
// final audit record is admitted like any other Admin write.
func (a *API) runImageSync(ctx context.Context, job *imageSyncJob, images []imageSyncImage, prefix string, credentials objectstore.Credentials) {
	client := objectstore.NewHTTPClient()
	defer client.CloseIdleConnections()
	store := objectstore.New(credentials, client)
	work := make(chan imageSyncImage)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for image := range work {
				result := a.syncImage(ctx, store, client, job, prefix, image)
				result.Error = credentials.Redact(result.Error)
				a.imageSync.mu.Lock()
				job.Results[image.Path] = result
				job.Done++
				a.imageSync.mu.Unlock()
			}
		})
	}
dispatch:
	for _, image := range images {
		select {
		case work <- image:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(work)
	workers.Wait()
	auditError := ""
	if job.Mode == "sync" {
		auditError = a.recordImageSync(a.imageSync.snapshot(), images, prefix, credentials)
	}
	a.imageSync.mu.Lock()
	job.State, job.AuditError = "complete", auditError
	if ctx.Err() != nil {
		job.State = "cancelled"
	}
	job.FinishedUTC = time.Now().UTC().Format(time.RFC3339)
	a.imageSync.mu.Unlock()
}

func (a *API) syncImage(ctx context.Context, store *objectstore.Store, client *http.Client, job *imageSyncJob, prefix string, image imageSyncImage) imageSyncResult {
	if !image.Local {
		return imageSyncResult{Remote: "error", Error: "本服务器缺少这张图片，请在原页面重新上传"}
	}
	dir := a.homeBannerDir
	if image.Kind == "gacha" {
		dir = a.gachaCoverDir
	}
	name := filepath.Base(image.Path)
	object := objectstore.Object{Key: prefix + image.Path, SourcePath: filepath.Join(dir, name), Bytes: image.Bytes,
		SHA256: strings.TrimSuffix(name, filepath.Ext(name)), ContentType: "image/png"}
	if strings.HasSuffix(name, ".jpg") {
		object.ContentType = "image/jpeg"
	}
	var result imageSyncResult
	if job.Mode == "check" {
		match, err := store.Matches(ctx, object)
		switch {
		case errors.Is(err, objectstore.ErrConflict):
			return imageSyncResult{Remote: "conflict", Error: "存储里已有同名但内容不同的文件，未覆盖"}
		case err != nil:
			return imageSyncResult{Remote: "error", Error: err.Error()}
		case !match:
			return imageSyncResult{Remote: "missing"}
		}
		result.Remote = "present"
	} else {
		uploaded, err := store.Put(ctx, object)
		switch {
		case errors.Is(err, objectstore.ErrConflict):
			return imageSyncResult{Remote: "conflict", Error: "存储里已有同名但内容不同的文件，未覆盖"}
		case err != nil:
			return imageSyncResult{Remote: "error", Error: err.Error()}
		}
		result.Remote = "present"
		if uploaded {
			result.Remote = "uploaded"
		}
	}
	address, err := objectstore.PublicURL(job.BaseURL, image.Path)
	if err == nil {
		err = objectstore.CheckPublic(ctx, client, address, object)
	}
	result.Public = "ok"
	if err != nil {
		result.Public, result.Error = "failed", "公开地址读取失败："+err.Error()
	}
	return result
}

// recordImageSync writes the outcome as the image-sync document, which also files it in 操作审计.
// It names files and the target only, never credentials.
func (a *API) recordImageSync(job *imageSyncJob, images []imageSyncImage, prefix string, credentials objectstore.Credentials) string {
	finish, ok := a.operations.AdmitAdminMutation()
	if !ok {
		return "维护操作进行中，本次同步结果未写入操作审计"
	}
	defer finish()
	type row struct {
		Path   string `json:"path"`
		Remote string `json:"remote"`
		Public string `json:"public,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	rows := make([]row, 0, len(images))
	uploaded := 0
	for _, image := range images {
		result := job.Results[image.Path]
		if result.Remote == "uploaded" {
			uploaded++
		}
		rows = append(rows, row{image.Path, result.Remote, result.Public, result.Error})
	}
	endpoint := credentials.EndpointURL
	if u, err := url.Parse(endpoint); err == nil {
		endpoint = u.Host
	}
	record := map[string]any{"base_url": job.BaseURL, "bucket": credentials.Bucket, "endpoint": endpoint, "object_prefix": prefix,
		"started_utc": job.StartedUTC, "images": len(images), "uploaded": uploaded, "results": rows}
	doc, err := a.operations.storage.ReadDocument(imageSyncKey)
	if err == nil {
		_, err = a.operations.writeDocument(imageSyncKey, doc.Revision, record)
	}
	if err != nil {
		return "同步结果未写入操作审计：" + err.Error()
	}
	return ""
}

// activateImageSync switches the image source to the storage container only after a check or sync
// proved every configured image readable there, like cdn.json after a resource sync.
func (a *API) activateImageSync(w http.ResponseWriter, r *http.Request) {
	if err := requireAdminMutation(r); err != nil {
		WriteAdminError(w, 403, err.Error())
		return
	}
	var body struct {
		Digest   string `json:"digest"`
		Expected *int   `json:"expected_revision"`
	}
	if decodeAdminJSON(r, &body) != nil || body.Expected == nil {
		WriteAdminError(w, 400, "缺少配置版本，请刷新后重试")
		return
	}
	job := a.imageSync.snapshot()
	a.saveRuntimeSettings(w, *body.Expected, "runtime-settings", func(settings *game.RuntimeSettings) error {
		view, err := a.imageSyncViewLocked()
		if err != nil {
			return err
		}
		if view.problem != "" {
			return errors.New(view.problem)
		}
		if body.Digest != view.digest || !job.ready(view.digest, view.base) {
			return errors.New("还有图片未确认可从存储容器读取，请重新检查")
		}
		settings.GachaCoverSource = "storage"
		return nil
	})
}
