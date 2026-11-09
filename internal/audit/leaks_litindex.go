package audit

import "strings"

// litIndex finds which of a fixed set of lowercased literals occur in a text in
// one pass, instead of one strings.Contains pass per literal: with a few dozen
// literals over hundreds of MB, the per-literal passes were the prefilter's
// largest cost. Every position whose two-byte pair begins some literal is checked
// against just the literals sharing that pair.
type litIndex struct {
	lits   []string
	stop   []bool  // a hit on this literal settles mayMatch on its own
	single []int   // literals of length 1, found by IndexByte
	slot   []uint8 // pair (b0<<8|b1) → 1-based index into bucket; 0 = no literal starts so
	bucket [][]int // literal indices sharing a starting pair
}

// newLitIndex indexes lits; stop marks the ones whose presence alone answers.
// Past 255 distinct starting pairs the index degrades to per-literal search.
func newLitIndex(lits []string, stop []bool) litIndex {
	x := litIndex{lits: lits, stop: stop}
	for i, l := range lits {
		if len(l) == 1 {
			x.single = append(x.single, i)
			continue
		}
		if x.slot == nil {
			x.slot = make([]uint8, 1<<16)
		}
		k := int(l[0])<<8 | int(l[1])
		if x.slot[k] == 0 {
			if len(x.bucket) == 255 {
				return litIndex{lits: lits, stop: stop, single: allIdx(len(lits))}
			}
			x.bucket = append(x.bucket, nil)
			x.slot[k] = uint8(len(x.bucket))
		}
		x.bucket[x.slot[k]-1] = append(x.bucket[x.slot[k]-1], i)
	}
	return x
}

func allIdx(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// scan reports which literals occur in low (found[i]) and stops early with
// stopped=true as soon as a stop literal is seen.
func (x litIndex) scan(low string) (found []bool, stopped bool) {
	found = make([]bool, len(x.lits))
	for _, i := range x.single {
		if strings.Contains(low, x.lits[i]) {
			if x.stop[i] {
				return found, true
			}
			found[i] = true
		}
	}
	if x.slot == nil {
		return found, false
	}
	slot := x.slot
	for p := 0; p+1 < len(low); p++ {
		s := slot[int(low[p])<<8|int(low[p+1])]
		if s == 0 {
			continue
		}
		for _, i := range x.bucket[s-1] {
			if !found[i] && strings.HasPrefix(low[p:], x.lits[i]) {
				if x.stop[i] {
					return found, true
				}
				found[i] = true
			}
		}
	}
	return found, false
}
