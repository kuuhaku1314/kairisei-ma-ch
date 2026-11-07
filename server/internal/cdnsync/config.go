// Package cdnsync provides the optional, short-lived resource upload command and reads the
// cdn-sync.json storage connection that Admin may use for operational images. Normal server
// startup never creates an S3 client; it only reads this file when the launcher passes it.
package cdnsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"

	"kairisei.local/server/internal/cnbootstrap"
	"kairisei.local/server/internal/objectstore"
)

type config struct {
	objectstore.Credentials
	PublicURL string `json:"public_url"`
	Prefix    string `json:"prefix"`
	Workers   int    `json:"workers"`
}

func defaultConfig() config {
	return config{Credentials: objectstore.Credentials{Region: "auto", AddressingStyle: "path"}, Prefix: "cn602", Workers: 8}
}

func readConfig(filename string, c *config) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 64*1024 {
		return errors.New("CDN 同步配置超过 64KB")
	}
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(c); err != nil {
		return errors.New("CDN 同步 JSON 格式错误或含未知字段，请对照示例（末项不要加逗号）")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("CDN 同步配置只能包含一个 JSON 对象")
	}
	return nil
}

func placeholder(value string) bool {
	return strings.Contains(value, "YOUR_") || strings.ContainsAny(value, "<>\r\n")
}

func validateCredentials(c *objectstore.Credentials) error {
	var err error
	for name, value := range map[string]string{"endpoint_url": c.EndpointURL, "bucket": c.Bucket, "access_key_id": c.AccessKeyID, "secret_access_key": c.SecretAccessKey} {
		if placeholder(value) {
			return fmt.Errorf("请填写 %s，不能保留示例占位符", name)
		}
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,62}$`).MatchString(c.Bucket) {
		return errors.New("请填写正确的 bucket 桶名称")
	}
	if c.EndpointURL != "" {
		c.EndpointURL, err = cnbootstrap.NormalizeCDNBaseURL(c.EndpointURL)
		if err != nil {
			return errors.New("endpoint_url 必须是存储服务提供的 HTTP(S) S3 API 地址")
		}
	} else if c.Region == "" || c.Region == "auto" {
		return errors.New("请填写 endpoint_url；只有 AWS S3 可省略，并须填写实际 region")
	}
	if c.Region == "" {
		return errors.New("region 不能为空；R2 使用 auto")
	}
	if c.AddressingStyle != "path" && c.AddressingStyle != "virtual" && c.AddressingStyle != "auto" {
		return errors.New("addressing_style 只能是 path、virtual 或 auto")
	}
	if c.AccessKeyID == "" && c.SecretAccessKey == "" {
		c.AccessKeyID, c.SecretAccessKey = os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
		if c.SessionToken == "" {
			c.SessionToken = os.Getenv("AWS_SESSION_TOKEN")
		}
	}
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return errors.New("请填写 access_key_id 和 secret_access_key（不是 Cloudflare 令牌值），或同时设置 AWS 凭据环境变量")
	}
	return nil
}

func loadConfig(filename string) (config, error) {
	c := defaultConfig()
	if err := readConfig(filename, &c); errors.Is(err, os.ErrNotExist) {
		return c, errors.New("请将 cdn-sync.example.json 复制为 cdn-sync.json，填写五项配置后重新运行")
	} else if err != nil {
		return c, err
	}
	if placeholder(c.PublicURL) {
		return c, errors.New("请填写 public_url，不能保留示例占位符")
	}
	if err := validateCredentials(&c.Credentials); err != nil {
		return c, err
	}
	var err error
	c.PublicURL, err = cnbootstrap.NormalizeCDNBaseURL(c.PublicURL)
	if err != nil || c.PublicURL == "" {
		return c, errors.New("请填写 public_url：桶的公开下载根地址，不含 cn602 后缀")
	}
	u, _ := url.Parse(c.PublicURL)
	if strings.HasSuffix(strings.ToLower(u.Hostname()), ".r2.cloudflarestorage.com") || c.PublicURL == c.EndpointURL {
		return c, errors.New("public_url 不能使用 S3 上传地址；R2 请填已启用的 r2.dev 地址或自定义域名")
	}
	c.Prefix = strings.Trim(c.Prefix, "/")
	if c.Prefix != "" && (path.Clean(c.Prefix) != c.Prefix || c.Prefix == "." || c.Prefix == ".." || strings.HasPrefix(c.Prefix, "../") || strings.ContainsAny(c.Prefix, "\\:?#% \r\n\t")) {
		return c, errors.New("prefix 必须是普通相对目录，不能包含 ..、空白或 URL 转义字符")
	}
	if c.Workers < 1 || c.Workers > 32 {
		return c, errors.New("workers 必须为 1–32，默认 8")
	}
	return c, nil
}

// LoadImageStorage reads the storage connection of cdn-sync.json for the Admin operational image
// sync. It is optional: an empty path, a missing file or the untouched example leaves it off, and a
// faulty file is reported in Admin instead of stopping the game service. public_url, prefix and
// workers belong to the resource sync; the images use the 运营设置 storage address.
func LoadImageStorage(filename string) objectstore.Setting {
	setting := objectstore.Setting{File: filename}
	if filename == "" {
		return setting
	}
	c := defaultConfig()
	if err := readConfig(filename, &c); errors.Is(err, os.ErrNotExist) {
		return setting
	} else if err != nil {
		setting.Problem = err.Error()
		return setting
	}
	unfilled := func(value string) bool { return value == "" || placeholder(value) }
	if unfilled(c.EndpointURL) && unfilled(c.AccessKeyID) && unfilled(c.SecretAccessKey) {
		return setting
	}
	if err := validateCredentials(&c.Credentials); err != nil {
		setting.Problem = err.Error()
		return setting
	}
	setting.Credentials = &c.Credentials
	return setting
}

func (c config) baseURL() string {
	if c.Prefix == "" {
		return c.PublicURL
	}
	return c.PublicURL + "/" + c.Prefix
}
