# main.go Guide

This file is the entrypoint for the AgentManager server. It does not solve the Traveling Salesman Problem directly. Instead, it accepts a distance matrix over WebSocket, starts worker goroutines, and streams better TSP routes back to the client as they are found.

## Big Picture

The application has two layers:

1. `main.go` handles HTTP/WebSocket traffic and coordinates workers.
2. `core/agent.go` contains the actual TSP solving logic based on an Ant Colony Optimization style algorithm.

Think of `main.go` as the traffic controller and `core/agent.go` as the solver.

## Startup Flow

When the program starts, `main()` does two things:

1. Registers an HTTP handler for `/`.
2. Registers a WebSocket handler for `/ws`.

Then it starts an HTTP server on port `8080`.

### `main()`

```go
func main() {
	http.HandleFunc("/", homePage)
	http.HandleFunc("/ws", handleConnections)

	fmt.Println("AgentManager started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		panic("Error starting server: " + err.Error())
	}
}
```

What this means:

- `/` returns a simple text response so the service can be checked in a browser.
- `/ws` is where the actual TSP messages are exchanged.
- `ListenAndServe` blocks forever unless the server fails.

## WebSocket Setup

`upgrader` is configured to allow connections from any origin:

```go
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}
```

This makes local development easy, but it is permissive. In production, you would usually restrict origins.

## Message Types

The server expects and sends JSON messages.

### IncomingMessage

```go
type IncomingMessage struct {
	Type   string      `json:"type"`
	Matrix [][]float64 `json:"matrix"`
}
```

Expected input:

```json
{
  "type": "start",
  "matrix": [[0, 10], [10, 0]]
}
```

Fields:

- `Type` tells the server what the message means.
- `Matrix` is the distance matrix for the TSP instance.

### OutgoingMessage

```go
type OutgoingMessage struct {
	Type     string  `json:"type"`
	Path     []int   `json:"path"`
	Distance float64 `json:"distance"`
}
```

This is what the server sends back when it discovers a better route.

Example:

```json
{
  "type": "improvement",
  "path": [0, 3, 2, 1, 4],
  "distance": 1196.726
}
```

## ManagerState

```go
type ManagerState struct {
	mu           sync.Mutex
	bestDistance float64
	bestPath     []int
}
```

This structure stores the best route found so far for the current client session.

Why it exists:

- Multiple goroutines may find routes at the same time.
- `mu` prevents race conditions when checking or updating the best result.
- `bestDistance` tracks the current global best.
- `bestPath` stores the matching route.

## Home Page Handler

```go
func homePage(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Distributed TSP AgentManager")
}
```

This is just a health-style response. It does not run the solver.

## WebSocket Handler

`handleConnections` is the core orchestration function.

### 1. Upgrade HTTP to WebSocket

The handler starts by upgrading the connection:

```go
conn, err := upgrader.Upgrade(w, r, nil)
```

If the upgrade fails, the request ends.

### 2. Read messages in a loop

The server then keeps reading JSON messages from the client:

```go
err := conn.ReadJSON(&msg)
```

If the connection closes or malformed JSON arrives, the loop ends.

### 3. React to `start`

When the message type is `start` and a matrix is present, the server:

- logs the matrix size
- cancels any previously running workers
- creates a new cancellable context
- resets the best-known route for this run
- starts worker goroutines

## Cancellation Flow

`cancelFunc` lets the server stop earlier workers when a new `start` message arrives.

```go
if cancelFunc != nil {
	cancelFunc()
}

ctx, cancelFunc = context.WithCancel(context.Background())
```

This is important because a new TSP run should not mix results with an older one.

## Best-Route Tracking

Inside `handleConnections`, the closure `reportImprovement` handles a newly discovered route.

```go
reportImprovement := func(path []int, distance float64) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if distance < state.bestDistance {
		state.bestDistance = distance
		state.bestPath = append([]int(nil), path...)

		connMu.Lock()
		err := conn.WriteJSON(OutgoingMessage{
			Type:     "improvement",
			Path:     state.bestPath,
			Distance: state.bestDistance,
		})
		connMu.Unlock()
		...
	}
}
```

What it does:

1. Locks the state so only one goroutine can update it at a time.
2. Compares the incoming route against the current best.
3. Copies the path before storing it, so later changes do not mutate the saved best route.
4. Sends the new best route to the client over WebSocket.

`connMu` exists because WebSocket writes must also be serialized.

## Worker Goroutines

The server starts 3 workers:

```go
for i := 0; i < 3; i++ {
	go func() {
		agent := core.NewAgent(msg.Matrix, core.Config{})
		for {
			select {
			case <-ctx.Done():
				return
			default:
				res := agent.Run(10)
				reportImprovement(res.Route, res.Dist)
			}
		}
	}()
}
```

Important details:

- Each goroutine creates its own `core.Agent`.
- Each worker repeatedly runs the ACO solver.
- If the context is cancelled, the worker exits.
- Every finished run is compared against the best route seen so far.

### Important note

The worker loop is infinite until cancelled. That means `agent.Run(10)` is not called once. It is called repeatedly to keep searching for better routes.

## How This Uses `core/agent.go`

`main.go` delegates actual route search to the ACO solver:

1. `core.NewAgent(msg.Matrix, core.Config{})` creates an ACO solver for the matrix.
2. `agent.Run(10)` runs the solver for 10 iterations.
3. The result contains:
   - `Route`: the best tour found
   - `Dist`: the route length

The solver logic in `core/agent.go` works like this:

- initialize pheromones
- evaporate pheromones each iteration
- let ants construct routes using pheromone + distance heuristics
- deposit pheromones on good routes
- keep the best route found during the run

So `main.go` is the orchestrator, and `core/agent.go` is the algorithm.

## Full Request Lifecycle

1. Client connects to `/ws`.
2. Client sends `{ "type": "start", "matrix": ... }`.
3. Server creates a fresh context and state.
4. Three worker goroutines start solving the TSP.
5. Each worker runs the ACO solver repeatedly.
6. When a worker finds a better route, the server sends it back immediately.
7. If a new start message arrives, the old workers are cancelled.

## Practical Caveats

- The WebSocket origin check is open to all origins.
- The current design is demo-friendly, not production-hardened.
- There is no explicit input validation for matrix shape or symmetry.
- The solver assumes the matrix contains valid finite distances.
- The server can keep running forever until the process is stopped.

## In One Sentence

`main.go` is a WebSocket-based TSP job manager: it accepts a distance matrix, launches repeated ACO searches from `core/agent.go`, and streams improving routes back to the client.