package core

import (
	"math"
	"math/rand"
)

// Config holds optional ACO parameters.
type Config struct {
	Alpha       float64
	Beta        float64
	Evaporation float64
	Q           float64
	AntCount    int
}

// Result holds the best route and its total distance.
type Result struct {
	Route []int
	Dist  float64
}

// Agent is the ACO solver.
type Agent struct {
	distMatrix [][]float64
	tauMatrix  [][]float64
	n          int

	alpha       float64
	beta        float64
	evaporation float64
	q           float64
	antCount    int
}

// NewAgent constructs a new Agent.
// distMatrix must be an N×N symmetric matrix of distances.
// Zero values in config fields fall back to the same defaults as the JS version.
func NewAgent(distMatrix [][]float64, cfg Config) *Agent {
	n := len(distMatrix)

	// initialise pheromone matrix to 1.0
	tau := make([][]float64, n)
	for i := range tau {
		tau[i] = make([]float64, n)
		for j := range tau[i] {
			tau[i][j] = 1.0
		}
	}

	// apply defaults (mirrors JS defaults)
	alpha := 1.0
	if cfg.Alpha != 0 {
		alpha = cfg.Alpha
	}
	beta := 5.0
	if cfg.Beta != 0 {
		beta = cfg.Beta
	}
	evaporation := 0.1
	if cfg.Evaporation != 0 {
		evaporation = cfg.Evaporation
	}
	q := 100.0
	if cfg.Q != 0 {
		q = cfg.Q
	}
	antCount := 5
	if cfg.AntCount != 0 {
		antCount = cfg.AntCount
	}

	return &Agent{
		distMatrix:  distMatrix,
		tauMatrix:   tau,
		n:           n,
		alpha:       alpha,
		beta:        beta,
		evaporation: evaporation,
		q:           q,
		antCount:    antCount,
	}
}

// TwoOpt applies the 2-opt local search heuristic to improve a given route.
func (a *Agent) TwoOpt(route []int) []int {
	n := len(route)
	if n < 3 {
		return route
	}

	improved := true
	bestRoute := make([]int, n)
	copy(bestRoute, route)

	for improved {
		improved = false
		for i := 1; i < n-1; i++ {
			for k := i + 1; k < n; k++ {
				v1 := bestRoute[i-1]
				v2 := bestRoute[i]
				v3 := bestRoute[k]
				v4 := bestRoute[(k+1)%n]

				currentDist := a.distMatrix[v1][v2] + a.distMatrix[v3][v4]
				newDist := a.distMatrix[v1][v3] + a.distMatrix[v2][v4]

				if newDist < currentDist-1e-6 {
					for start, end := i, k; start < end; start, end = start+1, end-1 {
						bestRoute[start], bestRoute[end] = bestRoute[end], bestRoute[start]
					}
					improved = true
				}
			}
		}
	}
	return bestRoute
}

// RouteLength calculates the total distance of a circular route.
func (a *Agent) RouteLength(route []int) float64 {
	total := 0.0
	for i := 0; i < len(route); i++ {
		total += a.distMatrix[route[i]][route[(i+1)%len(route)]]
	}
	return total
}

// constructRoute builds a single ant's tour using pheromone + heuristic probabilities.
func (a *Agent) constructRoute() []int {
	visited := make([]bool, a.n)
	route := []int{0} // always start at node 0
	visited[0] = true

	for step := 1; step < a.n; step++ {
		current := route[len(route)-1]

		weights := make([]float64, a.n)
		hasWeight := make([]bool, a.n)
		total := 0.0

		for j := 0; j < a.n; j++ {
			if visited[j] {
				continue
			}
			d := a.distMatrix[current][j]
			if d == 0 || math.IsInf(d, 0) {
				continue
			}

			tau := math.Pow(a.tauMatrix[current][j], a.alpha)
			// scaled heuristic (100/d)^beta to avoid floating-point underflow
			eta := math.Pow(100.0/d, a.beta)
			w := tau * eta

			weights[j] = w
			hasWeight[j] = true
			total += w
		}

		// roulette-wheel selection
		next := -1
		r := rand.Float64()
		cumulative := 0.0

		for j := 0; j < a.n; j++ {
			if !hasWeight[j] {
				continue
			}
			cumulative += weights[j] / total
			if r <= cumulative {
				next = j
				break
			}
		}

		// fallback for floating-point edge cases
		if next == -1 {
			for j := a.n - 1; j >= 0; j-- {
				if !visited[j] {
					next = j
					break
				}
			}
		}

		route = append(route, next)
		visited[next] = true
	}

	return route
}

