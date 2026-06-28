package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"

	"zdll/internal/skillvault"
)

func main() {
	var (
		in     = flag.String("in", "skills", "source skill directory")
		out    = flag.String("out", "skills.vault", "output encrypted bundle")
		dekHex = flag.String("dek", "", "hex-encoded 32-byte AES key; if empty, one is generated and printed")
	)
	flag.Parse()

	var dek []byte
	var err error
	if *dekHex == "" {
		dek, err = skillvault.GenerateDEK()
		if err != nil {
			log.Fatalf("generate dek: %v", err)
		}
		fmt.Fprintf(os.Stdout, "Generated DEK (keep secret): %s\n", hex.EncodeToString(dek))
	} else {
		dek, err = hex.DecodeString(*dekHex)
		if err != nil {
			log.Fatalf("decode dek: %v", err)
		}
	}

	bundle, err := skillvault.BuildBundle(*in, dek)
	if err != nil {
		log.Fatalf("build bundle: %v", err)
	}

	if err := skillvault.WriteBundle(*out, bundle); err != nil {
		log.Fatalf("write bundle: %v", err)
	}

	fmt.Fprintf(os.Stdout, "Encrypted %d file(s) from %s -> %s\n", len(bundle.Files), *in, *out)
}
