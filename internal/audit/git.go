package audit

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// GitRoot resolves path's work-tree root. On failure the error carries git's own
// stderr (gitError): a linked worktree whose `.git` gitdir pointer no longer
// resolves fails here and is otherwise indistinguishable from "not a repo" —
// only git's message names the pointer target as the cause.
func GitRoot(path string) (string, error) {
	out, err := GitCmd(path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", gitError(err)
	}
	return strings.TrimSpace(string(out)), nil
}

// gitError replaces an exec failure with the stderr git wrote, when it wrote any.
// exec.Cmd.Output() stashes it on *ExitError and it is the only place the real
// cause appears; "exit status 128" on its own is worse than useless.
func gitError(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}
	return err
}

// GitCmd builds a git command run in dir. core.quotePath is off so a non-ASCII
// path comes back verbatim: quoted ("caf\303\251.md") it names no file, and every
// check that opens or matches it silently drops it.
func GitCmd(dir string, args ...string) *exec.Cmd {
	return exec.Command("git", append([]string{"-C", dir, "-c", "core.quotePath=false"}, args...)...)
}

func gitLines(root string, args ...string) ([]string, error) {
	out, err := GitCmd(root, args...).Output()
	if err != nil {
		return nil, gitError(err)
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

// gitRawLines runs git and splits stdout on "\n" WITHOUT trimming, slash-
// converting, or dropping empty lines — required for diff parsing where '+'/'-'
// prefixes and blank added lines are significant.
func gitRawLines(root string, args ...string) ([]string, error) {
	out, err := GitCmd(root, args...).Output()
	if err != nil {
		return nil, gitError(err)
	}
	s := string(out)
	s = strings.TrimSuffix(s, "\n") // drop only the trailing record separator
	if s == "" {
		return nil, nil
	}
	return strings.Split(s, "\n"), nil
}

// changedMarkdown lists .md files changed in the given range (e.g. "A..B").
func changedMarkdown(root, rng string) ([]string, error) {
	return gitLines(root, "diff", "--name-only", rng, "--", "*.md")
}

// changedCode lists the non-prose ("code") files changed in the given diff spec.
// It shares nonCodePathspec with gitDiff/stillDefinedInCode on purpose: all three
// answer questions about the same "what is code?" set, so a divergent definition
// here would report drift against a file class the diff never scanned.
func changedCode(root, spec string) ([]string, error) {
	args := append([]string{"diff", "--name-only", spec, "--"}, nonCodePathspec...)
	return gitLines(root, args...)
}

// SpecChanges returns the code paths and markdown changed in one git-diff spec
// ("A..B", or a single rev diffed against the working tree). The markdown side is
// skipped when no code changed: nothing can drift without a code change.
func SpecChanges(root, spec string) (code, md []string, err error) {
	if code, err = changedCode(root, spec); err != nil || len(code) == 0 {
		return code, nil, err
	}
	md, err = changedMarkdown(root, spec)
	return code, md, err
}

// CommitChanges returns the code paths and markdown changed by the commits
// `git log --first-parent --no-merges revArgs` selects, each against its own
// parent. Merge commits are excluded so that merging a trunk in never imports
// other people's changes, and first-parent keeps the merged side's commits out.
func CommitChanges(root string, revArgs ...string) (code, md []string, err error) {
	base := append([]string{"log", "--first-parent", "--no-merges", "--name-only", "--format="}, revArgs...)
	if code, err = gitLines(root, append(append(append([]string{}, base...), "--"), nonCodePathspec...)...); err != nil || len(code) == 0 {
		return dedupe(code), nil, err
	}
	md, err = gitLines(root, append(append([]string{}, base...), "--", "*.md")...)
	return dedupe(code), dedupe(md), err
}

// WorktreeChanges returns the code paths and markdown that differ from HEAD in
// the working tree: staged, unstaged, and untracked-but-not-ignored.
func WorktreeChanges(root string) (code, md []string, err error) {
	if code, err = changedCode(root, "HEAD"); err != nil {
		return nil, nil, err
	}
	newCode, err := gitLines(root, append([]string{"ls-files", "--others", "--exclude-standard", "--"}, nonCodePathspec...)...)
	if err != nil {
		return nil, nil, err
	}
	if code = append(code, newCode...); len(code) == 0 {
		return nil, nil, nil
	}
	if md, err = changedMarkdown(root, "HEAD"); err != nil {
		return nil, nil, err
	}
	newMD, err := untrackedMD(root)
	if err != nil {
		return nil, nil, err
	}
	return dedupe(code), dedupe(append(md, newMD...)), nil
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// addedLines returns the set of new-file line numbers added to path in rng, by
// parsing unified-diff hunk headers and counting '+' lines from the new start.
func addedLines(root, rng, path string) (map[int]bool, error) {
	lines, err := gitRawLines(root, "diff", "--unified=0", rng, "--", path)
	if err != nil {
		return nil, err
	}
	added := map[int]bool{}
	newLine := 0
	inHunk := false // file headers (---/+++) come only before the first hunk
	for _, l := range lines {
		if !inHunk && !strings.HasPrefix(l, "@@") {
			continue
		}
		if strings.HasPrefix(l, "@@") {
			inHunk = true
			// @@ -a,b +c,d @@
			plus := strings.Index(l, "+")
			if plus < 0 {
				continue
			}
			rest := l[plus+1:]
			end := strings.IndexAny(rest, " ,")
			if end < 0 {
				end = len(rest)
			}
			start, e := strconv.Atoi(rest[:end])
			if e != nil {
				continue
			}
			newLine = start
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			added[newLine] = true
			newLine++
		case strings.HasPrefix(l, "-"), strings.HasPrefix(l, `\`):
			// deletion or "\ No newline at end of file": new-file line does not advance
		default:
			// context (none with --unified=0) advances new line
			newLine++
		}
	}
	return added, nil
}

// trackedAtRef lists the tracked "*.md" paths at ref from the object store, so it
// works on a bare repo (no work-tree). It enumerates all files at the ref via
// ls-tree and filters to .md — the same effective set trackedMD's "*.md" pathspec
// yields in a checkout, without depending on pathspec-glob semantics.
func trackedAtRef(gitDir, ref string) ([]string, error) {
	lines, err := gitLines(gitDir, "ls-tree", "-r", "--full-tree", "--name-only", ref)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range lines {
		if strings.HasSuffix(l, ".md") {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out, nil
}

// fileAtRev returns path's content at rev; ok=false if it doesn't exist there.
func fileAtRev(root, rev, path string) (string, bool) {
	out, err := GitCmd(root, "show", rev+":"+path).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// ClosestBase returns the merge-base of head with the nearest integration branch
// (fewest commits in base..head). Used only for a new-branch push with no upstream.
func ClosestBase(root, head string) (string, bool) {
	best, bestCnt := "", -1
	for _, cand := range []string{"origin/HEAD", "main", "master", "dev", "develop", "trunk"} {
		mb, err := GitCmd(root, "merge-base", head, cand).Output()
		if err != nil {
			continue
		}
		base := strings.TrimSpace(string(mb))
		if base == "" {
			continue
		}
		cntOut, err := GitCmd(root, "rev-list", "--count", base+".."+head).Output()
		if err != nil {
			continue
		}
		cnt, err := strconv.Atoi(strings.TrimSpace(string(cntOut)))
		if err != nil {
			continue
		}
		if bestCnt < 0 || cnt < bestCnt {
			best, bestCnt = base, cnt
		}
	}
	return best, best != ""
}
