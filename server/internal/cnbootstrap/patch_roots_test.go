package cnbootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineCNPatchCatalogSourcePriority(t *testing.T) {
	primary, fallback := t.TempDir(), t.TempDir()
	const name = "model.dat"
	if err := os.WriteFile(filepath.Join(primary, name), []byte("primary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fallback, name), []byte("longer fallback"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		roots []string
		want  string
	}{
		{"primary selected", []string{primary, fallback}, "primary"},
		{"reversed priority", []string{fallback, primary}, "longer fallback"},
		{"missing primary", []string{t.TempDir(), fallback}, "longer fallback"},
	} {
		t.Run(test.name, func(t *testing.T) {
			info, err := findCNPatchFile(test.roots, name)
			if err != nil || info.Size() != int64(len(test.want)) {
				t.Fatalf("catalog selection: %v, %v", info, err)
			}

		})
	}
}

func TestResolveCNPatchRootsParallelOfficialNamespace(t *testing.T) {
	const original = "<version>,790\n<bundle_ver>,main_c/a.dat,0,12345678\n<bundle_ver>,live2d/b.dat,0,87654321\n"
	cases := []struct {
		name      string
		variant   string
		wantError bool
	}{
		{"identical", original, false},
		{"historical CRC only", strings.ReplaceAll(original, "12345678", "ABCDEF00"), false},
		{"wrong version", strings.ReplaceAll(original, "790", "791"), true},
		{"different path", strings.ReplaceAll(original, "live2d/b.dat", "live2d/c.dat"), true},
		{"reordered paths", "<version>,790\n<bundle_ver>,live2d/b.dat,0,87654321\n<bundle_ver>,main_c/a.dat,0,12345678\n", true},
		{"missing version", strings.ReplaceAll(original, "<version>,790\n", ""), true},
		{"duplicate path", original + "<bundle_ver>,main_c/a.dat,0,12345678\n", true},
		{"invalid CRC", strings.ReplaceAll(original, "12345678", "not-crc"), true},
		{"unsafe path", strings.ReplaceAll(original, "live2d/b.dat", "../b.dat"), true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			roots := []string{filepath.Join(t.TempDir(), "primary"), filepath.Join(t.TempDir(), "variant")}
			for index, plain := range []string{original, test.variant} {
				if err := os.MkdirAll(roots[index], 0o700); err != nil {
					t.Fatal(err)
				}
				encoded := []byte(plain)
				for offset := range encoded {
					encoded[offset] += cn602ScrambleKey[offset%len(cn602ScrambleKey)]
				}
				if err := os.WriteFile(filepath.Join(roots[index], "version.dat"), encoded, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			resolved, err := resolveCNPatchRoots(roots)
			if (err != nil) != test.wantError {
				t.Fatalf("resolve roots: %v, want error %v", err, test.wantError)
			}
			if err == nil && (len(resolved) != 2 || resolved[0] != roots[0] || resolved[1] != roots[1]) {
				t.Fatalf("source priority changed: %v", resolved)
			}
		})
	}
}
