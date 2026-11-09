package main

import (
	"flag"
	"io"
	"log"
	"os"
)

func main() {
	datasetPath := flag.String("dataset", "", "path to dataset JSON (default: embedded labeled set)")
	topK := flag.Int("topk", 0, "hits per query (default: dataset topk)")
	jsonOut := flag.Bool("json", false, "emit the report as JSON")
	flag.Parse()
	if err := run(*datasetPath, *topK, *jsonOut, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// run loads the labeled set, evaluates the local driver and prints the
// baseline report; errors return so main exits non-zero with a clear cause.
func run(datasetPath string, topK int, jsonOut bool, stdout io.Writer) error {
	ds, err := loadDataset(datasetPath)
	if err != nil {
		return err
	}
	label := datasetPath
	if label == "" {
		label = "embedded:dataset.json"
	}
	report, err := evaluate(ds, label, topK)
	if err != nil {
		return err
	}
	if jsonOut {
		return renderJSON(stdout, report)
	}
	renderText(stdout, report)
	return nil
}
