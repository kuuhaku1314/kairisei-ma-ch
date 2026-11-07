package cdnsync

import (
	"os"
	"path/filepath"
	"testing"
)

// Launchers pass cdn-sync.json by default, so an absent or untouched file must simply leave the
// Admin image sync off, and a faulty one must not stop the server.
func TestLoadImageStorageIsOptional(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	root := t.TempDir()
	file := filepath.Join(root, "cdn-sync.json")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range []struct {
		name    string
		setting func() bool
	}{
		{"no flag", func() bool { s := LoadImageStorage(""); return s.Credentials == nil && s.Problem == "" }},
		{"missing file", func() bool { s := LoadImageStorage(file); return s.Credentials == nil && s.Problem == "" }},
		{"example", func() bool {
			write(`{"endpoint_url":"https://YOUR_ACCOUNT_ID.r2.cloudflarestorage.com","bucket":"kairisei","access_key_id":"YOUR_ACCESS_KEY_ID","secret_access_key":"YOUR_SECRET_ACCESS_KEY","public_url":"https://pub-YOUR_PUBLIC_ID.r2.dev"}`)
			s := LoadImageStorage(file)
			return s.Credentials == nil && s.Problem == ""
		}},
		{"partial", func() bool {
			write(`{"endpoint_url":"https://acct.r2.cloudflarestorage.com","bucket":"kairisei","access_key_id":"YOUR_ACCESS_KEY_ID","secret_access_key":""}`)
			s := LoadImageStorage(file)
			return s.Credentials == nil && s.Problem != ""
		}},
		{"broken json", func() bool {
			write(`{"endpoint_url":`)
			s := LoadImageStorage(file)
			return s.Credentials == nil && s.Problem != ""
		}},
		{"filled without public_url", func() bool {
			write(`{"endpoint_url":"https://acct.r2.cloudflarestorage.com/","bucket":"kairisei","access_key_id":"id","secret_access_key":"secret"}`)
			s := LoadImageStorage(file)
			return s.Problem == "" && s.Credentials != nil && s.Credentials.EndpointURL == "https://acct.r2.cloudflarestorage.com" && s.Credentials.Region == "auto"
		}},
	} {
		if !step.setting() {
			t.Errorf("%s: unexpected image storage setting", step.name)
		}
	}
}
