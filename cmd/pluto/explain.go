package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matjam/plutonium/internal/diag"
)

var explainCmd = &cobra.Command{
	Use:   "explain [code...]",
	Short: "Explain a compile error code",
	Long: `explain prints the long explanation of a compile error code, with an
example. Every compile error ends with the code to look up, such as
E0024. With no code, explain lists every code and its name.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return explain(cmd.OutOrStdout(), args)
	},
}

func init() {
	rootCmd.AddCommand(explainCmd)
}

// explain writes the explanation of each code in codes, or the list of
// codes if there are none.
func explain(w io.Writer, codes []string) error {
	if len(codes) == 0 {
		for _, c := range diag.Codes() {
			say(w, "%s  %s\n", c, diag.Lookup(c).Name)
		}
		return nil
	}
	for i, c := range codes {
		text, err := diag.Explain(normalizeCode(c))
		if err != nil {
			return err
		}
		if i > 0 {
			say(w, "\n")
		}
		say(w, "%s", text)
	}
	return nil
}

// normalizeCode accepts a code as printed (E0024), in lower case, or as a
// bare number (24).
func normalizeCode(s string) diag.Code {
	s = strings.ToUpper(strings.TrimSpace(s))
	if n, err := strconv.Atoi(strings.TrimPrefix(s, "E")); err == nil && n >= 0 {
		return diag.Code(fmt.Sprintf("E%04d", n))
	}
	return diag.Code(s)
}
