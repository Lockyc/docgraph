package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func TestVersionEmbedded(t *testing.T) {
	if version == "" {
		t.Fatal("version is empty; the VERSION file was not embedded")
	}
}

// TestModuleMajorMatchesVersion pins every restated module path to VERSION's
// major: go.mod's module line and every tracked `docgraph[/vN]@latest` or
// `MODULE="…docgraph[/vN]"` site outside the Go sources (whose import paths the
// compiler already checks against go.mod). A major bump that misses one ships
// install instructions Go silently resolves to the previous major.
func TestModuleMajorMatchesVersion(t *testing.T) {
	want := modulePath()
	gomod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(string(gomod), "\n"); first != "module "+want {
		t.Errorf("go.mod declares %q, VERSION %s needs %q", first, version, "module "+want)
	}

	out, err := exec.Command("git", "ls-files").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	// An install site: `…docgraph[/vN]@latest`, or a `MODULE="…docgraph[/vN]"` var.
	// Not a bare repo URL, which has no major.
	site := regexp.MustCompile(`github\.com/lockyc/docgraph(/v\d+)?@latest|MODULE="github\.com/lockyc/docgraph(/v\d+)?"`)
	for _, f := range strings.Fields(string(out)) {
		if strings.HasSuffix(f, ".go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range site.FindAllString(string(b), -1) {
			got := strings.TrimSuffix(strings.TrimPrefix(m, `MODULE="`), `"`)
			if got = strings.TrimSuffix(got, "@latest"); got != want {
				t.Errorf("%s names %s, VERSION %s needs %s", f, got, version, want)
			}
		}
	}
}