// evaporate reduces pheromone levels on all edges by the evaporation rate.
func (a *Agent) evaporate() {
	for i := 0; i < a.n; i++ {
		for j := 0; j < a.n; j++ {
			v := a.tauMatrix[i][j] * (1 - a.evaporation)
			if v < 0.01 {
				v = 0.01
			}
			a.tauMatrix[i][j] = round4(v)
		}
	}
}

// deposit adds pheromone to every edge in the given route.
func (a *Agent) deposit(route []int, dist float64) {
	delta := a.q / dist
	for k := 0; k < len(route); k++ {
		i := route[k]
		j := route[(k+1)%len(route)]
		a.tauMatrix[i][j] = round4(a.tauMatrix[i][j] + delta)
		a.tauMatrix[j][i] = round4(a.tauMatrix[j][i] + delta)
	}
}

// Run executes the ACO simulation for the given number of iterations.
// It mirrors the JS run() logic exactly: evaporate first, then deploy ants
// one at a time with immediate pheromone deposit so each subsequent ant
// sees the updated matrix.
func (a *Agent) Run(iterations int) Result {
	if iterations <= 0 {
		iterations = 3
	}

	globalBest := Result{Dist: math.Inf(1)}

	for iter := 1; iter <= iterations; iter++ {
		a.evaporate()

		iterBest := Result{Dist: math.Inf(1)}

		for antID := 0; antID < a.antCount; antID++ {
			route := a.constructRoute()
			route = a.TwoOpt(route)
			dist := round4(a.RouteLength(route))

			// deposit immediately so the next ant sees updated pheromones
			a.deposit(route, dist)

			if dist < iterBest.Dist {
				iterBest = Result{Route: append([]int(nil), route...), Dist: dist}
			}
		}

		if iterBest.Dist < globalBest.Dist {
			globalBest = Result{Route: append([]int(nil), iterBest.Route...), Dist: iterBest.Dist}
		}
	}

	return globalBest
}

// round4 rounds a float64 to 4 decimal places, mirroring JS toFixed(4).
func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// GetTauMatrix returns a deep copy of the current pheromone matrix.
func (a *Agent) GetTauMatrix() [][]float64 {
	cpy := make([][]float64, a.n)
	for i := 0; i < a.n; i++ {
		cpy[i] = make([]float64, a.n)
		copy(cpy[i], a.tauMatrix[i])
	}
	return cpy
}

// BlendPheromones merges a global pheromone matrix into the local one using a blendRatio.
func (a *Agent) BlendPheromones(globalTau [][]float64, blendRatio float64) {
	for i := 0; i < a.n; i++ {
		for j := 0; j < a.n; j++ {
			val := (1.0-blendRatio)*a.tauMatrix[i][j] + blendRatio*globalTau[i][j]
			a.tauMatrix[i][j] = round4(val)
		}
	}
}

// Disrupt partially randomizes the pheromone matrix by resetting values toward 1.0.
func (a *Agent) Disrupt(strength float64) {
	for i := 0; i < a.n; i++ {
		for j := 0; j < a.n; j++ {
			val := (1.0-strength)*a.tauMatrix[i][j] + strength*1.0
			a.tauMatrix[i][j] = round4(val)
		}
	}
}

// SetAntCount dynamically adjusts the number of ants the agent runs.
func (a *Agent) SetAntCount(count int) {
	if count < 1 {
		count = 1
	}
	a.antCount = count
}

// GetAntCount returns the current number of ants the agent uses.
func (a *Agent) GetAntCount() int {
	return a.antCount
}

// InjectBestRoute deposits pheromone along a known-good route into the agent's
// local pheromone matrix. The strength parameter controls how aggressively the
// global signal is applied (typically the ratio agentBest/globalBest, capped).
// This is the mechanism by which global knowledge feeds back into local search
// without erasing the agent's own discoveries.
func (a *Agent) InjectBestRoute(route []int, dist float64, strength float64) {
	if len(route) == 0 || dist <= 0 {
		return
	}
	delta := (a.q / dist) * strength
	for k := 0; k < len(route); k++ {
		i := route[k]
		j := route[(k+1)%len(route)]
		a.tauMatrix[i][j] = round4(a.tauMatrix[i][j] + delta)
		a.tauMatrix[j][i] = round4(a.tauMatrix[j][i] + delta)
	}
}
