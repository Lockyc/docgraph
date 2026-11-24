package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// otherSession is a second agent's Stop in the same checkout: its own session id
// and transcript, which records no edits.
func (r *sessionRepo) otherSession(id string) func() (int, string) {
	tr := filepath.Join(r.t.TempDir(), id+".jsonl")
	start := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	os.WriteFile(tr, []byte(`{"timestamp":"`+start+`"}`+"\n"), 0o644)
	return func() (int, string) {
		var errb strings.Builder
		payload := fmt.Sprintf(`{"session_id":%q,"transcript_path":%q}`, id, tr)
		code := runDocDrift([]string{r.dir}, strings.NewReader(payload), io.Discard, &errb)
		return code, errb.String()
	}
}

// On a trunk each session's symbol scan covers its own commits, so what it was
// already told is its own: another session's Stop in the same checkout, clean or
// with a finding of its own, must not make this session's finding block again.
func TestDocDriftTrunkNagIsPerSession(t *testing.T) {
	r := newSessionRepo(t)
	r.write("src/w.go", "package src\n\ntype OldWidget struct{}\n")
	r.write("src/v.go", "package src\n\ntype OtherWidget struct{}\n")
	r.write("docs/widgets.md", "---\ntype: reference\n---\n\nOldWidget and OtherWidget.\n")
	r.commitAt("2020-01-02T00:00:00Z", "widgets")
	s2 := r.otherSession("s2")

	// s1 removes OldWidget and is told, then commits it.
	r.named("src/w.go")
	r.write("src/w.go", "package src\n")
	if code, msg := r.stop("s1"); code != 2 || !strings.Contains(msg, "`OldWidget`") {
		t.Fatalf("s1 first Stop -> want 2 naming OldWidget, got %d:\n%s", code, msg)
	}
	r.commitAt("", "drop OldWidget")

	// s2 Stops clean: s1's commit is not s2's.
	if code, msg := s2(); code != 0 {
		t.Fatalf("s2 clean Stop -> want 0, got %d:\n%s", code, msg)
	}
	if code, msg := r.stop("s1"); code != 0 {
		t.Fatalf("s1 after s2's clean Stop -> OldWidget already raised, want 0, got %d:\n%s", code, msg)
	}
}

// With covers drift off, the trunk symbol scan's window still starts at the HEAD
// the session's previous Stop saw, not at the session's start.
func TestDocDriftCoversOffStillAdvancesTheWindow(t *testing.T) {
	r := newSessionRepo(t)
	t.Setenv("DOCGRAPH_COVERS_OFF", "1")
	r.change("src/a.go", "first")
	r.stop("s1")
	out, _ := exec.Command("git", "-C", r.dir, "rev-parse", "HEAD").Output()
	head := strings.TrimSpace(string(out))
	p := stopPayload{SessionID: "s1", TranscriptPath: r.transcript}
	if got, want := trunkSessionRevs(r.dir, head, p), []string{head + ".." + head}; !slices.Equal(got, want) {
		t.Fatalf("HEAD unmoved since the last Stop -> want the empty window %v, got %v", want, got)
	}
}
