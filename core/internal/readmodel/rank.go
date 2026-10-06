package readmodel

import (
	"errors"
	"math"
)

var ErrRankGap = errors.New("rank_gap_exhausted")

// RankBetween uses two indexed neighbors supplied by the repository. Nil is
// an open column boundary. Never rebalance a column in the request transaction.
// Unsigned subtraction handles the full signed rank range without overflow.
func RankBetween(left, right *int64) (int64, error) {
	const spacing int64 = 1024
	if left == nil && right == nil {
		return 0, nil
	}
	if right == nil {
		if *left > math.MaxInt64-spacing {
			return 0, ErrRankGap
		}
		return *left + spacing, nil
	}
	if left == nil {
		if *right < math.MinInt64+spacing {
			return 0, ErrRankGap
		}
		return *right - spacing, nil
	}
	if *left >= *right {
		return 0, ErrRankGap
	}
	gap := uint64(*right) - uint64(*left)
	if gap < 2 {
		return 0, ErrRankGap
	}
	return int64(uint64(*left) + gap/2), nil
}
