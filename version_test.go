package main

import "testing"

func TestVersionEmbedded(t *testing.T) {
	if version == "" {
		t.Fatal("version is empty; the VERSION file was not embedded")
	}
}
