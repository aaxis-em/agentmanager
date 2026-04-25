package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"agentmanager/core"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type IncomingMessage struct {
	Type   string      `json:"type"`
	Matrix [][]float64 `json:"matrix"`
}

type OutgoingMessage struct {
	Type     string  `json:"type"`
	Path     []int   `json:"path"`
	Distance float64 `json:"distance"`
}

type ManagerState struct {
	mu           sync.Mutex
	bestDistance float64
	bestPath     []int
}

func main() {
	http.HandleFunc("/", homePage)
	http.HandleFunc("/ws", handleConnections)

	fmt.Println("AgentManager started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		panic("Error starting server: " + err.Error())
	}
}

func homePage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Distributed TSP AgentManager")
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Upgrade error:", err)
		return
	}
	defer conn.Close()

	log.Println("Main Server Connected!")

	var cancelFunc context.CancelFunc

	for {
		var msg IncomingMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Println("Read error:", err)
			break
		}

		if msg.Type == "start" && len(msg.Matrix) > 0 {
			log.Printf("Received Matrix size %d. Spawning agents...\n", len(msg.Matrix))
			
			// Cancel existing agents if any
			if cancelFunc != nil {
				cancelFunc()
			}

			var ctx context.Context
			ctx, cancelFunc = context.WithCancel(context.Background())

			state := &ManagerState{
				bestDistance: 1e18, // infinity
				bestPath:     nil,
			}

			var connMu sync.Mutex
			
			reportImprovement := func(path []int, distance float64) {
				state.mu.Lock()
				defer state.mu.Unlock()

				// Record and send if strictly better than the global best
				if distance < state.bestDistance {
					state.bestDistance = distance
					state.bestPath = core.CloneTour(path)
					
					// Safely write out
					connMu.Lock()
					err := conn.WriteJSON(OutgoingMessage{
						Type:     "improvement",
						Path:     state.bestPath,
						Distance: state.bestDistance,
					})
					connMu.Unlock()
					if err != nil {
						log.Println("Write error back over ws:", err)
					}
				}
			}

			for i := 0; i < 3; i++ {   // Spawn 3 agents
				go core.RunAgent(ctx, i, msg.Matrix, reportImprovement)
			}
		}
	}

	if cancelFunc != nil {
		cancelFunc()
	}
}
