package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/lsp"
)

var lspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Run the language server on standard input and output",
	Long: `lsp runs a Language Server Protocol server for editors, speaking JSON-RPC
on standard input and output: diagnostics, hover, go to definition,
references, symbols, formatting, completion, signature help and rename.

The server uses $PLUTOROOT as the standard library if it is set, and
otherwise extracts the library embedded in pluto into the user's cache
directory, so that go to definition can open its files. It logs to
standard error.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		verbose, _ := cmd.Flags().GetBool("verbose")
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		opts := lsp.Options{
			Stdlib:  plutonium.Stdlib(),
			Version: version,
			Log:     slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})),
		}
		if root := os.Getenv("PLUTOROOT"); root != "" {
			if abs, err := filepath.Abs(root); err == nil {
				opts.StdlibDir = abs
			}
		}
		return lsp.Serve(cmd.Context(), os.Stdin, os.Stdout, opts)
	},
}

func init() {
	lspCmd.Flags().BoolP("verbose", "v", false, "log every request and how long it took")
	rootCmd.AddCommand(lspCmd)
}
