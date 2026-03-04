package cmd

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/digitally-rendered/stellar-drive/pkg/config"
	"github.com/digitally-rendered/stellar-drive/pkg/engine"
)

// runPort is the optional --port flag value. Zero means "use config value".
var runPort int

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the stellar-drive server",
	Long: `Load the configuration file, connect to MongoDB, register all schemas
found in the schemas directory, and start the HTTP API server.

The server blocks until it receives SIGINT or SIGTERM, then performs a
graceful shutdown.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("run: load config: %w", err)
		}

		// --port flag overrides cfg.Server.Port when explicitly provided.
		if cmd.Flags().Changed("port") {
			if runPort <= 0 || runPort > 65535 {
				return fmt.Errorf("run: invalid port %d (must be 1-65535)", runPort)
			}
			cfg.Server.Port = runPort
		}

		slog.Info("starting stellar-drive",
			"config", cfgFile,
			"project", cfg.Project.Name,
			"port", cfg.Server.Port,
		)

		eng := engine.New(cfg)
		return eng.Start(context.Background())
	},
}

func init() {
	runCmd.Flags().IntVar(
		&runPort,
		"port",
		0,
		"HTTP port to listen on (overrides config server.port)",
	)
	rootCmd.AddCommand(runCmd)
}
