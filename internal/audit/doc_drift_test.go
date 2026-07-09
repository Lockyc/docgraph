package audit

import (
	"reflect"
	"testing"
)

func TestLooksLikeSymbol(t *testing.T) {
	yes := []string{"MAX_ROOTS", "OldWidget", "HTTPServer", "A_BC"}
	no := []string{"handleClick", "Window", "Math", "foo", "abc", "X1", "lowercase_only"}
	for _, s := range yes {
		if !looksLikeSymbol(s) {
			t.Errorf("looksLikeSymbol(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeSymbol(s) {
			t.Errorf("looksLikeSymbol(%q) = true, want false", s)
		}
	}
}

func TestRemovedNotReadded(t *testing.T) {
	diff := "" +
		"--- a/x.go\n+++ b/x.go\n" +
		"-type OldWidget struct{}\n" +
		"-func Helper() {}\n" +
		"+func Helper() {}\n" // Helper re-added -> not gone
	got := removedNotReadded(diff)
	want := []string{"OldWidget"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("removedNotReadded = %v, want %v", got, want)
	}
}

func TestChangedConstants(t *testing.T) {
	diff := "" +
		"-const MAX_ROOTS = 1000\n" +
		"+const MAX_ROOTS = 2000\n" +
		"-UNCHANGED = 5\n+UNCHANGED = 5\n" // same value -> not changed
	got := changedConstants(diff)
	want := []constChange{{Name: "MAX_ROOTS", Old: "1000"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedConstants = %v, want %v", got, want)
	}
}
