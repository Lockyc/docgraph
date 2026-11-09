package audit

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LeakCache remembers which files scanned clean, so a repeat run skips reading
// them. An entry is the digest of everything a file's result depends on — the
// scanning binary (Salt), the whole leaks config, the file's absolute path (which
// fixes the [[dir]] rules it falls under) and git's blob id for its content — so
// any change to any of them is a miss, never a stale hit.
//
// A file is recorded clean only when the bytes actually scanned hash to the blob
// id the index holds: a file edited mid-scan, or one a content filter rewrites on
// checkout, never matches and is simply rescanned every time. A hit additionally
// requires the index to report the worktree file unmodified (`ls-files -m`).
// Files with findings, binary files and unreadable files are never recorded.
type LeakCache struct {
	Path string // the cache file; one per worktree
	Salt string // identifies the scanning code; a rebuild must change it
}

// LeakCachePath is the cache file for the worktree at root under cacheDir.
func LeakCachePath(cacheDir, root string) string {
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(cacheDir, "docgraph", "leaks", hex.EncodeToString(sum[:8]))
}

type cacheKey [16]byte

const cacheKeyLen = len(cacheKey{})

func (c *LeakCache) load() map[cacheKey]bool {
	b, err := os.ReadFile(c.Path)
	if err != nil || len(b)%cacheKeyLen != 0 {
		return map[cacheKey]bool{}
	}
	out := make(map[cacheKey]bool, len(b)/cacheKeyLen)
	for i := 0; i < len(b); i += cacheKeyLen {
		out[cacheKey(b[i:i+cacheKeyLen])] = true
	}
	return out
}

// save replaces the cache with exactly keys — this run's clean files — so it
// never outgrows the tree. Best-effort: a cache that can't be written costs the
// next run its speed, never its result.
func (c *LeakCache) save(keys []cacheKey) {
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.Path), ".leaks-*")
	if err != nil {
		return
	}
	buf := make([]byte, 0, len(keys)*cacheKeyLen)
	for _, k := range keys {
		buf = append(buf, k[:]...)
	}
	_, werr := tmp.Write(buf)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), c.Path) != nil {
		os.Remove(tmp.Name())
	}
}

// keyFor digests one file's result inputs; base already holds the salt and config.
func keyFor(base []byte, abs, blob string) cacheKey {
	h := sha256.New()
	h.Write(base)
	h.Write([]byte{0})
	h.Write([]byte(abs))
	h.Write([]byte{0})
	h.Write([]byte(blob))
	var k cacheKey
	copy(k[:], h.Sum(nil))
	return k
}

func cacheBase(salt string, cfg LeakConfig) []byte {
	sum := sha256.Sum256([]byte(salt + "\x00" + fmt.Sprintf("%#v", cfg)))
	return sum[:]
}

// gitBlobID is the object id git gives content, in the hash the repo uses
// (inferred from the index's id length).
func gitBlobID(content string, like string) string {
	var h hash.Hash
	if len(like) == 64 {
		h = sha256.New()
	} else {
		h = sha1.New()
	}
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// indexEntry is one path from `git ls-files -s`; blob is "" when the path is
// unmerged (several stages), which makes it uncacheable.
type indexEntry struct {
	path, blob string
}

// lsFilesStaged lists tracked paths with their index blob ids, one entry per path.
func lsFilesStaged(root string) ([]indexEntry, error) {
	out, err := GitCmd(root, "ls-files", "-s", "-z").Output()
	if err != nil {
		return nil, gitError(err)
	}
	var entries []indexEntry
	seen := map[string]int{}
	for _, rec := range strings.Split(string(out), "\x00") {
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta) // mode, object, stage
		if len(f) != 3 {
			continue
		}
		if i, dup := seen[path]; dup {
			entries[i].blob = ""
			continue
		}
		blob := f[1]
		if f[2] != "0" {
			blob = ""
		}
		seen[path] = len(entries)
		entries = append(entries, indexEntry{path: path, blob: blob})
	}
	return entries, nil
}

// lsFilesModified is the set of tracked paths whose worktree file differs from
// the index (edited or deleted).
func lsFilesModified(root string) (map[string]bool, error) {
	out, err := GitCmd(root, "ls-files", "-m", "-z").Output()
	if err != nil {
		return nil, gitError(err)
	}
	m := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			m[p] = true
		}
	}
	return m, nil
}
