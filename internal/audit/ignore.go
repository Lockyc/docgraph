package audit

import (
	"path/filepath"
	"strings"
)

// defaultIgnores excludes non-documentation .md that would otherwise show up as
// false orphans: intentionally-untracked scratch, and agent skill/config files
// under .claude/ and .agents/ (loaded by the runtime, never part of the doc graph).
// Real docs outside docs/ (e.g. a config-dir README) are NOT excluded — they are
// documents and get audited.
var defaultIgnores = []string{"**/superpowers/**", ".claude/**", ".agents/**"}

func matchGlob(pattern, path string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchSegments(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := filepath.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// loadIgnores returns the default ignore, patterns from .docgraphignore (if
// present), then extra patterns — order preserved.
func loadIgnores(root string, extra []string) ([]string, error) {
	return loadIgnoresFrom(worktreeSource{root: root}, extra)
}

func loadIgnoresFrom(src fileSource, extra []string) ([]string, error) {
	globs := append([]string{}, defaultIgnores...)
	if content, err := src.read(".docgraphignore"); err == nil {
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				globs = append(globs, line)
			}
		}
	}
	return append(globs, extra...), nil
}

func matchesIgnore(path string, globs []string) bool {
	for _, g := range globs {
		if matchGlob(g, path) {
			return true
		}
	}
	return false
}
