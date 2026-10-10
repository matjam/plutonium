package main

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matjam/plutonium/internal/syntax/scanner"
)

var rootCmd = &cobra.Command{
	Use:   "pluto",
	Short: "The Plutonium compiler",
	Long:  "pluto is the compiler for the Plutonium kernel-safe systems language.",

	// execute prints errors and usage itself: cobra would print an error
	// with its usage text whatever the error, and main would print it again.
	SilenceErrors: true,
	SilenceUsage:  true,

	// Cobra calls this once the flags and arguments have been accepted, so
	// an error after it comes from the command's work, not from its usage.
	PersistentPreRun: func(*cobra.Command, []string) { commandRan = true },
}

// commandRan records that cobra accepted the command line and ran the
// command.
var commandRan bool

func main() {
	os.Exit(execute(os.Args[1:], os.Stderr))
}

// execute runs pluto with the command-line arguments args and returns the
// exit status. A flag or argument cobra rejects prints the error and the
// command's usage, and a flag value the build rejects (a flagError) prints
// the error with the command's name; both exit with status 2. Any other
// error, such as a compile error, prints the error alone and exits with
// status 1.
func execute(args []string, stderr io.Writer) int {
	commandRan = false
	rootCmd.SetArgs(args)
	rootCmd.SetErr(stderr)
	cmd, err := rootCmd.ExecuteC()
	if err == nil {
		return 0
	}
	var ferr flagError
	switch {
	case !commandRan:
		say(stderr, "%s: %v\n%s", cmd.CommandPath(), err, cmd.UsageString())
		return 2
	case errors.As(err, &ferr):
		say(stderr, "%s: %v\n", cmd.CommandPath(), ferr.error)
		return 2
	}
	printError(stderr, err)
	return 1
}

// printError prints err one message to a line. The errors of a compilation
// come joined (errors.Join) or as a scanner.ErrorList, whose own Error
// method shows only the first; printError prints them all.
func printError(w io.Writer, err error) {
	switch err := err.(type) {
	case scanner.ErrorList:
		for _, e := range err {
			say(w, "%s\n", e)
		}
	case interface{ Unwrap() []error }:
		for _, e := range err.Unwrap() {
			printError(w, e)
		}
	default:
		say(w, "%s\n", strings.TrimRight(err.Error(), "\n"))
	}
}
