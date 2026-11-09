package audit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func cacheKeys(t *testing.T, c *LeakCache) int {
	t.Helper()
	b, err := os.ReadFile(c.Path)
	if err != nil {
		return 0
	}
	return len(b) / cacheKeyLen
}

func newTestCache(t *testing.T, root string) *LeakCache {
	return &LeakCache{Path: LeakCachePath(t.TempDir(), root), Salt: "test"}
}

// Clean files are recorded and kept across runs; a file with a finding never is.
func TestLeakCacheRecordsOnlyCleanFiles(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "nothing here\n", "b.md": "host corehost\n"}, []string{"a.md", "b.md"})
	c := newTestCache(t, dir)
	cfg := LeakConfig{Terms: []string{"corehost"}}
	for run := 0; run < 2; run++ {
		found, err := LeakScanCached(dir, cfg, nil, c)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 || found[0].File != "b.md" {
			t.Fatalf("run %d: want b.md's finding, got %+v", run, found)
		}
		if n := cacheKeys(t, c); n != 1 {
			t.Fatalf("run %d: want 1 cached clean file, got %d", run, n)
		}
	}
}

// Everything a result depends on is in the key: changing any of it rescans.
func TestLeakCacheMissesOnAnyInputChange(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "host corehost\n"}, []string{"a.md"})
	c := newTestCache(t, dir)
	if found, _ := LeakScanCached(dir, LeakConfig{Terms: []string{"other"}}, nil, c); len(found) != 0 {
		t.Fatalf("seed run: %+v", found)
	}
	// Config change.
	if found, _ := LeakScanCached(dir, LeakConfig{Terms: []string{"corehost"}}, nil, c); len(found) != 1 {
		t.Fatalf("a new rule must rescan a cached file, got %+v", found)
	}
	// Salt change (a rebuilt binary).
	LeakScanCached(dir, LeakConfig{Terms: []string{"other"}}, nil, c)
	c2 := &LeakCache{Path: c.Path, Salt: "rebuilt"}
	if n := cacheKeys(t, c); n != 1 {
		t.Fatalf("want the file cached, got %d", n)
	}
	LeakScanCached(dir, LeakConfig{Terms: []string{"other"}}, nil, c2)
	if n := cacheKeys(t, c2); n != 1 {
		t.Fatalf("rebuilt binary must rescan and re-record, got %d", n)
	}
}

// A worktree edit the index doesn't hold must be scanned, and never cached.
func TestLeakCacheUnstagedEditIsScanned(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "nothing here\n"}, []string{"a.md"})
	c := newTestCache(t, dir)
	cfg := LeakConfig{Terms: []string{"corehost"}}
	LeakScanCached(dir, cfg, nil, c)
	if n := cacheKeys(t, c); n != 1 {
		t.Fatalf("seed: want 1 key, got %d", n)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("host corehost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if found, _ := LeakScanCached(dir, cfg, nil, c); len(found) != 1 {
		t.Fatalf("an unstaged leak must be found despite the cache, got %+v", found)
	}
	// A clean unstaged edit is scanned but not recorded: its bytes aren't the blob.
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("still clean\n"), 0o644)
	LeakScanCached(dir, cfg, nil, c)
	if n := cacheKeys(t, c); n != 0 {
		t.Fatalf("a modified file must not be cached, got %d keys", n)
	}
}

// A staged change has a new blob id, so the cached clean result can't apply.
func TestLeakCacheStagedChangeIsScanned(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "nothing here\n"}, []string{"a.md"})
	c := newTestCache(t, dir)
	cfg := LeakConfig{Terms: []string{"corehost"}}
	LeakScanCached(dir, cfg, nil, c)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("host corehost\n"), 0o644)
	if out, err := exec.Command("git", "-C", dir, "add", "a.md").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if found, _ := LeakScanCached(dir, cfg, nil, c); len(found) != 1 {
		t.Fatalf("a staged leak must be found despite the cache, got %+v", found)
	}
}

// An intent-to-add entry carries the empty blob whatever the file holds.
func TestLeakCacheIntentToAddIsScanned(t *testing.T) {
	dir := setupRepo(t, map[string]string{"a.md": "", "b.md": "host corehost\n"}, []string{"a.md"})
	c := newTestCache(t, dir)
	cfg := LeakConfig{Terms: []string{"corehost"}}
	LeakScanCached(dir, cfg, nil, c)
	if out, err := exec.Command("git", "-C", dir, "add", "-N", "b.md").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for run := 0; run < 2; run++ {
		if found, _ := LeakScanCached(dir, cfg, nil, c); len(found) != 1 {
			t.Fatalf("run %d: an intent-to-add leak must be found, got %+v", run, found)
		}
	}
}

// Parallel scanning must report exactly what one file at a time would, in order.
func TestLeakScanManyFilesDeterministic(t *testing.T) {
	files := map[string]string{}
	var track []string
	for i := 0; i < 60; i++ {
		name := filepath.Join("d", string(rune('a'+i%26))+string(rune('a'+i/26))+".md")
		files[name] = "x\nhost corehost\n"
		track = append(track, name)
	}
	dir := setupRepo(t, files, track)
	found, err := LeakScan(dir, LeakConfig{Terms: []string{"corehost"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 60 {
		t.Fatalf("want 60 findings, got %d", len(found))
	}
	for i := 1; i < len(found); i++ {
		if found[i-1].File >= found[i].File {
			t.Fatalf("findings out of order at %d: %s then %s", i, found[i-1].File, found[i].File)
		}
	}
}
