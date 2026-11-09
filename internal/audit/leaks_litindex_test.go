package audit

import (
	"math/rand"
	"strings"
	"testing"
)

// The one-pass index must report exactly what a per-literal strings.Contains
// would — a literal it misses is a file the prefilter wrongly rejects.
func TestLitIndexAgreesWithContains(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const alpha = "abcs:/=_ \n"
	randStr := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alpha[rng.Intn(len(alpha))]
		}
		return string(b)
	}
	for round := 0; round < 300; round++ {
		var lits []string
		for i := 0; i < 1+rng.Intn(12); i++ {
			lits = append(lits, randStr(1+rng.Intn(4)))
		}
		stop := make([]bool, len(lits))
		x := newLitIndex(lits, stop)
		text := randStr(rng.Intn(400))
		found, stopped := x.scan(text)
		if stopped {
			t.Fatal("no stop literals, yet scan stopped")
		}
		for i, l := range lits {
			if found[i] != strings.Contains(text, l) {
				t.Fatalf("lit %q in %q: index says %v", l, text, found[i])
			}
		}
	}
}

func TestLitIndexStopsOnStopLiteral(t *testing.T) {
	x := newLitIndex([]string{"zzz", "abc"}, []bool{false, true})
	if _, stopped := x.scan("xx abc yy"); !stopped {
		t.Fatal("a stop literal must stop the scan")
	}
	found, stopped := x.scan("xx zzz yy")
	if stopped || !found[0] {
		t.Fatalf("non-stop literal: found=%v stopped=%v", found, stopped)
	}
}

// Past 255 distinct starting pairs the index falls back to per-literal search,
// which must still find everything.
func TestLitIndexManyPairsFallsBack(t *testing.T) {
	var lits []string
	for a := 'a'; a <= 'z'; a++ {
		for b := 'a'; b <= 'k'; b++ {
			lits = append(lits, string([]rune{a, b, '!'}))
		}
	}
	x := newLitIndex(lits, make([]bool, len(lits)))
	found, _ := x.scan("... zk! ...")
	for i, l := range lits {
		if found[i] != (l == "zk!") {
			t.Fatalf("lit %q: found=%v", l, found[i])
		}
	}
}
