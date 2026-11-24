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
