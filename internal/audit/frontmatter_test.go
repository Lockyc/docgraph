package audit

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCoreVocabularies(t *testing.T) {
	if !reflect.DeepEqual(CoreTypes, []string{"runbook", "architecture", "reference", "decision", "guide", "index"}) {
		t.Errorf("CoreTypes = %v", CoreTypes)
	}
	if !reflect.DeepEqual(CoreRels, []string{"covers", "part-of", "supersedes", "depends-on", "runbook-for", "see-also", "source"}) {
		t.Errorf("CoreRels = %v", CoreRels)
	}
}

func TestDocDecode(t *testing.T) {
	src := `type: runbook
title: Restore Vaultwarden
description: recovery
tags: [vault, nucleus]
verified: 2026-06-30
review: 90d
service: vaultwarden
links:
  - rel: covers
    to: scripts/vault-restore.sh
  - rel: depends-on
    to: docs/services/proxmox.md
    note: needs nucleus reachable first
`
	var d Doc
	if err := yaml.Unmarshal([]byte(src), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Type != "runbook" || d.Title != "Restore Vaultwarden" {
		t.Errorf("type/title = %q / %q", d.Type, d.Title)
	}
	if !reflect.DeepEqual(d.Tags, []string{"vault", "nucleus"}) {
		t.Errorf("tags = %v", d.Tags)
	}
	if d.Verified != "2026-06-30" || d.Review != "90d" {
		t.Errorf("verified/review = %q / %q", d.Verified, d.Review)
	}
	if len(d.Links) != 2 {
		t.Fatalf("links len = %d, want 2: %v", len(d.Links), d.Links)
	}
	if d.Links[0].Rel != "covers" || d.Links[0].To != "scripts/vault-restore.sh" {
		t.Errorf("links[0] = %+v", d.Links[0])
	}
	if d.Links[1].Note != "needs nucleus reachable first" {
		t.Errorf("links[1].Note = %q", d.Links[1].Note)
	}
	if d.Extra["service"] != "vaultwarden" {
		t.Errorf("Extra[service] = %v, want vaultwarden", d.Extra["service"])
	}
}
