package twoopsaco

import (
	"math"
	"math/rand"
)

type Result struct {
	Route []int
	Dist  float64
}

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

func NewAgent(distMatrix, tauMatrix [][]float64, n int, alpha, beta, evaporation, q float64, antCount int) *Agent {
	return &Agent{
		distMatrix:  distMatrix,
		tauMatrix:   tauMatrix,
		n:           n,
		alpha:       alpha,
		beta:        beta,
		evaporation: evaporation,
		q:           q,
		antCount:    antCount,
	}
}

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

func (a *Agent) RouteLength(route []int) float64 {
	total := 0.0
	for i := 0; i < len(route); i++ {
		total += a.distMatrix[route[i]][route[(i+1)%len(route)]]
	}
	return total
}

func (a *Agent) constructRoute() []int {
	visited := make([]bool, a.n)
	route := []int{0}
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
			eta := math.Pow(100.0/d, a.beta)
			w := tau * eta

			weights[j] = w
			hasWeight[j] = true
			total += w
		}

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

func (a *Agent) deposit(route []int, dist float64) {
	delta := a.q / dist
	for k := 0; k < len(route); k++ {
		i := route[k]
		j := route[(k+1)%len(route)]
		a.tauMatrix[i][j] = round4(a.tauMatrix[i][j] + delta)
		a.tauMatrix[j][i] = round4(a.tauMatrix[j][i] + delta)
	}
}

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

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

func (a *Agent) GetTauMatrix() [][]float64 {
	cpy := make([][]float64, a.n)
	for i := 0; i < a.n; i++ {
		cpy[i] = make([]float64, a.n)
		copy(cpy[i], a.tauMatrix[i])
	}
	return cpy
}

func (a *Agent) BlendPheromones(globalTau [][]float64, blendRatio float64) {
	for i := 0; i < a.n; i++ {
		for j := 0; j < a.n; j++ {
			val := (1.0-blendRatio)*a.tauMatrix[i][j] + blendRatio*globalTau[i][j]
			a.tauMatrix[i][j] = round4(val)
		}
	}
}

func (a *Agent) Disrupt(strength float64) {
	for i := 0; i < a.n; i++ {
		for j := 0; j < a.n; j++ {
			val := (1.0-strength)*a.tauMatrix[i][j] + strength*1.0
			a.tauMatrix[i][j] = round4(val)
		}
	}
}

func (a *Agent) SetAntCount(count int) {
	if count < 1 {
		count = 1
	}
	a.antCount = count
}

func (a *Agent) GetAntCount() int {
	return a.antCount
}

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
