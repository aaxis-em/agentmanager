package core

import (
	"agentmanager/twoopsaoc"
	"context"
	"math"
	"sync"
)

type Config struct {
	Alpha       float64
	Beta        float64
	Evaporation float64
	Q           float64
	AntCount    int
}

type Result struct {
	Route []int
	Dist  float64
}

func NewAgent(distMatrix [][]float64, cfg Config) *twoopsaoc.Agent {
	n := len(distMatrix)

	tau := make([][]float64, n)
	for i := range tau {
		tau[i] = make([]float64, n)
		for j := range tau[i] {
			tau[i][j] = 1.0
		}
	}

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

	return twoopsaoc.NewAgent(distMatrix, tau, n, alpha, beta, evaporation, q, antCount)
}
func RunSwarm(
	ctx context.Context,
	matrix [][]float64,
	sendLocalBest func([]int, float64),
	globalMu *sync.Mutex,
	globalBestDist *float64,
	globalBestRoute *[]int,
) {

	const nAgents = 6
	nNodes := len(matrix)

	agents := initAgents(matrix)
	agentBestDist := [6]float64{math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1)}
	varianceWindow := [6][]float64{}

	bestLocalDistSent := math.Inf(1)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		results, matrices := runAgents(agents)

		updateAgentBests(results, &agentBestDist)

		bestIdx := findBestIndex(results)

		if results[bestIdx].Dist < bestLocalDistSent {
			bestLocalDistSent = results[bestIdx].Dist
			sendLocalBest(results[bestIdx].Route, results[bestIdx].Dist)
		}

		weights := computeWeights(results)
		mergedTau := mergePheromones(matrices, weights, nNodes, results)

		globalMu.Lock()
		curGlobalDist := *globalBestDist
		curGlobalRoute := append([]int(nil), *globalBestRoute...)
		globalMu.Unlock()

		adaptAgents(
			agents,
			results,
			weights,
			&varianceWindow,
			mergedTau,
			agentBestDist,
			curGlobalDist,
			curGlobalRoute,
		)
	}
}
func adaptAgents(
	agents []*twoopsaoc.Agent,
	results [6]twoopsaoc.Result,
	weights [6]float64,
	varianceWindow *[6][]float64,
	mergedTau [][]float64,
	agentBestDist [6]float64,
	globalDist float64,
	globalRoute []int,
) {

	for i := 0; i < len(agents); i++ {

		// update window
		varianceWindow[i] = append(varianceWindow[i], results[i].Dist)
		if len(varianceWindow[i]) > 5 {
			varianceWindow[i] = varianceWindow[i][1:]
		}

		variance := computeVariance(varianceWindow[i])
		isStagnant := len(varianceWindow[i]) == 5 && variance < 1e-6

		if isStagnant {
			agents[i].Disrupt(0.40)
			agents[i].SetAntCount(3)
		} else if weights[i] > 0.4 {
			agents[i].SetAntCount(8)
		} else {
			agents[i].SetAntCount(5)
		}

		agents[i].BlendPheromones(mergedTau, 0.20)

		if len(globalRoute) > 0 && agentBestDist[i] > globalDist {
			strength := math.Min(agentBestDist[i]/globalDist, 3.0)
			agents[i].InjectBestRoute(globalRoute, globalDist, strength)
		}
	}
}
func computeVariance(window []float64) float64 {
	if len(window) < 5 {
		return math.Inf(1)
	}

	mean := 0.0
	for _, v := range window {
		mean += v
	}
	mean /= float64(len(window))

	var variance float64
	for _, v := range window {
		diff := v - mean
		variance += diff * diff
	}
	return variance / float64(len(window))
}
func mergePheromones(
	matrices [6][][]float64,
	weights [6]float64,
	nNodes int,
	results [6]twoopsaoc.Result,
) [][]float64 {

	merged := make([][]float64, nNodes)
	for i := 0; i < nNodes; i++ {
		merged[i] = make([]float64, nNodes)
		for j := 0; j < nNodes; j++ {
			v := 0.0
			for a := 0; a < 6; a++ {
				v += weights[a] * matrices[a][i][j]
			}
			merged[i][j] = v
		}
	}

	// edge reinforcement
	type Edge struct{ U, V int }
	edgeCounts := make(map[Edge]int)

	for a := 0; a < 6; a++ {
		route := results[a].Route
		for k := 0; k < len(route); k++ {
			u, v := route[k], route[(k+1)%len(route)]
			if u > v {
				u, v = v, u
			}
			edgeCounts[Edge{u, v}]++
		}
	}

	for edge, count := range edgeCounts {
		if count >= 2 {
			merged[edge.U][edge.V] += 1.0
			merged[edge.V][edge.U] += 1.0
		}
	}

	return merged
}
func computeWeights(results [6]twoopsaoc.Result) [6]float64 {
	var weights [6]float64
	sum := 0.0

	for i := range results {
		weights[i] = 1.0 / results[i].Dist
		sum += weights[i]
	}
	for i := range weights {
		weights[i] /= sum
	}
	return weights
}
func findBestIndex(results [6]twoopsaoc.Result) int {
	best := 0
	for i := 1; i < len(results); i++ {
		if results[i].Dist < results[best].Dist {
			best = i
		}
	}
	return best
}
func updateAgentBests(results [6]twoopsaoc.Result, agentBestDist *[6]float64) {
	for i := range results {
		if results[i].Dist < agentBestDist[i] {
			agentBestDist[i] = results[i].Dist
		}
	}
}
func runAgents(agents []*twoopsaoc.Agent) ([6]twoopsaoc.Result, [6][][]float64) {
	var wg sync.WaitGroup
	results := [6]twoopsaoc.Result{}
	matrices := [6][][]float64{}

	for i := 0; i < len(agents); i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			results[id] = agents[id].Run(10)
			matrices[id] = agents[id].GetTauMatrix()
		}(i)
	}
	wg.Wait()

	return results, matrices
}
func initAgents(matrix [][]float64) []*twoopsaoc.Agent {
	return []*twoopsaoc.Agent{
		NewAgent(matrix, Config{Alpha: 1.0, Beta: 5.0, Evaporation: 0.5, AntCount: 2}), // Explorer
		NewAgent(matrix, Config{Alpha: 3.0, Beta: 2.0, Evaporation: 0.1, AntCount: 5}), // Exploiter
		NewAgent(matrix, Config{Alpha: 1.0, Beta: 5.0, Evaporation: 0.5, AntCount: 5}), // Diversifier
		NewAgent(matrix, Config{Alpha: 0.3, Beta: 7.0, Evaporation: 0.3, AntCount: 5}), // Explorer
		NewAgent(matrix, Config{Alpha: 2.0, Beta: 3.0, Evaporation: 0.2, AntCount: 5}), // Exploiter
		NewAgent(matrix, Config{Alpha: 1.0, Beta: 9.0, Evaporation: 0.5, AntCount: 5}), // Diversifier
	}
}
