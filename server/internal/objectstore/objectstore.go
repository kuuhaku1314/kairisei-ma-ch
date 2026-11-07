// Package objectstore is the S3-compatible upload adapter shared by the resource CDN sync command
// and the Admin operational image sync. Credentials come only from the operator's cdn-sync.json or
// AWS_* environment; this package reads no game data, opens no database and listens on no port.
package objectstore

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// Credentials name one bucket and its signing keys. They are validated by the caller that reads them.
type Credentials struct {
	EndpointURL     string `json:"endpoint_url"`
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	Region          string `json:"region"`
	AddressingStyle string `json:"addressing_style"`
	SessionToken    string `json:"session_token"`
}

// Setting is an optional connection read at startup. Nil Credentials with an empty Problem means
// the operator has not filled one in; a Problem keeps the feature off without stopping the server.
type Setting struct {
	File        string
	Credentials *Credentials
	Problem     string
}

// Object is one local file and its complete key inside the bucket.
type Object struct {
	Key         string
	SourcePath  string
	Bytes       int64
	SHA256      string
	ContentType string
}

// ErrConflict reports a different object already stored under the same key; it is never overwritten.
var ErrConflict = errors.New("远端同名对象与本地内容不一致，已拒绝覆盖")

// SDK/network errors can contain URLs (including proxy credentials). Do not
// expose them in progress or receipts; report status/API code at the boundary.
func (c Credentials) Redact(message string) string {
	for _, secret := range []string{c.AccessKeyID, c.SecretAccessKey, c.SessionToken} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}

func NewHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.DisableCompression = true
	transport.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 60 * time.Second
	transport.MaxIdleConnsPerHost = 32
	return &http.Client{Transport: transport, Timeout: 15 * time.Minute,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			// SDK credentials must only reach the configured storage endpoint.
			return http.ErrUseLastResponse
		}}
}

// Store signs requests for one bucket. The HTTP client is shared with public download checks.
type Store struct {
	bucket string
	client *s3.Client
}

func New(c Credentials, client *http.Client) *Store {
	options := s3.Options{
		Region: c.Region, HTTPClient: client, UsePathStyle: c.AddressingStyle == "path",
		RetryMaxAttempts:           4,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey, SessionToken: c.SessionToken}, nil
		}),
	}
	if c.EndpointURL != "" {
		options.BaseEndpoint = aws.String(c.EndpointURL)
	}
	return &Store{bucket: c.Bucket, client: s3.New(options)}
}

func statusCode(err error) int {
	var status interface{ HTTPStatusCode() int }
	if errors.As(err, &status) {
		return status.HTTPStatusCode()
	}
	return 0
}

func NetworkError(err error) error {
	if errors.Is(err, context.Canceled) {
		return errors.New("同步已取消；重新运行即可继续")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("请求超时；检查网络或代理后重试")
	}
	var api smithy.APIError
	if errors.As(err, &api) {
		return fmt.Errorf("存储请求失败 HTTP %d / %s；请检查桶、密钥权限、endpoint 和 region", statusCode(err), api.ErrorCode())
	}
	return errors.New("网络请求失败；请检查网络、TLS 证书及 HTTP_PROXY / HTTPS_PROXY / NO_PROXY 设置后重试")
}

// Matches reports whether the identical object is stored; a missing key is false without error.
func (s *Store) Matches(ctx context.Context, object Object) (bool, error) {
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &object.Key})
	if statusCode(err) == 404 {
		return false, nil
	}
	if err != nil {
		return false, NetworkError(err)
	}
	encoding := aws.ToString(head.ContentEncoding)
	if aws.ToInt64(head.ContentLength) != object.Bytes || head.Metadata["sha256"] != object.SHA256 || (encoding != "" && encoding != "identity") {
		return false, ErrConflict
	}
	return true, nil
}

// Put uploads a missing object after re-reading the local bytes; an identical stored object is
// skipped (false) and a different one is refused with ErrConflict.
func (s *Store) Put(ctx context.Context, object Object) (bool, error) {
	match, err := s.Matches(ctx, object)
	if err != nil || match {
		return false, err
	}
	f, err := os.Open(object.SourcePath)
	if err != nil {
		return false, err
	}
	defer f.Close()
	sha, md := sha256.New(), md5.New() // MD5 is a transfer checksum, not authentication.
	buffer := make([]byte, 1024*1024)
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return false, NetworkError(err)
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			sha.Write(buffer[:n])
			md.Write(buffer[:n])
			size += int64(n)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return false, readErr
		}
	}
	if size != object.Bytes || hex.EncodeToString(sha.Sum(nil)) != object.SHA256 {
		return false, errors.New("本地文件大小或 SHA-256 已变化，未上传")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	contentType := object.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &object.Key, Body: f, ContentLength: &object.Bytes,
		ContentMD5:  aws.String(base64.StdEncoding.EncodeToString(md.Sum(nil))),
		ContentType: &contentType, CacheControl: aws.String("public, max-age=31536000, immutable"),
		Metadata: map[string]string{"sha256": object.SHA256}, IfNoneMatch: aws.String("*"),
	})
	// A concurrent uploader may have won; accept only the identical object.
	if statusCode(err) == 412 {
		match, headErr := s.Matches(ctx, object)
		if headErr != nil {
			return false, headErr
		}
		if match {
			return false, nil
		}
	}
	if err != nil {
		return false, NetworkError(err)
	}
	match, err = s.Matches(ctx, object)
	if err != nil {
		return false, err
	}
	if !match {
		return false, errors.New("上传后对象不可读，请稍后重新运行")
	}
	return true, nil
}

// PublicURL joins a key below a public base address, escaping it as a URL path.
func PublicURL(base, key string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", errors.New("公开下载地址无效")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + key
	u.RawPath = ""
	return u.String(), nil
}

// CheckPublic reads an uploaded object anonymously: HEAD length, an identity Range 206 and the
// leading original bytes, as the game client downloads it.
func CheckPublic(ctx context.Context, client *http.Client, address string, object Object) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	head, err := http.NewRequestWithContext(ctx, http.MethodHead, address, nil)
	if err != nil {
		return err
	}
	head.Header.Set("Accept-Encoding", "identity")
	response, err := client.Do(head)
	if err != nil {
		return NetworkError(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || response.ContentLength != object.Bytes || !identityEncoding(response.Header) {
		return fmt.Errorf("公开 HEAD 不匹配（HTTP %d），请确认桶公开可读、公开地址和前缀正确", response.StatusCode)
	}
	length := min(int64(64), object.Bytes)
	get, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	get.Header.Set("Accept-Encoding", "identity")
	get.Header.Set("Range", fmt.Sprintf("bytes=0-%d", length-1))
	response, err = client.Do(get)
	if err != nil {
		return NetworkError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 206 || response.Header.Get("Content-Range") != fmt.Sprintf("bytes 0-%d/%d", length-1, object.Bytes) || !identityEncoding(response.Header) {
		return errors.New("公开地址未保留 Range 206；请关闭压缩／转码并允许字节范围下载")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, length+1))
	if err != nil {
		return NetworkError(err)
	}
	f, err := os.Open(object.SourcePath)
	if err != nil {
		return err
	}
	defer f.Close()
	expected := make([]byte, length)
	if _, err := io.ReadFull(f, expected); err != nil {
		return err
	}
	if !bytes.Equal(data, expected) {
		return errors.New("公开下载的原始字节与本地文件不同")
	}
	return nil
}

func identityEncoding(header http.Header) bool {
	value := header.Get("Content-Encoding")
	return value == "" || value == "identity"
}
