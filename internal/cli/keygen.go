package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/xmdemt/xdev/internal/signer"
)

func newKeygenCmd() *cobra.Command {
	var pubKeyPath, privKeyPath string

	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate Ed25519 key pair for manifest signing",
		Long: `Generate an Ed25519 key pair for signing tool manifests.

The public key should be embedded in the binary (internal/signer/verify.go)
and the private key should be kept secure for release signing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if pubKeyPath == "" || privKeyPath == "" {
				return fmt.Errorf("both --pub-key and --priv-key are required")
			}

			// Ensure directories exist
			if dir := filepath.Dir(pubKeyPath); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("create pub key dir: %w", err)
				}
			}
			if dir := filepath.Dir(privKeyPath); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("create priv key dir: %w", err)
				}
			}

			if err := signer.GenerateKeyPair(pubKeyPath, privKeyPath); err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "✅ Key pair generated:\n")
			fmt.Fprintf(os.Stdout, "   Public key:  %s\n", pubKeyPath)
			fmt.Fprintf(os.Stdout, "   Private key: %s\n", privKeyPath)
			fmt.Fprintf(os.Stdout, "\n⚠️  Keep the private key secure!\n")
			fmt.Fprintf(os.Stdout, "   Copy the public key content to internal/signer/verify.go\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&pubKeyPath, "pub-key", "", "path to write the public key")
	cmd.Flags().StringVar(&privKeyPath, "priv-key", "", "path to write the private key")
	_ = cmd.MarkFlagRequired("pub-key")
	_ = cmd.MarkFlagRequired("priv-key")

	return cmd
}
