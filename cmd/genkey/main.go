// Command genkey prints a fresh Ed25519 signing seed as a shell-sourceable
// export line. Dev-only — not included in the production image.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	outPath := flag.String("f", "", "write export line to this file (refuses to overwrite unless -force)")
	force := flag.Bool("force", false, "overwrite -f target if it exists")
	flag.Parse()

	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	privB64 := base64.StdEncoding.EncodeToString(seed)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	fmt.Printf("JWT_PRIVATE_KEY_B64=%s\n", privB64)
	fmt.Printf("# public key (for JWT_PREVIOUS_KEYS later): %s\n", pubB64)

	if *outPath == "" {
		return
	}
	if _, err := os.Stat(*outPath); err == nil && !*force {
		fmt.Fprintf(os.Stderr, "error: %s exists; pass -force to overwrite\n", *outPath)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	content := fmt.Sprintf("export JWT_PRIVATE_KEY_B64=%s\n", privB64)
	if err := os.WriteFile(*outPath, []byte(content), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "# wrote %s (mode 0600)\n", *outPath)
}
