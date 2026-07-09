package audit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// keys returns the keys of m in no particular order.
func keys(m map[string]string) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}

// trim is strings.TrimSpace, aliased for brevity in this file's tests.
func trim(s string) string { return strings.TrimSpace(s) }

// writeFile writes content to path under dir, creating parent dirs as needed.
func writeFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitRepo builds a repo, commits `base` content, then commits `head` content,
// returning (dir, baseSHA, headSHA). Files map path→content for each snapshot.
func commitRepo(t *testing.T, base, head map[string]string) (string, string, string) {
	t.Helper()
	dir := setupRepo(t, base, keys(base))
	git := func(a ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
		return string(out)
	}
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "base")
	baseSHA := trim(git("rev-parse", "HEAD"))
	for p, c := range head {
		writeFile(t, dir, p, c)
		git("add", p)
	}
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "head")
	headSHA := trim(git("rev-parse", "HEAD"))
	return dir, baseSHA, headSHA
}

func TestFootgunDriftFlagsAddedDeclaration(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{"CLAUDE.md": "intro\n"},
		map[string]string{"CLAUDE.md": "intro\n\n- **Footgun:** no why here.\n"},
	)
	got, err := FootgunDrift(dir, []RevRange{{Base: base, Head: head}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].File != "CLAUDE.md" || got[0].Line != 3 {
		t.Fatalf("want one finding at CLAUDE.md:3, got %+v", got)
	}
}

func TestFootgunDriftIgnoresPreexistingDeclaration(t *testing.T) {
	// The unjustified declaration exists in BASE already; head only adds unrelated text.
	dir, base, head := commitRepo(t,
		map[string]string{"CLAUDE.md": "- **Footgun:** no why.\n"},
		map[string]string{"CLAUDE.md": "- **Footgun:** no why.\n\nunrelated new line\n"},
	)
	got, err := FootgunDrift(dir, []RevRange{{Base: base, Head: head}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("pre-existing declaration (not added in range) must not flag, got %+v", got)
	}
}

// A rationale word no longer excuses an added declaration — every one flags, so
// the pusher is nagged to verify it's a real footgun regardless of wording.
func TestFootgunDriftFlagsAddedDeclarationDespiteRationale(t *testing.T) {
	dir, base, head := commitRepo(t,
		map[string]string{"CLAUDE.md": "intro\n"},
		map[string]string{"CLAUDE.md": "intro\n\n- **Footgun:** don't, because it races.\n"},
	)
	got, err := FootgunDrift(dir, []RevRange{{Base: base, Head: head}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Line != 3 {
		t.Fatalf("added declaration must flag even with a rationale word, got %+v", got)
	}
}
