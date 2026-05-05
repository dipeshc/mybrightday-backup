// Command obscure is a one-off development tool for generating and verifying obfuscated
// credential values used by the credential package.
//
// Usage:
//
//	go run ./tools/obscure <plaintext>          # obfuscate a value
//	go run ./tools/obscure -reveal <obfuscated>  # verify roundtrip
package main

import (
	"fmt"
	"os"

	"github.com/dipesh/daycare-photos/internal/credential"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: obscure <plaintext>")
		fmt.Fprintln(os.Stderr, "       obscure -reveal <obfuscated>")
		os.Exit(1)
	}

	if os.Args[1] == "-reveal" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: obscure -reveal <obfuscated>")
			os.Exit(1)
		}
		plaintext, err := credential.Reveal(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "reveal error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(plaintext)
		return
	}

	obfuscated, err := credential.Obscure(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "obscure error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(obfuscated)
}
