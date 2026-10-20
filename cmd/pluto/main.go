package main

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/matjam/plutonium/internal/diag"
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
	PersistentPreRun: func(cmd *cobra.Command, _ []string) {
		commandRan = true
		if f := cmd.Flags().Lookup("brief"); f != nil && f.Changed {
			briefErrors, _ = cmd.Flags().GetBool("brief")
		}
	},
}

// commandRan records that cobra accepted the command line and ran the
// command.
var commandRan bool

// briefErrors, set by --brief or $PLUTO_BRIEF, prints compile errors
// without their explanations: one line each, as file:line:col: message.
var briefErrors bool

// briefUsage describes the --brief flag of build, run and test.
const briefUsage = "print each error on one line, without its explanation (default from $PLUTO_BRIEF)"

// envBrief reports whether $PLUTO_BRIEF asks for brief errors: set, and
// not 0 or false.
func envBrief() bool {
	switch strings.ToLower(os.Getenv("PLUTO_BRIEF")) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

func main() {
	stop := startProfile()
	code := execute(os.Args[1:], os.Stderr)
	stop()
	os.Exit(code)
}

// execute runs pluto with the command-line arguments args and returns the
// exit status. A flag or argument cobra rejects prints the error and the
// command's usage, and a flag value the build rejects (a flagError) prints
// the error with the command's name; both exit with status 2. Any other
// error, such as a compile error, prints the error alone and exits with
// status 1.
func execute(args []string, stderr io.Writer) int {
	commandRan = false
	briefErrors = envBrief()
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

// printError prints every error in err. The errors of a compilation come
// as a diag.List (or joined, or as a scanner.ErrorList, whose own Error
// method shows only the first); each is printed as file:line:col: message
// followed by its explanation, or alone with --brief.
func printError(w io.Writer, err error) {
	diag.Print(w, err, briefErrors)
}
