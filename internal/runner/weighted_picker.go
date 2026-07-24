package runner

import "math/rand/v2"

func pickWeighted(
	operations []OperationConfig,
	excluded []bool,
	random WeightedRandomSource,
) (int, bool) {
	totalWeight := 0
	for i, operation := range operations {
		if !excluded[i] {
			totalWeight += operation.Weight
		}
	}
	if totalWeight == 0 {
		return 0, false
	}

	selected := random.IntN(totalWeight)
	for i, operation := range operations {
		if excluded[i] {
			continue
		}
		if selected < operation.Weight {
			return i, true
		}
		selected -= operation.Weight
	}
	return 0, false
}

type globalRandomSource struct{}

func (globalRandomSource) IntN(n int) int {
	return rand.IntN(n)
}
