package conn

import (
	"agentmanager/core"
	"context"
	"github.com/gorilla/websocket"
	"log"
	"math"
	"net/http"
	"sync"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type IncomingMessage struct {
	Type     string      `json:"type"`
	Matrix   [][]float64 `json:"matrix,omitempty"`
	Path     []int       `json:"path,omitempty"`
	Distance float64     `json:"distance,omitempty"`
}

type OutgoingMessage struct {
	Type     string  `json:"type"`
	Path     []int   `json:"path"`
	Distance float64 `json:"distance"`
}

func HandleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	defer conn.Close()
	log.Println("Main server connected")

	var connMu sync.Mutex // guards writes to conn
	var cancelFunc context.CancelFunc

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

		case "start":
			if len(msg.Matrix) == 0 {
				continue
			}
			log.Printf("Received matrix (%d nodes). Starting swarm…\n", len(msg.Matrix))

			if cancelFunc != nil {
				cancelFunc()
			}

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

			go core.RunSwarm(ctx, msg.Matrix, sendLocalBest, &globalMu, &globalBestDist, &globalBestRoute)

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
