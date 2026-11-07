package cdnsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kairisei.local/server/internal/cnbootstrap"
	"kairisei.local/server/internal/objectstore"
)

type Options struct {
	ConfigPath      string
	ResourceSet     string
	PlatformContent []string
	CDNConfigPath   string
	DryRun          bool
}

type receipt struct {
	ResourceSetSHA256 string                           `json:"resource_set_sha256"`
	PlatformContents  []cnbootstrap.CDNPlatformContent `json:"platform_contents,omitempty"`
	Bucket            string                           `json:"bucket"`
	Prefix            string                           `json:"prefix"`
	BaseURL           string                           `json:"base_url"`
	Objects           int                              `json:"objects"`
	Bytes             int64                            `json:"bytes"`
	Uploaded          int                              `json:"uploaded"`
	Skipped           int                              `json:"skipped"`
	UploadedBytes     int64                            `json:"uploaded_bytes"`
	PublicSamples     int                              `json:"public_samples"`
	Activated         bool                             `json:"activated"`
	Error             string                           `json:"error,omitempty"`
}

func Run(ctx context.Context, options Options, out io.Writer) error {
	c, err := loadConfig(options.ConfigPath)
	if err != nil {
		return err
	}
	if out == nil {
		out = io.Discard
	}
	fmt.Fprintln(out, "[1/3] 读取当前资源版本与别名映射……")
	manifest, err := cnbootstrap.BuildCDNManifest(options.ResourceSet, options.PlatformContent...)
	if err != nil {
		return err
	}
	return syncManifest(ctx, options, c, manifest, out)
}

func syncManifest(ctx context.Context, options Options, c config, manifest cnbootstrap.CDNManifest, out io.Writer) (err error) {
	if len(manifest.Files) == 0 {
		return errors.New("没有可同步的资源")
	}
	target, err := writableTarget(options.CDNConfigPath, manifest.ReadOnlyRoots()...)
	if err != nil {
		return err
	}
	inputPath, err := filepath.Abs(options.ConfigPath)
	if err != nil {
		return err
	}
	if filepath.Clean(inputPath) == target {
		return errors.New("同步密钥配置和公开 CDN 配置不能使用同一个文件")
	}
	previous, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	existed := err == nil
	if existed {
		if _, err := cnbootstrap.LoadCDNConfig(target); err != nil {
			return errors.New("目标 cdn.json 不是有效的公开地址配置，已拒绝覆盖")
		}
	}
	r := receipt{ResourceSetSHA256: manifest.ResourceSetSHA256, PlatformContents: manifest.PlatformContents, Bucket: c.Bucket, Prefix: c.Prefix, BaseURL: c.baseURL(), Objects: len(manifest.Files)}
	for _, object := range manifest.Files {
		r.Bytes += object.Bytes
	}
	fmt.Fprintf(out, "资源 %d 个，共 %.2f GiB；并发 %d。\n", r.Objects, float64(r.Bytes)/(1<<30), c.Workers)
	if options.DryRun {
		fmt.Fprintln(out, "Dry run：本地配置、映射及文件大小检查通过；未联网、未上传、未修改 CDN 配置。")
		return nil
	}
	// Reserve a unique receipt before uploading. Never put credentials in it.
	receiptRoot := filepath.Join(filepath.Dir(target), "_local", "cdn-sync")
	if err := os.MkdirAll(receiptRoot, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(receiptRoot, "run-"+time.Now().Format("20060102-150405")+"-")
	if err != nil {
		return err
	}
	record, err := os.OpenFile(filepath.Join(dir, "receipt.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			r.Error = c.Redact(err.Error())
			err = errors.New(r.Error)
		}
		encoder := json.NewEncoder(record)
		encoder.SetIndent("", "  ")
		err = errors.Join(err, encoder.Encode(r), record.Close())
		fmt.Fprintf(out, "同步记录：%s\n", record.Name())
	}()
	client := newHTTPClient()
	defer client.CloseIdleConnections()
	storage := objectstore.New(c.Credentials, client)
	fmt.Fprintln(out, "[2/3] 增量同步；已存在且一致的文件自动跳过。Ctrl+C 可停止，重跑即可继续。")
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type completion struct {
		object   cnbootstrap.CDNObject
		uploaded bool
		err      error
	}
	jobs := make(chan cnbootstrap.CDNObject)
	results := make(chan completion, c.Workers)
	var workers sync.WaitGroup
	for range c.Workers {
		workers.Go(func() {
			for object := range jobs {
				if workCtx.Err() != nil {
					return
				}
				uploaded, err := syncObject(workCtx, storage, c, object)
				results <- completion{object, uploaded, err}
				if err != nil {
					return
				}
			}
		})
	}
	go func() {
		defer close(jobs)
		for _, object := range manifest.Files {
			select {
			case jobs <- object:
			case <-workCtx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	progress := func() {
		fmt.Fprintf(out, "%d/%d：上传 %d，跳过 %d，已上传 %.1f MiB\n", r.Uploaded+r.Skipped, r.Objects, r.Uploaded, r.Skipped, float64(r.UploadedBytes)/(1<<20))
	}
collect:
	for {
		select {
		case result, ok := <-results:
			if !ok {
				break collect
			}
			if result.err != nil {
				if err == nil {
					err = fmt.Errorf("%s: %w", result.object.Key, result.err)
					cancel()
				}
			} else if result.uploaded {
				r.Uploaded++
				r.UploadedBytes += result.object.Bytes
			} else {
				r.Skipped++
			}
		case <-ticker.C:
			progress()
		}
	}
	progress()
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return objectstore.NetworkError(ctx.Err())
	}
	if r.Uploaded+r.Skipped != r.Objects {
		return errors.New("同步未完成，未启用 CDN；请重新运行")
	}
	fmt.Fprintln(out, "[3/3] 检查公开地址的 HEAD、Range 与原始字节……")
	for _, object := range publicSamples(manifest.Files) {
		if err := checkPublic(ctx, client, c.baseURL(), object); err != nil {
			return fmt.Errorf("%s: %w", object.Key, err)
		}
		r.PublicSamples++
	}
	if ctx.Err() != nil {
		return objectstore.NetworkError(ctx.Err())
	}
	current, readErr := os.ReadFile(target)
	if (existed && (readErr != nil || !bytes.Equal(previous, current))) || (!existed && !errors.Is(readErr, os.ErrNotExist)) {
		return errors.New("同步期间 cdn.json 已被修改，保留当前配置；请核对后重跑")
	}
	content, _ := json.MarshalIndent(cnbootstrap.CDNConfig{BaseURL: c.baseURL()}, "", "  ")
	if err := replaceConfig(target, append(content, '\n')); err != nil {
		return err
	}
	r.Activated = true
	fmt.Fprintln(out, "同步完成，cdn.json 已启用。请重启游戏服务并让客户端重新登录；无需更新 APK。")
	return nil
}

func writableTarget(filename string, resourceRoots ...string) (string, error) {
	abs, err := filepath.Abs(filename)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	abs = filepath.Join(parent, filepath.Base(abs))
	for _, root := range resourceRoots {
		if relative, err := filepath.Rel(root, abs); err == nil && filepath.IsLocal(relative) {
			return "", errors.New("CDN 配置不能写入只读资源输入目录")
		}
	}
	if info, err := os.Lstat(abs); err == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("CDN 配置目标必须是普通文件，不能是目录或链接")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return abs, nil
}

func replaceConfig(filename string, content []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(filename), ".cdn-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // Only our own newly created temporary file.
	_, writeErr := tmp.Write(content)
	err = errors.Join(writeErr, tmp.Sync(), tmp.Close())
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filename)
}
