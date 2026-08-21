// This Source Code Form is licensed MPL-2.0: http://mozilla.org/MPL/2.0

package matching

import (
	"cmdiff/pkg/ir"
)

// score is one cell of the m×n similarity table.
type score struct {
	old, new int
	sim      float64
}

// SimilarityTable computes the full m×n table: all distances are always calculated
// (skipping one can only lose the best match); deliberately no input-size cap.
func SimilarityTable(old, new []*ir.SemanticBlock, oldText, newText []string) [][]float64 {
	return similarityTable(old, new, oldText, newText, BlockSimilarity)
}

// commentSimilarityTable is the full m×n table for unmatched comments
// (see commentSimilarity): comments are never compared against code blocks.
func commentSimilarityTable(old, new []*ir.SemanticBlock, oldText, newText []string) [][]float64 {
	return similarityTable(old, new, oldText, newText, commentSimilarity)
}

func similarityTable(old, new []*ir.SemanticBlock, oldText, newText []string,
	sim func(a, b *ir.SemanticBlock, aText, bText string) float64) [][]float64 {
	m, n := len(old), len(new)
	table := make([][]float64, m)
	for i := 0; i < m; i++ {
		table[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			if !kindPairEligible(old[i].Kind, new[j].Kind) {
				table[i][j] = ineligibleSim
				continue
			}
			table[i][j] = sim(old[i], new[j], oldText[i], newText[j])
		}
	}
	return table
}

// bestMatches picks the deterministic maximum-weight 1:1 assignment (deliberately not
// greedy), maximizing total similarity; unmatched vertices stay free.
func bestMatches(table [][]float64, threshold float64) (pairs []score) {
	threshold = normalizeThreshold(threshold)
	m := len(table)
	if m == 0 {
		return nil
	}
	n := len(table[0])
	if n == 0 {
		return nil
	}

	// Pad to a square matrix: dummy rows/columns = unmatched vertices (cost 1); ineligible edges cost maxCost.
	const maxCost = 1e9
	nPad := m + n
	cost := make([][]float64, nPad)
	for i := range cost {
		cost[i] = make([]float64, nPad)
		for j := range cost[i] {
			cost[i][j] = 1 // dummy edge (unmatched)
		}
	}
	for i := 0; i < m; i++ {
		for j := 0; j < n; j++ {
			// ineligibleSim and below-threshold edges cost maxCost; real eligible edges cost 1 - similarity.
			if table[i][j] >= threshold && table[i][j] != ineligibleSim {
				cost[i][j] = 1 - table[i][j]
			} else {
				cost[i][j] = maxCost
			}
		}
	}

	assign := hungarianAssignment(cost)
	for i := 0; i < m; i++ {
		if j := assign[i]; j < n {
			pairs = append(pairs, score{old: i, new: j, sim: table[i][j]})
		}
	}
	return pairs
}

// hungarianAssignment: classic O(n³) Hungarian algorithm (e-maxx) with deterministic scan order.
func hungarianAssignment(cost [][]float64) []int {
	n := len(cost)
	if n == 0 {
		return nil
	}
	const inf = 1e18
	u := make([]float64, n+1)
	v := make([]float64, n+1)
	p := make([]int, n+1)   // p[j] = row matched to column j (0 = free)
	way := make([]int, n+1) // augmenting path reconstruction
	for i := 1; i <= n; i++ {
		p[0] = i
		j0 := 0
		minv := make([]float64, n+1)
		used := make([]bool, n+1)
		for j := range minv {
			minv[j] = inf
		}
		for {
			used[j0] = true
			i0 := p[j0]
			delta := inf
			j1 := 0
			for j := 1; j <= n; j++ {
				if used[j] {
					continue
				}
				cur := cost[i0-1][j-1] - u[i0] - v[j]
				if cur < minv[j] {
					minv[j] = cur
					way[j] = j0
				}
				if minv[j] < delta {
					delta = minv[j]
					j1 = j
				}
			}
			for j := 0; j <= n; j++ {
				if used[j] {
					u[p[j]] += delta
					v[j] -= delta
				} else {
					minv[j] -= delta
				}
			}
			j0 = j1
			if p[j0] == 0 {
				break
			}
		}
		for {
			j1 := way[j0]
			p[j0] = p[j1]
			j0 = j1
			if j0 == 0 {
				break
			}
		}
	}
	assign := make([]int, n)
	for j := 1; j <= n; j++ {
		if p[j] > 0 {
			assign[p[j]-1] = j - 1
		}
	}
	return assign
}
