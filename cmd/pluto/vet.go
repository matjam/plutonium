package main

import (
	"flag"
	"io"
	"os"
	"strings"

	"github.com/matjam/plutonium/internal/audit"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
	"github.com/spf13/cobra"
)

var vetCmd = &cobra.Command{
	Use:                "vet [-unsafe] [packages]",
	Short:              "List unsafe operations and their safe callers",
	DisableFlagParsing: true,
	Long: `vet -unsafe audits each package and its dependencies without running or
linking them. Packages may be directories, standard library paths or /... patterns.
With no packages it audits the current directory. Unsafe labels identify direct
operations; safe caller paths show possible indirect calls separately.

Findings are a report, so a successful audit exits 0. Source errors exit 1;
invalid arguments exit 2. With no audit flag, vet performs the unsafe audit.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, arg := range args {
			if arg == "-h" || arg == "-help" || arg == "--help" {
				return cmd.Help()
			}
		}
		flags := flag.NewFlagSet("pluto vet", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		flags.Bool("unsafe", false, "audit unsafe code")
		flags.BoolVar(&briefErrors, "brief", briefErrors, briefUsage)
		modeFlag := flags.String("mode", "hosted", "source build mode")
		tagFlag := flags.String("tags", "", "additional build tags")
		if err := flags.Parse(args); err != nil {
			return flagError{err}
		}
		mode := *modeFlag
		if mode != "hosted" && mode != "freestanding" && mode != "kernel" {
			return flagErrorf("unknown mode %q (supported: hosted, freestanding, kernel)", mode)
		}
		var tags []string
		if *tagFlag != "" {
			tags = strings.Split(*tagFlag, ",")
		}
		targets, err := expandTestTargets(flags.Args())
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, target := range targets {
			cfg := &load.Config{Tags: load.BuildTags(mode != "hosted", mode == "kernel", tags), Stdlib: target.stdlib, Sizes: types2.SizesFor("gc", "amd64")}
			if mode == "hosted" {
				cfg.Tags["gencheck"] = true
			}
			prog, err := load.LoadPackage(cfg, target.pkg)
			if err != nil {
				return err
			}
			// A pattern can select a dependency more than once. Report its
			// source only once, while retaining its nodes for call resolution.
			var selected []*load.Package
			for _, p := range prog.Packages {
				if !seen[p.Dir] {
					seen[p.Dir] = true
					selected = append(selected, p)
				}
			}
			if err := audit.WriteSelected(os.Stdout, prog, selected); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	vetCmd.Flags().Bool("unsafe", false, "audit unsafe operations and safe callers")
	vetCmd.Flags().Bool("brief", false, briefUsage)
	vetCmd.Flags().String("mode", "hosted", "source build mode: hosted, freestanding or kernel")
	vetCmd.Flags().StringSlice("tags", nil, "additional build tags")
	rootCmd.AddCommand(vetCmd)
}
