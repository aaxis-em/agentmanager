package core

import (
	"context"
	"math/rand"
	"time"
)

// CalculateTourDistance computes the total distance of a given tour.
func CalculateTourDistance(tour []int, matrix [][]float64) float64 {
	dist := 0.0
	for i := 0; i < len(tour)-1; i++ {
		dist += matrix[tour[i]][tour[i+1]]
	}
	dist += matrix[tour[len(tour)-1]][tour[0]] // return to start
	return dist
}

// GenerateRandomTour creates a random permutation of cities.
func GenerateRandomTour(n int) []int {
	tour := make([]int, n)
	for i := 0; i < n; i++ {
		tour[i] = i
	}
	for i := len(tour) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		tour[i], tour[j] = tour[j], tour[i]
	}
	return tour
}

// CloneTour safely copies a slice of ints.
func CloneTour(tour []int) []int {
	c := make([]int, len(tour))
	copy(c, tour)
	return c
}

// reverse reverses elements in a slice in place between indices i and j.
func reverse(tour []int, i, j int) {
	for i < j {
		tour[i], tour[j] = tour[j], tour[i]
		i++
		j--
	}
}

// RunAgent perpetually runs Two-Opt optimization on random tours until cancelled.
func RunAgent(ctx context.Context, id int, matrix [][]float64, reportImprovement func(path []int, distance float64)) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(id)))
	n := len(matrix)
	if n < 3 {
		return
	}

	// Wait randomly just to stagger the agents slightly.
	time.Sleep(time.Duration(rng.Intn(100)) * time.Millisecond)

	bestTour := GenerateRandomTour(n)
	bestDist := CalculateTourDistance(bestTour, matrix)
	reportImprovement(bestTour, bestDist)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			currentTour := CloneTour(bestTour)
			currentDist := CalculateTourDistance(currentTour, matrix)

			// Random perturbation to escape local minima
			if rng.Float64() < 0.15 {
				currentTour = GenerateRandomTour(n)
				currentDist = CalculateTourDistance(currentTour, matrix)
			} else {
				i := rng.Intn(n)
				j := rng.Intn(n)
				currentTour[i], currentTour[j] = currentTour[j], currentTour[i]
				currentDist = CalculateTourDistance(currentTour, matrix)
			}

			// Local Search: rigorous randomized 2-opt
			improved := true
			for improved {
				select {
				case <-ctx.Done():
					return
				default:
				}
				improved = false
				for i := 0; i < n-1; i++ {
					for j := i + 2; j < n; j++ {
						// wrap around case optimization skipped to avoid reversing entire array back-and-forth
						if i == 0 && j == n-1 {
							continue
						}

						costBefore := matrix[currentTour[i]][currentTour[i+1]] + matrix[currentTour[j]][currentTour[(j+1)%n]]
						costAfter := matrix[currentTour[i]][currentTour[j]] + matrix[currentTour[i+1]][currentTour[(j+1)%n]]

						if costAfter < costBefore {
							// Valid 2-opt move that reduces distance
							reverse(currentTour, i+1, j)
							currentDist = currentDist - costBefore + costAfter
							improved = true

							// Dynamically check against global best and report so it doesn't hang!
							if currentDist < bestDist {
								bestDist = currentDist
								bestTour = CloneTour(currentTour)
								reportImprovement(bestTour, bestDist)
							}
						}
					}
				}
			}
		}
	}
}
