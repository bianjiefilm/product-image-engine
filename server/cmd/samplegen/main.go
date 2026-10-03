// samplegen writes or checks the frozen synthetic sample set.
//
//	go run ./cmd/samplegen -write ../fixtures/frozen-samples/v1
//	go run ./cmd/samplegen -check ../fixtures/frozen-samples/v1
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bianjiefilm/product-image-engine/server/internal/samplegen"
)

func main() {
	write := flag.String("write", "", "write the set into this directory")
	check := flag.String("check", "", "verify the committed set in this directory")
	flag.Parse()
	if (*write == "") == (*check == "") {
		fmt.Fprintln(os.Stderr, "usage: samplegen -write DIR | -check DIR")
		os.Exit(2)
	}
	if *write != "" {
		if err := samplegen.Write(*write); err != nil {
			fmt.Fprintln(os.Stderr, "samplegen:", err)
			os.Exit(1)
		}
		fmt.Println("samplegen: wrote", *write)
		return
	}
	if err := samplegen.Check(*check); err != nil {
		fmt.Fprintln(os.Stderr, "samplegen: CHECK FAILED:", err)
		os.Exit(1)
	}
	fmt.Println("samplegen: check ok (9 cases)")
}
