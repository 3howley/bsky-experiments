package main

import (
	"fmt"
	"log"
	"os"

	"github.com/ericvolp12/bsky-experiments/pkg/graph"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Println("Usage: go run main.go inputfile1 inputfile2 outputfile")
		return
	}

	inputFile1 := os.Args[1]
	inputFile2 := os.Args[2]
	outputFile := os.Args[3]

	binReaderWriter := graph.BinaryGraphReaderWriter{}

	// Read the graph from the Binary file
	g1, err := binReaderWriter.ReadGraph(inputFile1)
	if err != nil {
		log.Fatalf("Error reading graph1 from binary file: %v", err)
	}

	// Read graph 2 from the Binary file
	g2, err := binReaderWriter.ReadGraph(inputFile2)
	if err != nil {
		log.Fatalf("Error reading graph2 from binary file: %v", err)
	}

	// Read graph 3 from the Binary file we want to update
	g3, err := binReaderWriter.ReadGraph(outputFile)
	if err != nil {
		log.Fatalf("Error reading output graph from binary file: %v", err)
	}

	// Diff the graphs
	diff := graph.Diff(&g2, &g1)
	g3.ApplyDiff(diff)

	// Write the graph to the new Binary database
	err = binReaderWriter.WriteGraph(g3, outputFile)
	if err != nil {
		log.Fatalf("Error writing graph to the output binary file: %v", err)
	}

	fmt.Println("Graphs successfully diffed and merged to binary file")
}
