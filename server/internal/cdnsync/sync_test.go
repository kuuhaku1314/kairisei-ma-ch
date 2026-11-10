package cdnsync

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"kairisei.local/server/internal/cnbootstrap"
)

// A fresh process is necessary because net/http caches proxy environment on
// first use. Actual SDK PUT/HEAD and anonymous GET/HEAD traverse this proxy;
// no external storage or DNS service is used.
func TestSyncLifecycleThroughEnvironmentProxy(t *testing.T) {
	proxyCase := os.Getenv("KAIRI_CDN_TEST_PROXY_CASE")
	if proxyCase == "" {
		for _, name := range []string{"upper", "lower"} {
			t.Run(name, func(t *testing.T) {
				command := exec.Command(os.Args[0], "-test.run=^TestSyncLifecycleThroughEnvironmentProxy$", "-test.v")
				command.Env = append(os.Environ(), "KAIRI_CDN_TEST_PROXY_CASE="+name)
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("proxy lifecycle: %v\n%s", err, output)
				}
			})
		}
		return
	}
	for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(name, "")
	}
	root := t.TempDir()
	resourceRoot := filepath.Join(root, "资源 包")
	if err := os.Mkdir(resourceRoot, 0700); err != nil {
		t.Fatal(err)
	}
	original := bytes.Repeat([]byte("native payload"), 11)
	source := filepath.Join(resourceRoot, "voice.cpk")
	write := func(file string, value []byte) {
		t.Helper()
		if err := os.WriteFile(file, value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(source, original)
	object := cnbootstrap.CDNObject{Key: "cpk/CPK/测试 voice.cpk.v2", Source: "voice.cpk", SourcePath: source, Bytes: int64(len(original)), SHA256: fmt.Sprintf("%x", sha256.Sum256(original))}
	manifest := cnbootstrap.CDNManifest{ResourceSetSHA256: strings.Repeat("a", 64), Root: resourceRoot, Files: []cnbootstrap.CDNObject{object}}
	var lock sync.Mutex
	var stored []byte
	var storedSHA string
	var uploads, publicRequests int
	var badPublic, racePut bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		if r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-pass")) {
			t.Error("proxy credentials not used")
		}
		if r.URL.Path != "/cn602/"+object.Key && r.URL.Path != "/bucket/cn602/"+object.Key {
			http.Error(w, "wrong object path", 400)
			return
		}
		if r.Host == "public.cdn.invalid" {
			publicRequests++
			if r.Header.Get("Authorization") != "" {
				t.Error("storage credentials leaked to public request")
			}
			if badPublic {
				http.Error(w, "unavailable", 403)
				return
			}
			if r.Method == "HEAD" {
				w.Header().Set("Content-Length", fmt.Sprint(len(stored)))
				return
			}
			if r.Header.Get("Range") != "bytes=0-63" || r.Header.Get("Accept-Encoding") != "identity" {
				t.Error("native byte range contract lost")
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-63/%d", len(stored)))
			w.WriteHeader(206)
			w.Write(stored[:64])
			return
		}
		if r.Host != "storage.cdn.invalid" {
			http.Error(w, "unexpected endpoint", 400)
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-access/") {
			t.Error("SDK signature absent")
		}
		switch r.Method {
		case "HEAD":
			if stored == nil {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(stored)))
			w.Header().Set("X-Amz-Meta-Sha256", storedSHA)
		case "PUT":
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			digest := md5.Sum(data)
			if !bytes.Equal(original, data) || r.ContentLength != int64(len(original)) ||
				r.Header.Get("Content-MD5") != base64.StdEncoding.EncodeToString(digest[:]) ||
				r.Header.Get("If-None-Match") != "*" || r.Header.Get("X-Amz-Meta-Sha256") != object.SHA256 ||
				r.Header.Get("Content-Encoding") != "" || r.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Error("PUT did not preserve streaming identity/conditional write")
			}
			uploads++
			stored, storedSHA = data, object.SHA256
			if racePut {
				w.WriteHeader(412)
				return
			}
			w.Header().Set("ETag", `"etag"`)
		default:
			http.Error(w, "unexpected mutation", 405)
		}
	}))
	defer proxy.Close()
	proxyURL := strings.Replace(proxy.URL, "http://", "http://proxy-user:proxy-pass@", 1)
	if proxyCase == "lower" {
		t.Setenv("http_proxy", proxyURL)
		t.Setenv("https_proxy", proxyURL)
		t.Setenv("no_proxy", "direct.cdn.invalid")
	} else {
		t.Setenv("HTTP_PROXY", proxyURL)
		t.Setenv("HTTPS_PROXY", proxyURL)
		t.Setenv("NO_PROXY", "direct.cdn.invalid")
	}
	client := newHTTPClient()
	defer client.CloseIdleConnections()
	for _, spec := range []struct {
		target    string
		wantProxy bool
	}{{"https://storage.cdn.invalid", true}, {"https://public.cdn.invalid", true}, {"http://direct.cdn.invalid", false}} {
		u, _ := url.Parse(spec.target)
		p, err := client.Transport.(*http.Transport).Proxy(&http.Request{URL: u})
		if err != nil || (p != nil) != spec.wantProxy {
			t.Fatalf("HTTPS_PROXY/NO_PROXY resolution: %v", err)
		}
	}
	options := Options{ConfigPath: filepath.Join(root, "cdn-sync.json"), CDNConfigPath: filepath.Join(root, "cdn.json")}
	write(options.ConfigPath, []byte(`{"endpoint_url":"http://storage.cdn.invalid","bucket":"bucket","access_key_id":"test-access","secret_access_key":"test-secret","public_url":"http://public.cdn.invalid"}`))
	c, err := loadConfig(options.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	initial := []byte("{\"base_url\":\"\"}\n")
	reset := func() { write(options.CDNConfigPath, initial) }
	var output bytes.Buffer
	run := func(wantError bool) {
		t.Helper()
		output.Reset()
		err := syncManifest(context.Background(), options, c, manifest, &output)
		if (err != nil) != wantError {
			t.Fatalf("error=%v, output=%s", err, output.String())
		}
		if wantError {
			current, _ := os.ReadFile(options.CDNConfigPath)
			if !bytes.Equal(current, initial) {
				t.Fatal("failure changed public config")
			}
		}
		if strings.Contains(output.String(), "test-secret") || strings.Contains(output.String(), "proxy-pass") {
			t.Fatal("secret leaked to progress")
		}
	}
	reset()
	options.DryRun = true
	run(false)
	options.DryRun = false
	if _, err := os.Stat(filepath.Join(root, "_local")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote a receipt")
	}
	run(false)
	configured, err := cnbootstrap.LoadCDNConfig(options.CDNConfigPath)
	if err != nil || configured.BaseURL != "http://public.cdn.invalid/cn602" {
		t.Fatalf("activation: %+v %v", configured, err)
	}
	run(false)
	lock.Lock()
	if uploads != 1 || publicRequests != 4 {
		t.Errorf("incremental replay: puts=%d public=%d", uploads, publicRequests)
	}
	storedSHA = "conflict"
	lock.Unlock()
	reset()
	run(true)
	lock.Lock()
	stored = nil
	lock.Unlock()
	write(source, bytes.Repeat([]byte("x"), len(original)))
	run(true)
	write(source, original)
	lock.Lock()
	racePut = true
	lock.Unlock()
	run(false) // Another identical upload won between HEAD and PUT.
	reset()
	lock.Lock()
	badPublic = true
	lock.Unlock()
	run(true)
	records, err := filepath.Glob(filepath.Join(root, "_local/cdn-sync/run-*/receipt.json"))
	if err != nil || len(records) != 6 {
		t.Fatalf("unique receipts: %d %v", len(records), err)
	}
	for _, file := range records {
		data, _ := os.ReadFile(file)
		if !json.Valid(data) || bytes.Contains(data, []byte("test-secret")) || bytes.Contains(data, []byte("proxy-pass")) {
			t.Fatal("invalid or credential-bearing receipt")
		}
	}
}
