package audit

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const defaultIgnore = "**/superpowers/**"

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

// loadIgnores returns the default ignore, patterns from .docauditignore (if
// present), then extra patterns — order preserved.
func loadIgnores(root string, extra []string) ([]string, error) {
	globs := []string{defaultIgnore}
	f, err := os.Open(filepath.Join(root, ".docauditignore"))
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				globs = append(globs, line)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
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
