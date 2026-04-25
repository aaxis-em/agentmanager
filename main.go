package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sync"

	"agentmanager/core"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// WebSocket upgrader (accepts all origins for dev/distributed deployment)
// ---------------------------------------------------------------------------

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ---------------------------------------------------------------------------
// Message types
// ---------------------------------------------------------------------------

// IncomingMessage is received from the external main server.
type IncomingMessage struct {
	Type     string      `json:"type"`
	Matrix   [][]float64 `json:"matrix,omitempty"`
	Path     []int       `json:"path,omitempty"`
	Distance float64     `json:"distance,omitempty"`
}

// OutgoingMessage is sent back to the external main server.
type OutgoingMessage struct {
	Type     string  `json:"type"`
	Path     []int   `json:"path"`
	Distance float64 `json:"distance"`
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func main() {
	http.HandleFunc("/", homePage)
	http.HandleFunc("/ws", handleConnection)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("AgentManager listening on :%s\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func homePage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Distributed TSP AgentManager")
}

// ---------------------------------------------------------------------------
// handleConnection manages the lifecycle of a single main-server session.
//
// Protocol with the external main server:
//   main server -> this server:  {"type":"start",       "matrix":[[...],...]}
//   main server -> this server:  {"type":"global_best",  "path":[...], "distance":...}
//   this server -> main server:  {"type":"local_best",   "path":[...], "distance":...}
// ---------------------------------------------------------------------------

func handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	defer conn.Close()
	log.Println("Main server connected")

	var connMu sync.Mutex // guards writes to conn
	var cancelFunc context.CancelFunc

	// Global best received from the main server, shared with the swarm goroutine.
	var globalMu sync.Mutex
	globalBestDist := math.Inf(1)
	var globalBestRoute []int

	for {
		var msg IncomingMessage
		if err := conn.ReadJSON(&msg); err != nil {
			log.Println("Read error:", err)
			break
		}

		switch msg.Type {

		// ---------------------------------------------------------------
		// START — main server sends a distance matrix, we spawn the swarm.
		// ---------------------------------------------------------------
		case "start":
			if len(msg.Matrix) == 0 {
				continue
			}
			log.Printf("Received matrix (%d nodes). Starting swarm…\n", len(msg.Matrix))

			// Tear down any previous swarm.
			if cancelFunc != nil {
				cancelFunc()
			}

			// Reset global best for the new problem instance.
			globalMu.Lock()
			globalBestDist = math.Inf(1)
			globalBestRoute = nil
			globalMu.Unlock()
			ctx, cancel := context.WithCancel(context.Background())
			cancelFunc = cancel

			sendLocalBest := func(path []int, dist float64) {
				connMu.Lock()
				defer connMu.Unlock()
				if err := conn.WriteJSON(OutgoingMessage{
					Type:     "local_best",
					Path:     path,
					Distance: dist,
				}); err != nil {
					log.Println("Write error:", err)
				}
			}

			go runSwarm(ctx, msg.Matrix, sendLocalBest, &globalMu, &globalBestDist, &globalBestRoute)

		// ---------------------------------------------------------------
		// GLOBAL_BEST — main server broadcasts the best route it knows.
		// ---------------------------------------------------------------
		case "global_best":
			if len(msg.Path) == 0 {
				continue
			}
			globalMu.Lock()
			globalBestDist = msg.Distance
			globalBestRoute = append([]int(nil), msg.Path...)
			globalMu.Unlock()
			log.Printf("Received global best: %.4f\n", msg.Distance)
		}
	}

	if cancelFunc != nil {
		cancelFunc()
	}
}

// ---------------------------------------------------------------------------
// runSwarm orchestrates the three specialised agents.
//
// Each epoch:
//  1. All agents run 10 ACO iterations in parallel.
//  2. Local best among the three is sent to the main server.
//  3. Agent pheromone matrices are merged locally (stigmergic layer).
//  4. Elite-edge consensus reinforcement is applied to the merged matrix.
//  5. Convergence detection triggers disruption for stagnant agents.
//  6. Adaptive ant count adjusts compute budget per agent.
//  7. If a global best exists and is better than an agent's own best,
//     that agent receives pheromone injection along the global route.
//     Agents that are already better keep their matrix untouched.
// ---------------------------------------------------------------------------

func runSwarm(
	ctx context.Context,
	matrix [][]float64,
	sendLocalBest func([]int, float64),
	globalMu *sync.Mutex,
	globalBestDist *float64,
	globalBestRoute *[]int,
) {
	const nAgents = 3
	nNodes := len(matrix)

	// --- Agent specialisation ---
	agents := [nAgents]*core.Agent{
		core.NewAgent(matrix, core.Config{Alpha: 0.5, Beta: 8.0, Evaporation: 0.1, AntCount: 5}), // Explorer
		core.NewAgent(matrix, core.Config{Alpha: 3.0, Beta: 2.0, Evaporation: 0.1, AntCount: 5}), // Exploiter
		core.NewAgent(matrix, core.Config{Alpha: 1.0, Beta: 5.0, Evaporation: 0.5, AntCount: 5}), // Diversifier
	}

	// Per-agent tracking.
	agentBestDist := [nAgents]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	varianceWindow := [nAgents][]float64{}

	type Edge struct{ U, V int }
	
	bestLocalDistSent := math.Inf(1)

	// ---- Main epoch loop ----
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 1. Run all agents in parallel.
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

		// Track each agent's personal best.
		for i := 0; i < nAgents; i++ {
			if results[i].Dist < agentBestDist[i] {
				agentBestDist[i] = results[i].Dist
			}
		}

		// 2. Find local best among the three and send to main server.
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

		// 3. Stigmergic pheromone merge (weighted by inverse distance).
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

		// 4. Elite-edge consensus reinforcement.
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

		// Snapshot the current global best (lock-free read).
		globalMu.Lock()
		curGlobalDist := *globalBestDist
		curGlobalRoute := append([]int(nil), *globalBestRoute...)
		globalMu.Unlock()

		// 5–7. Per-agent post-processing.
		for i := 0; i < nAgents; i++ {

			// --- Convergence detection (sliding variance window) ---
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

			// --- Disruption + adaptive ant count ---
			if isStagnant {
				agents[i].Disrupt(0.40)
				agents[i].SetAntCount(3)
			} else if weights[i] > 0.4 {
				agents[i].SetAntCount(8)
			} else {
				agents[i].SetAntCount(5)
			}

			// --- Local stigmergic blend (20%) ---
			agents[i].BlendPheromones(mergedTau, 0.20)

			// --- Global best injection (conditional) ---
			// Only inject if a global best exists AND this agent's own best
			// is worse than the global optimum. Agents that are already
			// ahead keep their pheromone matrix untouched so they can
			// continue exploiting their superior local knowledge.
			if len(curGlobalRoute) > 0 && agentBestDist[i] > curGlobalDist {
				// Strength proportional to how far behind the agent is,
				// capped at 3× to prevent overwhelming its local memory.
				strength := math.Min(agentBestDist[i]/curGlobalDist, 3.0)
				agents[i].InjectBestRoute(curGlobalRoute, curGlobalDist, strength)
			}
		}
	}
}
