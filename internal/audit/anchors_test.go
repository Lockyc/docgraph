package audit

import (
	"reflect"
	"sort"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Land tax is an open question worth checking":           "land-tax-is-an-open-question-worth-checking",
		"The SAP in full: four wastewater limbs plus stormwater": "the-sap-in-full-four-wastewater-limbs-plus-stormwater",
		"What's new?":         "whats-new",
		"The `foo` command":   "the-foo-command",
		"A  B":                "a--b",
		"C++ & Go":            "c--go",
		"Café au lait":        "café-au-lait",
		"snake_case name":     "snake_case-name",
		"[Link](x.md) text":   "link-text",
		"**Bold** bit":        "bold-bit",
		"_Emphasis_ here":     "emphasis-here",
		"v1.2 notes":          "v12-notes",
		"Title ##":            "title",
		"Pre-push -- gate":    "pre-push----gate",
		"<code>x</code> tag":  "x-tag",
		"Done ✓":              "done-",
		"![logo](a.png) Logo": "logo-logo",
	}
	for in, want := range cases {
		if got := slugify(headingText(in)); got != want {
			t.Errorf("slugify(headingText(%q)) = %q, want %q", in, got, want)
		}
	}
}

func anchorList(content string) []string {
	var out []string
	for a := range docAnchors(content) {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func TestDocAnchors(t *testing.T) {
	content := "---\ntype: guide\ntitle: Not A Heading\n---\n" +
		"# Setup\n" +
		"## Setup\n" +
		"### Setup\n" +
		"## Contents {#contents}\n" +
		"## Open {#open .wide}\n" +
		"Setext heading\n" +
		"==============\n" +
		"Second setext\n" +
		"---\n" +
		"```\n## Fenced heading\n```\n" +
		"    ## indented code\n" +
		"<a name=\"old-name\"></a>\n" +
		"<div id='boxed'>x</div>\n" +
		"`<span id=\"in-code\">` is an example\n" +
		"#not-a-heading\n"
	want := []string{
		"boxed", "contents", "old-name", "open",
		"second-setext", "setext-heading", "setup", "setup-1", "setup-2",
	}
	if got := anchorList(content); !reflect.DeepEqual(got, want) {
		t.Errorf("docAnchors =\n %v\nwant\n %v", got, want)
	}
}

// A heading with an explicit id also answers to its text's slug, and that slug
// still counts toward the duplicates of later headings with the same text.
func TestDocAnchorsDuplicateAfterExplicitId(t *testing.T) {
	got := anchorList("## Notes {#first}\n## Notes\n")
	want := []string{"first", "notes", "notes-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("docAnchors = %v, want %v", got, want)
	}
}

func TestLinkFragment(t *testing.T) {
	cases := []struct {
		target, frag string
		ok           bool
	}{
		{"y.md#frag", "frag", true},
		{"#frag", "frag", true},
		{"y.md", "", false},
		{"y.md#", "", false},
		{"y.md#land-tax \"Title\"", "land-tax", true},
		{"<a b.md#sec>", "sec", true},
		{"y.md#caf%C3%A9", "café", true},
		{"https://e.com/a.md#x", "x", true},
	}
	for _, c := range cases {
		frag, ok := linkFragment(c.target)
		if frag != c.frag || ok != c.ok {
			t.Errorf("linkFragment(%q) = %q, %v; want %q, %v", c.target, frag, ok, c.frag, c.ok)
		}
	}
}

// The punna shape: a link to a heading that was never written (the section is
// #land-tax, via an explicit {#id}) is a finding; links into existing headings,
// same-file fragments, duplicates and code are judged as GitHub renders them.
func TestAuditBrokenAnchors(t *testing.T) {
	fm := "---\ntype: guide\nlinks:\n  - rel: part-of\n    to: README.md\n---\n"
	dir := setupRepo(t, map[string]string{
		"README.md": "[a](docs/approvals.md) [r](docs/regulation.md)\n",
		"docs/regulation.md": fm +
			"# Regulation\n" +
			"## The SAP in full: four limbs\n" +
			"### Land tax is an open question worth checking {#land-tax}\n" +
			"## Setup\n## Setup\n" +
			"<a id=\"legacy\"></a>\n",
		"docs/approvals.md": fm +
			"| SRO | see [Regulation](regulation.md#rates) |\n" + // line 7: broken
			"ok [r](regulation.md#land-tax)\n" +
			"ok [r](regulation.md#the-sap-in-full-four-limbs)\n" +
			"ok [r](regulation.md#setup-1) bad [r](regulation.md#setup-2)\n" + // line 10
			"ok [r](regulation.md#legacy) [top](#top) [r](regulation.md#Land-Tax)\n" +
			"## Local\n" +
			"ok [here](#local) bad [here](#elsewhere)\n" + // line 13
			"```\n[fenced](regulation.md#fenced-nope)\n```\n" +
			"`[inline](regulation.md#inline-nope)`\n" +
			"[gone](missing.md#x)\n" +
			"[ext](https://example.com/a.md#nope)\n" +
			"[ref]: regulation.md#ref-nope\n", // line 20
	}, []string{"README.md", "docs/regulation.md", "docs/approvals.md"})

	rep, err := Audit(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []BrokenLink{
		{Source: "docs/approvals.md", Line: 7, Target: "docs/regulation.md#rates"},
		{Source: "docs/approvals.md", Line: 10, Target: "docs/regulation.md#setup-2"},
		{Source: "docs/approvals.md", Line: 13, Target: "docs/approvals.md#elsewhere"},
		{Source: "docs/approvals.md", Line: 20, Target: "docs/regulation.md#ref-nope"},
	}
	if !reflect.DeepEqual(rep.BrokenAnchors, want) {
		t.Errorf("BrokenAnchors =\n %+v\nwant\n %+v", rep.BrokenAnchors, want)
	}
	// A missing file is the broken-links finding, never also an anchor one.
	if len(rep.BrokenLinks) != 1 || rep.BrokenLinks[0].Target != "docs/missing.md" {
		t.Errorf("BrokenLinks = %+v, want only docs/missing.md", rep.BrokenLinks)
	}
	if CheckCounts(rep, nil)["anchors"] != len(want) {
		t.Errorf("CheckCounts[anchors] = %d, want %d", CheckCounts(rep, nil)["anchors"], len(want))
	}
}
