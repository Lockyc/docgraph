package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileLeaksLiteralAndRegex(t *testing.T) {
	cfg := LeakConfig{
		Terms:      []string{"nucleus", "", "  "},
		Regex:      []string{`192\.168\.1\.\d+`},
		Allow:      []string{"github.com/lockyc"},
		AllowRegex: []string{`au\.lsjc\.[a-z]+`},
		Dir:        []DirRule{{Path: "/x", Ignore: []string{"v/*.json"}, Allow: []string{"mycelium"}}},
	}
	cl, err := cfg.compile()
	if err != nil {
		t.Fatal(err)
	}
	if len(cl.deny) != 6 {
		t.Errorf("deny = %d, want 6 (1 term + 1 regex + 4 built-in)", len(cl.deny))
	}
	if len(cl.allow) != 2 {
		t.Errorf("allow = %d, want 2", len(cl.allow))
	}
	if len(cl.dirs) != 1 || cl.dirs[0].path != "/x" || len(cl.dirs[0].allow) != 1 {
		t.Errorf("dirs = %+v, want one dir /x with 1 allow", cl.dirs)
	}
}

func TestCompileLeaksBadRegex(t *testing.T) {
	_, err := LeakConfig{Regex: []string{"(unclosed"}}.compile()
	if err == nil || !strings.Contains(err.Error(), "leaks regex") {
		t.Errorf("want a leaks-regex compile error, got %v", err)
	}
}

// J1: a regexp deny is case-insensitive by default — a footprint term written in
// a different casing must NOT slip the gate (false negatives are the cardinal sin).
func TestLeakScanRegexDenyIsCaseInsensitive(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "host Nucleus-Prod here\n"}, []string{"a.md"})
	found, err := LeakScan(dir, LeakConfig{Regex: []string{"nucleus"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("regex deny must be case-insensitive (catch 'Nucleus'), got %+v", found)
	}
}

// J1: allow_regex is case-insensitive too, so it suppresses a deny match whose
// casing differs from the allow pattern.
func TestLeakScanAllowRegexIsCaseInsensitive(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "id au.LSJC.curator ok\n"}, []string{"a.md"})
	found, err := LeakScan(dir, LeakConfig{Terms: []string{"lsjc"}, AllowRegex: []string{`au\.lsjc\.[a-z]+`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("case-insensitive allow_regex should suppress the match, got %+v", found)
	}
}

// J2: a non-absolute [[dir]] path is a fatal config error, not a silent no-op.
func TestCompileDirPathRelativeIsError(t *testing.T) {
	_, err := LeakConfig{Dir: []DirRule{{Path: "relative/dir"}}}.compile()
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("a non-absolute [[dir]] path must be a config error, got %v", err)
	}
}

// J2: a leading ~/ in a [[dir]] path expands to the home dir.
func TestCompileDirPathTildeExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	cl, err := LeakConfig{Dir: []DirRule{{Path: "~/proj"}}}.compile()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "proj")
	if len(cl.dirs) != 1 || cl.dirs[0].path != want {
		t.Errorf("~ must expand to %q, got %+v", want, cl.dirs)
	}
}

func TestLeakScanLiteralAndRegex(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"README.md": "clean\ncontact Lachlan here\nhost nucleus up\n", // case-insensitive literal + literal
		"net.conf":  "ip 192.168.1.42 assigned\n",                     // regex
	}, []string{"README.md", "net.conf"})

	cfg := LeakConfig{Terms: []string{"lachlan", "nucleus"}, Regex: []string{`192\.168\.1\.\d+`}}
	found, err := LeakScan(dir, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 {
		t.Fatalf("findings = %+v, want 3 (Lachlan, nucleus, IP)", found)
	}
}

func TestLeakScanGlobalAllowSuppresses(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"a.md": "bundle au.lsjc.curator is fine\nbut lsjc.au alone leaks\n",
	}, []string{"a.md"})

	cfg := LeakConfig{Terms: []string{"lsjc"}, Allow: []string{"au.lsjc.curator"}}
	found, err := LeakScan(dir, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	// line 1 'lsjc' covered by the allow span; line 2 'lsjc' flagged.
	if len(found) != 1 || found[0].Line != 2 {
		t.Fatalf("findings = %+v, want exactly a.md:2", found)
	}
}

func TestLeakScanBuiltinSuppressedByAllowRegex(t *testing.T) {
	const token = "ghp_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8"
	dir := setupRepo(t, map[string]string{"README.md": "key " + token + "\n"}, []string{"README.md"})

	// Bare: the built-in ghp_ pattern must fire (proves the fixture is a real match).
	bare, err := LeakScan(dir, LeakConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(bare) != 1 || bare[0].Match != token {
		t.Fatalf("built-in ghp_ should match, got %+v", bare)
	}
	// With an allow_regex covering it, the built-in match is suppressed.
	found, err := LeakScan(dir, LeakConfig{AllowRegex: []string{`ghp_[A-Za-z0-9]{36}`}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("allow_regex should suppress the built-in, got %+v", found)
	}
}

func TestLeakScanDirIgnoreDropsFile(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"vendor/spec.json": "key AKIAIOSFODNN7EXAMPLE inside\n", // dropped by dir ignore
		"src.go":           "key AKIAIOSFODNN7EXAMPLE inside\n", // flagged
	}, []string{"vendor/spec.json", "src.go"})

	cfg := LeakConfig{Dir: []DirRule{{Path: dir, Ignore: []string{"vendor/*.json"}}}}
	found, err := LeakScan(dir, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].File != "src.go" {
		t.Fatalf("findings = %+v, want only src.go (vendor/spec.json ignored)", found)
	}
}

func TestLeakScanDirAllowIsScoped(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"sub/a.md": "mycelium here\n", // suppressed: under the dir allow
		"top.md":   "mycelium here\n", // flagged: outside the dir
	}, []string{"sub/a.md", "top.md"})

	cfg := LeakConfig{
		Terms: []string{"mycelium"},
		Dir:   []DirRule{{Path: dir + "/sub", Allow: []string{"mycelium"}}},
	}
	found, err := LeakScan(dir, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].File != "top.md" {
		t.Fatalf("findings = %+v, want only top.md (sub/ allow-scoped)", found)
	}
}

func TestLeakScanTrackedToolingStillScanned(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		".claude/skills/foo.md": "internal note: nucleus\n",
		".docauditignore":       ".claude/**\n",
	}, []string{".claude/skills/foo.md", ".docauditignore"})

	found, err := LeakScan(dir, LeakConfig{Terms: []string{"nucleus"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var hit bool
	for _, f := range found {
		if f.File == ".claude/skills/foo.md" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("tracked .claude/ must stay in leak scope despite doc-graph ignores, got %+v", found)
	}
}

func TestLeakScanBinarySkippedAndExtraIgnore(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"logo.bin":  "\x00\x01nucleus\x00", // binary, skipped
		"vendor.md": "nucleus here\n",      // dropped by --ignore
		"real.md":   "nucleus here\n",      // flagged
	}, []string{"logo.bin", "vendor.md", "real.md"})

	found, err := LeakScan(dir, LeakConfig{Terms: []string{"nucleus"}}, []string{"vendor.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].File != "real.md" {
		t.Fatalf("findings = %+v, want only real.md", found)
	}
}

func TestLeakScanBadRegexIsError(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "x\n"}, []string{"a.md"})
	_, err := LeakScan(dir, LeakConfig{Regex: []string{"(unclosed"}}, nil)
	if err == nil {
		t.Error("bad regex in config should make LeakScan error")
	}
}
