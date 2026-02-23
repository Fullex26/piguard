package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Fullex26/piguard/internal/config"
	"github.com/Fullex26/piguard/internal/daemon"
	"github.com/Fullex26/piguard/internal/setup"
	"github.com/Fullex26/piguard/internal/store"
)

var cfgPath string

func main() {
	root := &cobra.Command{
		Use:   "piguard",
		Short: "🛡️ PiGuard — Lightweight host security monitor for Raspberry Pi",
	}

	root.PersistentFlags().StringVar(&cfgPath, "config", config.DefaultConfigPath, "config file path")

	root.AddCommand(
		runCmd(),
		statusCmd(),
		testCmd(),
		setupCmd(),
		versionCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start the PiGuard daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			})))

			d, err := daemon.New(cfg)
			if err != nil {
				return fmt.Errorf("initializing daemon: %w", err)
			}

			return d.Run()
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current security status",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := store.Open(store.DefaultDBPath)
			if err != nil {
				return fmt.Errorf("opening store: %w", err)
			}
			defer db.Close()

			events, err := db.GetRecentEvents(24)
			if err != nil {
				return err
			}

			lastAlert, _ := db.GetLastAlertTime()
			count24h, _ := db.GetEventCount(24)

			fmt.Println("🛡️  PiGuard Status")
			fmt.Println("─────────────────────────")
			fmt.Printf("  Events (24h):  %d\n", count24h)
			fmt.Printf("  Last alert:    %s\n", lastAlert)
			fmt.Println()

			if len(events) > 0 {
				fmt.Println("  Recent events:")
				limit := 10
				if len(events) < limit {
					limit = len(events)
				}
				for _, e := range events[:limit] {
					fmt.Printf("    %s %s %s\n",
						e.Timestamp.Format("15:04"),
						e.Severity.Emoji(),
						e.Message,
					)
				}
			} else {
				fmt.Println("  ✅ No events in last 24 hours")
			}
			return nil
		},
	}
}

func testCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Send a test notification to all configured channels",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			d, err := daemon.New(cfg)
			if err != nil {
				return err
			}

			fmt.Println("🛡️  Sending test notification...")
			if err := d.TestNotifiers(); err != nil {
				return err
			}
			fmt.Println("✅ Test notification sent!")
			return nil
		},
	}
}

func setupCmd() *cobra.Command {
	var envPath string
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive setup wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setup.Run(cfgPath, envPath)
		},
	}
	cmd.Flags().StringVar(&envPath, "env-file", setup.DefaultEnvPath, "path to env file for credentials")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("PiGuard v%s\nhttps://github.com/Fullex26/piguard\n", daemon.Version)
		},
	}
}
