package cnbootstrap

import (
	"kairisei.local/server/internal/cpk"
	"strings"
	"testing"
)

func TestCPKSphereMoviesUseBattleCategory(t *testing.T) {
	files := []cpk.File{
		{Name: "mov_16000011.cpk", Size: 1048576, Version: 1},
		{Name: "mov_16000400.cpk", Size: 1048576, Version: 1},
		{Name: "mov_op.cpk", Size: 1048576, Version: 1},
		{Name: "mov_prologue.cpk", Size: 1048576, Version: 1},
		{Name: "mov_unknown.cpk", Size: 1048576, Version: 1},
	}
	list := string(cpk.FileList(files))
	for _, row := range []string{
		"mov_16000011.cpk,SPHR_MOVIE,16000011,1.000000,ALL,1048576\n",
		"mov_16000400.cpk,SPHR_MOVIE,16000400,1.000000,ALL,1048576\n",
		"mov_op.cpk,MOVIE,1,1.000000,ALL,1048576\n",
		"mov_prologue.cpk,MOVIE,0,1.000000,ALL,1048576\n",
		"mov_unknown.cpk,MOVIE,0,1.000000,ALL,1048576\n",
	} {
		if !strings.Contains(list, row) {
			t.Fatalf("missing native movie category: %q in %s", row, list)
		}
	}
}

func TestCDNRejectsInvalidPublicURLs(t *testing.T) {
	for _, base := range []string{"file:///tmp/assets", "https://user:secret@cdn.test", "https://cdn.test/?token=secret"} {
		if _, err := (CDNConfig{BaseURL: base}).normalized(); err == nil {
			t.Fatalf("invalid public CDN configuration accepted: %s", base)
		}
	}
}
