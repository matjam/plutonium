package main

import (
	"flag"
	"io"
	"strings"

	"github.com/matjam/plutonium/internal/doc"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
	"github.com/spf13/cobra"
)

var docCmd = &cobra.Command{
	Use:                "doc [-all] [-tags=tags] [package-or-directory] [symbol[.method]]",
	Short:              "Show package documentation and type ownership kinds",
	Long:               "Show source documentation and declarations without running or linking the package.\nThe package defaults to the current directory. Listings show exported names;\n-all includes private names. An explicit symbol may name a private declaration.\nTypes are labelled value, view, owner (moves), or counted.",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, a := range args {
			if a == "-h" || a == "-help" || a == "--help" {
				return cmd.Help()
			}
		}
		flags := flag.NewFlagSet("pluto doc", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		all := flags.Bool("all", false, "include private names")
		mode := flags.String("mode", "hosted", "source build mode")
		tags := flags.String("tags", "", "additional build tags")
		flags.BoolVar(&briefErrors, "brief", briefErrors, briefUsage)
		if err := flags.Parse(args); err != nil {
			return flagError{err}
		}
		if flags.NArg() > 2 {
			return flagErrorf("expected a package and an optional symbol")
		}
		if *mode != "hosted" && *mode != "freestanding" && *mode != "kernel" {
			return flagErrorf("unknown mode %q (supported: hosted, freestanding, kernel)", *mode)
		}
		target, symbol := ".", ""
		if flags.NArg() > 0 {
			target = flags.Arg(0)
		}
		if flags.NArg() > 1 {
			symbol = flags.Arg(1)
		}
		cfg := &load.Config{Stdlib: stdlib(), Sizes: types2.SizesFor("gc", "amd64"), Tags: load.BuildTags(*mode != "hosted", *mode == "kernel", strings.Split(*tags, ","))}
		if *mode == "hosted" {
			cfg.Tags["gencheck"] = true
		}
		prog, err := load.LoadPackage(cfg, target)
		if err != nil {
			return err
		}
		return doc.Write(cmd.OutOrStdout(), prog, symbol, *all)
	},
}

func init() {
	docCmd.Flags().Bool("all", false, "include private names")
	docCmd.Flags().String("mode", "hosted", "source build mode: hosted, freestanding or kernel")
	docCmd.Flags().String("tags", "", "additional build tags")
	docCmd.Flags().Bool("brief", false, briefUsage)
	rootCmd.AddCommand(docCmd)
}
