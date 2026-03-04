// Package cmd provides the CLI for stellar-drive via Cobra subcommands.
package cmd

import "github.com/spf13/cobra"

// cfgFile is the path to the YAML configuration file. It is set by the
// persistent --config flag and consumed by all subcommands.
var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "stellar",
	Short: "Stellar-Drive: JSON Schema-driven backend framework",
	Long: `Stellar-Drive turns a JSON Schema into a fully-featured CRUD REST API
backed by versioned, append-only MongoDB persistence.

Drop a *.schema.json file in your schemas/ directory, run "stellar run", and
your API is live. No code generation required.`,
}

// Execute runs the root command and returns any error. Call this from main.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(
		&cfgFile,
		"config",
		"stellar.yaml",
		"config file path (default: stellar.yaml in the current directory)",
	)
}
