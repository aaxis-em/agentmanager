package main

import (
	"agentmanager/core"
	"fmt"
)

func main() {
	// 1. Distance matrix (mirrors test2.js)
	distMatrix := [][]float64{
		{0, 212.132, 212.132, 206.155, 180.278},
		{212.132, 0, 300, 403.113, 304.138},
		{212.132, 300, 0, 206.155, 390.512},
		{206.155, 403.113, 206.155, 0, 316.228},
		{180.278, 304.138, 390.512, 316.228, 0},
	}

	// 2. Initialise the Agent (all defaults: 5 ants, alpha=1, beta=5, etc.)
	myAgent := core.NewAgent(distMatrix, core.Config{})

	// 3. Run for 10 iterations
	bestResult := myAgent.Run(10)

	fmt.Println("Best Distance:", bestResult.Dist)
	fmt.Println("Best Route:  ", bestResult.Route)
}
