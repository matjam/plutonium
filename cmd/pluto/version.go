package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

const version = "0.1.0-dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the pluto compiler version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("pluto version %s\n", version)
		fmt.Printf("target: x86_64-linux\n")
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
