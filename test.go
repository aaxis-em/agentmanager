//go:build ignore
// +build ignore

package main

import (
	"agentmanager/core"
	"fmt"
	"math"
	"sync"
	"time"
)

// This script simulates the main loop inside runSwarm but without the websocket connection.
// It runs the agents, merges the pheromones, applies disruption and prints detailed logs
// of everything so you can see the swarm intelligence in action.

func main() {
	fmt.Println("=========================================================")
	fmt.Println("    Standalone Swarm Intelligence ACO Simulation         ")
	fmt.Println("=========================================================")

	// Distance matrix (mirrors test2.js)
	matrix := [][]float64{
		{0, 212.132, 212.132, 206.155, 180.278},
		{212.132, 0, 300, 403.113, 304.138},
		{212.132, 300, 0, 206.155, 390.512},
		{206.155, 403.113, 206.155, 0, 316.228},
		{180.278, 304.138, 390.512, 316.228, 0},
	}
	nNodes := len(matrix)

	const nAgents = 3
	agentNames := []string{"Explorer", "Exploiter", "Diversifier"}

	// --- 1. Agent specialisation ---
	agents := [nAgents]*core.Agent{
		core.NewAgent(matrix, core.Config{Alpha: 0.5, Beta: 8.0, Evaporation: 0.1, AntCount: 5}), // Explorer
		core.NewAgent(matrix, core.Config{Alpha: 3.0, Beta: 2.0, Evaporation: 0.1, AntCount: 5}), // Exploiter
		core.NewAgent(matrix, core.Config{Alpha: 1.0, Beta: 5.0, Evaporation: 0.5, AntCount: 5}), // Diversifier
	}

	// Per-agent tracking
	agentBestDist := [nAgents]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	varianceWindow := [nAgents][]float64{}
	type Edge struct{ U, V int }

	// Run for a fixed number of epochs to observe behavior
	const maxEpochs = 10

	for epoch := 1; epoch <= maxEpochs; epoch++ {
		fmt.Printf("\n--- [ EPOCH %d ] ----------------------------------------\n", epoch)
		time.Sleep(500 * time.Millisecond) // slow down slightly for readability

		// Run all agents in parallel (10 iterations each)
		var wg sync.WaitGroup
		results := [nAgents]core.Result{}
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

		// Track and log each agent's personal best
		bestIdx := 0
		for i := 0; i < nAgents; i++ {
			if results[i].Dist < agentBestDist[i] {
				agentBestDist[i] = results[i].Dist
			}
			if results[i].Dist < results[bestIdx].Dist {
				bestIdx = i
			}
			fmt.Printf("Agent %d (%-11s) -> Epoch Best: %.3f (Overall Best: %.3f) Route: %v (Ants: %d)\n",
				i+1, agentNames[i], results[i].Dist, agentBestDist[i], results[i].Route, agents[i].GetAntCount())
		}

		fmt.Printf("\n>> LOCAL BEST THIS EPOCH: Agent %d (%s) with distance %.3f\n",
			bestIdx+1, agentNames[bestIdx], results[bestIdx].Dist)

		// --- 2. Stigmergic pheromone merge ---
		weights := [nAgents]float64{}
		sumW := 0.0
		for i := 0; i < nAgents; i++ {
			weights[i] = 1.0 / results[i].Dist
			sumW += weights[i]
		}
		fmt.Printf("\n[Pheromone Merge Weights]\n")
		for i := 0; i < nAgents; i++ {
			weights[i] /= sumW
			fmt.Printf("- %-11s weight: %.2f%%\n", agentNames[i], weights[i]*100)
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

		// --- 3. Elite-edge consensus reinforcement ---
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

		consensusFound := false
		for edge, count := range edgeCounts {
			if count >= 2 {
				if !consensusFound {
					fmt.Printf("\n[Consensus Reinforcement]\n")
					consensusFound = true
				}
				fmt.Printf("- Edge %d-%d found in %d routes. Boosting pheromones!\n", edge.U, edge.V, count)
				mergedTau[edge.U][edge.V] += 1.0
				mergedTau[edge.V][edge.U] += 1.0
			}
		}

		// --- 4. Convergence detection & Disruption ---
		fmt.Printf("\n[Agent Adjustments]\n")
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

			blendRatio := 0.20 // Default local stigmergic blend

			if isStagnant {
				fmt.Printf("- %-11s is STAGNANT (variance %.6f). Disrupting matrix (40%%) and lowering ants to 3.\n", agentNames[i], variance)
				agents[i].Disrupt(0.40)
				agents[i].SetAntCount(3)
				blendRatio = 0.50 // Stronger global injection when stagnant
			} else {
				if weights[i] > 0.4 {
					fmt.Printf("- %-11s is PERFORMING WELL (weight %.2f). Increasing ants to 8.\n", agentNames[i], weights[i])
					agents[i].SetAntCount(8)
				} else {
					fmt.Printf("- %-11s is STABLE. Maintaining 5 ants.\n", agentNames[i])
					agents[i].SetAntCount(5)
				}
			}

			agents[i].BlendPheromones(mergedTau, blendRatio)
		}
	}

	fmt.Println("\n=========================================================")
	fmt.Println("    Simulation Complete                                  ")
	fmt.Println("=========================================================")
}
