package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"git.lsjc.au/lachlan/docaudit/internal/audit"
)

type multiFlag []string

func (m *multiFlag) String() string     { return "" }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docaudit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var roots, ignores multiFlag
	fs.Var(&roots, "root", "extra root doc to start reachability from (repeatable)")
	fs.Var(&ignores, "ignore", "glob to exclude from checks (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path := "."
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	}
	root, err := audit.GitRoot(path)
	if err != nil {
		fmt.Fprintf(stderr, "docaudit: not a git repository: %s\n", path)
		return 2
	}
	rep, err := audit.Audit(root, audit.Options{ExtraRoots: roots, Ignores: ignores})
	if err != nil {
		fmt.Fprintf(stderr, "docaudit: %v\n", err)
		return 2
	}
	printReport(stdout, rep)
	if rep.HasFindings() {
		return 1
	}
	return 0
}

func printReport(w io.Writer, r audit.Report) {
	fmt.Fprintf(w, "roots: %v\n", r.Roots)
	fmt.Fprintf(w, "tracked .md: %d   reachable: %d\n\n", r.TrackedMD, r.Reachable)

	fmt.Fprintf(w, "ORPHANS (%d) — tracked but unreachable by link-following:\n", len(r.Orphans))
	for _, o := range r.Orphans {
		fmt.Fprintf(w, "  %s\n", o)
	}
	fmt.Fprintf(w, "\nBROKEN LINKS (%d) — .md targets that don't exist:\n", len(r.BrokenLinks))
	for _, b := range r.BrokenLinks {
		fmt.Fprintf(w, "  %s:%d → %s\n", b.Source, b.Line, b.Target)
	}
	fmt.Fprintf(w, "\nUNTRACKED (%d) — .md on disk but not in git:\n", len(r.Untracked))
	for _, u := range r.Untracked {
		fmt.Fprintf(w, "  %s\n", u)
	}
	if !r.HasFindings() {
		fmt.Fprintln(w, "\nclean ✓")
	}
}
