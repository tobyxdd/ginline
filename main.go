package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

var (
	outDir    = flag.String("out", "", "Output directory")
	outSuffix = flag.String("suffix", "inlined", "Output file suffix")
)

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage of ginline:\n")
	_, _ = fmt.Fprintf(os.Stderr, "\tginline [flags] directory\n")
	_, _ = fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		log.Fatalln("Please specify one (and only one) directory to be processed")
	}
	if err := inlinePackage(args[0], *outDir, *outSuffix); err != nil {
		log.Fatalf("%s: %s\n", args[0], err.Error())
	}
}
