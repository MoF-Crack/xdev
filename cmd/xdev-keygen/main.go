// xdev-keygen is a standalone utility that generates Ed25519 key pairs
// for manifest signing. The generated keys are used by the signer package
// to verify manifest integrity at download time.
//
// Usage:
//
//	xdev-keygen --pub-key <path> --priv-key <path>
//
// The public key file contains a base64-encoded 32-byte Ed25519 public key.
// The private key file contains a base64-encoded 64-byte Ed25519 private key
// (seed + public key concatenation) and is written with restricted permissions.
//
// After generating, embed the public key in the binary via -ldflags:
//
//	go build -ldflags "-X internal/signer.rootPublicKeyB64=$(cat <pub-key-path>)" ./cmd/xmdevenvmt/
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/xmdemt/xdev/internal/signer"
)

func main() {
	pubPath := flag.String("pub-key", "", "Path to write the base64-encoded Ed25519 public key")
	privPath := flag.String("priv-key", "", "Path to write the base64-encoded Ed25519 private key (restricted permissions)")
	flag.Parse()

	if *pubPath == "" || *privPath == "" {
		fmt.Fprintln(os.Stderr, "Usage: xdev-keygen --pub-key <path> --priv-key <path>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Generate an Ed25519 key pair for manifest signing.")
		fmt.Fprintln(os.Stderr, "After generating, embed the public key via -ldflags:")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  go build -ldflags \"-X internal/signer.rootPublicKeyB64=$(cat <pub-key>)\" ./cmd/xmdevenvmt/")
		os.Exit(1)
	}

	if err := signer.GenerateKeyPair(*pubPath, *privPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "Key pair generated successfully.\n")
	fmt.Fprintf(os.Stdout, "  Public key: %s\n", *pubPath)
	fmt.Fprintf(os.Stdout, "  Private key: %s (keep this secure!)\n", *privPath)
	fmt.Fprintf(os.Stdout, "\nTo embed the public key in the binary:\n")
	fmt.Fprintf(os.Stdout, "  go build -ldflags \"-X internal/signer.rootPublicKeyB64=$(cat %s)\" ./cmd/xmdevenvmt/\n", *pubPath)
}
