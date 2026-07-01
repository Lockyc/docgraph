package audit

import (
	"os/exec"
	"path/filepath"
	"strings"
)

func GitRoot(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitLines(root string, args ...string) ([]string, error) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			lines = append(lines, filepath.ToSlash(l))
		}
	}
	return lines, nil
}

func trackedMD(root string) ([]string, error) {
	return gitLines(root, "ls-files", "*.md")
}

func untrackedMD(root string) ([]string, error) {
	return gitLines(root, "ls-files", "--others", "--exclude-standard", "*.md")
}
