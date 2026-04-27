package agent

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

	agents := [nAgents]*twoopsaoc.Agent{
		NewAgent(matrix, Config{Alpha: 1.0, Beta: 5.0, Evaporation: 0.5, AntCount: 2}), // Explorer
		NewAgent(matrix, Config{Alpha: 3.0, Beta: 2.0, Evaporation: 0.1, AntCount: 5}), // Exploiter
		NewAgent(matrix, Config{Alpha: 1.0, Beta: 5.0, Evaporation: 0.5, AntCount: 5}), // Diversifier
		NewAgent(matrix, Config{Alpha: 0.3, Beta: 7.0, Evaporation: 0.3, AntCount: 5}), // Explorer
		NewAgent(matrix, Config{Alpha: 2.0, Beta: 3.0, Evaporation: 0.2, AntCount: 5}), // Exploiter
		NewAgent(matrix, Config{Alpha: 1.0, Beta: 9.0, Evaporation: 0.5, AntCount: 5}), // Diversifier

	}

	agentBestDist := [nAgents]float64{math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1), math.Inf(1)}
	varianceWindow := [nAgents][]float64{}

	type Edge struct{ U, V int }

	bestLocalDistSent := math.Inf(1)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var wg sync.WaitGroup
		results := [nAgents]twoopsaoc.Result{}
		matrices := [nAgents][][]float64{}

		for i := 0; i < nAgents; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				results[id] = agents[id].Run(10)
				matrices[id] = agents[id].GetTauMatrix()
			}(i)
		}
		wg.Wait()

		for i := 0; i < nAgents; i++ {
			if results[i].Dist < agentBestDist[i] {
				agentBestDist[i] = results[i].Dist
			}
		}

		bestIdx := 0
		for i := 1; i < nAgents; i++ {
			if results[i].Dist < results[bestIdx].Dist {
				bestIdx = i
			}
		}

		if results[bestIdx].Dist < bestLocalDistSent {
			bestLocalDistSent = results[bestIdx].Dist
			sendLocalBest(results[bestIdx].Route, results[bestIdx].Dist)
		}

		weights := [nAgents]float64{}
		sumW := 0.0
		for i := 0; i < nAgents; i++ {
			weights[i] = 1.0 / results[i].Dist
			sumW += weights[i]
		}
		for i := 0; i < nAgents; i++ {
			weights[i] /= sumW
		}

		mergedTau := make([][]float64, nNodes)
		for i := 0; i < nNodes; i++ {
			mergedTau[i] = make([]float64, nNodes)
			for j := 0; j < nNodes; j++ {
				v := 0.0
				for a := 0; a < nAgents; a++ {
					v += weights[a] * matrices[a][i][j]
				}
				mergedTau[i][j] = v
			}
		}

		edgeCounts := make(map[Edge]int)
		for a := 0; a < nAgents; a++ {
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
				mergedTau[edge.U][edge.V] += 1.0
				mergedTau[edge.V][edge.U] += 1.0
			}
		}

		globalMu.Lock()
		curGlobalDist := *globalBestDist
		curGlobalRoute := append([]int(nil), *globalBestRoute...)
		globalMu.Unlock()

		for i := 0; i < nAgents; i++ {

			varianceWindow[i] = append(varianceWindow[i], results[i].Dist)
			if len(varianceWindow[i]) > 5 {
				varianceWindow[i] = varianceWindow[i][1:]
			}

			variance := 0.0
			if len(varianceWindow[i]) == 5 {
				mean := 0.0
				for _, v := range varianceWindow[i] {
					mean += v
				}
				mean /= 5.0
				for _, v := range varianceWindow[i] {
					variance += (v - mean) * (v - mean)
				}
				variance /= 5.0
			}

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

			if len(curGlobalRoute) > 0 && agentBestDist[i] > curGlobalDist {
				strength := math.Min(agentBestDist[i]/curGlobalDist, 3.0)
				agents[i].InjectBestRoute(curGlobalRoute, curGlobalDist, strength)
			}
		}
	}
}
