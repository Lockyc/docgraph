package audit

import (
	"reflect"
	"strings"
	"testing"
)

func TestChangeIdentsReadsChangedLinesAndHunkContext(t *testing.T) {
	diff := "" +
		"diff --git a/src/a.go b/src/a.go\n--- a/src/a.go\n+++ b/src/a.go\n" +
		"@@ -10,3 +10,3 @@ func Login(user string) error {\n" +
		" \tunchanged := ContextOnly()\n" +
		"-\treturn checkToken(user)\n" +
		"+\treturn verifyToken(user)\n" +
		"diff --git a/src/b.go b/src/b.go\n--- a/src/b.go\n+++ b/src/b.go\n" +
		"@@ -1 +1 @@\n" +
		"-const MaxRetries = 3\n+const MaxRetries = 4\n"
	got := changeIdents(diff)
	for _, want := range []string{"Login", "checkToken", "verifyToken"} {
		if !got["src/a.go"][want] {
			t.Errorf("src/a.go idents missing %q: %v", want, got["src/a.go"])
		}
	}
	// Plain lowercase words (user, return) are too common to point at anything.
	for _, not := range []string{"unchanged", "ContextOnly", "MaxRetries", "user", "return"} {
		if got["src/a.go"][not] {
			t.Errorf("src/a.go idents must not include %q", not)
		}
	}
	if !got["src/b.go"]["MaxRetries"] {
		t.Errorf("src/b.go idents missing MaxRetries: %v", got["src/b.go"])
	}
}

// A restyled line still calls what it called: identifiers on both sides of a
// hunk are not changes, and a hunk with none changed adds no function context.
func TestChangeIdentsIgnoresUnchangedTokensOnChangedLines(t *testing.T) {
	diff := "+++ b/view.html\n@@ -5 +5 @@ function renderPanel() {\n" +
		"-<td class=\"text-sm\">{{ order.isOverdue() }}</td>\n" +
		"+<td class=\"cell\">{{ order.isOverdue() }}</td>\n"
	if got := changeIdents(diff)["view.html"]; len(got) != 0 {
		t.Fatalf("want no identifiers from a restyle, got %v", got)
	}
}

func TestPointCoversNamesCodeLinesOnly(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"docs/auth.md": "---\ntype: reference\nlinks:\n  - rel: covers\n    to: src/auth.go\n---\n\n" +
			"# Auth\n\n" +
			"`Login` checks the token.\n" + // line 10: code span names a changed ident
			"Login is described in prose.\n" + // prose mention: not code, not pointed
			"`Logout` is untouched.\n" +
			"```go\nerr := verifyToken(u)\n```\n", // line 14: fenced code
		"src/auth.go": "package auth\n",
	}, []string{"docs/auth.md", "src/auth.go"})
	diff := "+++ b/src/auth.go\n@@ -1 +1 @@ func Login() {\n+\treturn verifyToken(u)\n"
	fs := PointCovers(dir, []CoversFinding{{Doc: "docs/auth.md", Paths: []string{"src/auth.go"}}}, diff)
	var lines []int
	for _, h := range fs[0].Lines {
		lines = append(lines, h.Line)
	}
	if !reflect.DeepEqual(lines, []int{10, 14}) {
		t.Fatalf("pointed lines = %v (%+v), want [10 14]", lines, fs[0].Lines)
	}
}

// An identifier named on many of the doc's lines is its subject, not a claim:
// it points nowhere, while a rarer one on the same line still does.
func TestPointCoversSkipsTheDocsSubject(t *testing.T) {
	dir := setupRepo(t, map[string]string{
		"docs/f.md": "---\ntype: reference\n---\n# F\n" +
			"`Billing\\Invoice` a\n`Billing\\Ledger` b\n`Billing\\Mail` c\n`Billing\\Refund` d\n" +
			"`Billing\\TaxRate` e\n",
	}, []string{"docs/f.md"})
	diff := "+++ b/src/x.php\n@@ -1 +1 @@\n+use Billing\\TaxRate;\n"
	fs := PointCovers(dir, []CoversFinding{{Doc: "docs/f.md", Paths: []string{"src/x.php"}}}, diff)
	if len(fs[0].Lines) != 1 || fs[0].Lines[0].Line != 9 {
		t.Fatalf("want only line 9 (TaxRate), got %+v", fs[0].Lines)
	}
}

func TestCoversChangeDiffIncludesUntrackedFiles(t *testing.T) {
	dir := setupRepo(t, map[string]string{"README.md": "x\n"}, []string{"README.md"})
	gitIn(t, dir)("commit", "-qm", "init")
	writeFile(t, dir, "src/new.go", "package src\n\nfunc BrandNew() {}\n")
	diff, err := CoversChangeDiff(dir, nil, []string{"src/new.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !changeIdents(diff)["src/new.go"]["BrandNew"] {
		t.Fatalf("untracked file's identifiers missing from:\n%s", diff)
	}
	if strings.Contains(diff, "README") {
		t.Fatalf("diff must be scoped to the given paths:\n%s", diff)
	}
}
