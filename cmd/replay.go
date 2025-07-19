package cmd

import (
	"fmt"
	"log"
	"os"

	"mimic/config"
	"mimic/replay"
	"mimic/storage"

	"github.com/spf13/cobra"
)

var (
	replaySessionName        string
	replayTargetHost         string
	replayTargetPort         int
	replayProtocol           string
	replayMatchingStrategy   string
	replayFailFast           bool
	replayTimeoutSeconds     int
	replayMaxConcurrency     int
	replayIgnoreTimestamps   bool
	replayInsecureSkipVerify bool
	replayGRPCMaxMessageSize int
	replayGRPCInsecure       bool
)

var replayCmd = &cobra.Command{
	Use:   "replay",
	Short: "Replay recorded interactions against a target server",
	Long: `Replay recorded interactions from a session against a target server for testing purposes.
This validates that the target server returns the same responses as were originally recorded.`,
	Run: func(cmd *cobra.Command, args []string) {
		runReplay()
	},
}

func init() {
	replayCmd.Flags().StringVar(&replaySessionName, "session", "", "session name to replay (required)")
	replayCmd.Flags().StringVar(&replayTargetHost, "target-host", "", "target server hostname (required)")
	replayCmd.Flags().IntVar(&replayTargetPort, "target-port", 443, "target server port")
	replayCmd.Flags().StringVar(&replayProtocol, "protocol", "https", "target server protocol (http, https, or grpc)")
	replayCmd.Flags().StringVar(&replayMatchingStrategy, "matching-strategy", "exact", "response matching strategy (exact, fuzzy, or status_code)")
	replayCmd.Flags().BoolVar(&replayFailFast, "fail-fast", false, "exit on first mismatch (otherwise collect all errors)")
	replayCmd.Flags().IntVar(&replayTimeoutSeconds, "timeout", 30, "request timeout in seconds")
	replayCmd.Flags().IntVar(&replayMaxConcurrency, "concurrency", 0, "max concurrent requests (0 for sequential)")
	replayCmd.Flags().BoolVar(&replayIgnoreTimestamps, "ignore-timestamps", false, "ignore original timing and fire all requests immediately")
	replayCmd.Flags().BoolVar(&replayInsecureSkipVerify, "insecure-skip-verify", false, "skip TLS verification for HTTPS/gRPC")
	replayCmd.Flags().IntVar(&replayGRPCMaxMessageSize, "grpc-max-message-size", 256*1024*1024, "max gRPC message size in bytes")
	replayCmd.Flags().BoolVar(&replayGRPCInsecure, "grpc-insecure", false, "use insecure gRPC connection (no TLS)")

	replayCmd.MarkFlagRequired("session")
	replayCmd.MarkFlagRequired("target-host")

	rootCmd.AddCommand(replayCmd)
}

func runReplay() {
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	db, err := storage.NewDatabase(cfg.Database.Path)
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	// Build replay config from CLI flags
	replayConfig := &config.ReplayConfig{
		TargetHost:       replayTargetHost,
		TargetPort:       replayTargetPort,
		Protocol:         replayProtocol,
		SessionName:      replaySessionName,
		MatchingStrategy: replayMatchingStrategy,
		FailFast:         replayFailFast,
		TimeoutSeconds:   replayTimeoutSeconds,
		MaxConcurrency:   replayMaxConcurrency,
		IgnoreTimestamps: replayIgnoreTimestamps,
	}

	// Validate the replay config
	if replayConfig.TargetHost == "" {
		log.Fatal("target-host is required")
	}
	if replayConfig.SessionName == "" {
		log.Fatal("session is required")
	}
	if replayConfig.Protocol != "http" && replayConfig.Protocol != "https" && replayConfig.Protocol != "grpc" {
		log.Fatal("protocol must be 'http', 'https', or 'grpc'")
	}
	if replayConfig.MatchingStrategy != "exact" && replayConfig.MatchingStrategy != "fuzzy" && replayConfig.MatchingStrategy != "status_code" {
		log.Fatal("matching-strategy must be 'exact', 'fuzzy', or 'status_code'")
	}

	// Create and run the replay engine
	engine, err := replay.NewReplayEngine(replayConfig, db)
	if err != nil {
		log.Fatal("Failed to create replay engine:", err)
	}

	fmt.Printf("Starting replay of session '%s' against %s://%s:%d\n",
		replayConfig.SessionName, replayConfig.Protocol, replayConfig.TargetHost, replayConfig.TargetPort)

	replaySession, err := engine.Replay()
	if err != nil {
		if replayConfig.FailFast {
			log.Fatal("Replay failed:", err)
		} else {
			log.Printf("Replay completed with errors: %v", err)
		}
	}

	// Print summary
	fmt.Printf("\nReplay Summary:\n")
	fmt.Printf("Session: %s\n", replaySession.SessionName)
	fmt.Printf("Total Requests: %d\n", replaySession.TotalRequests)
	fmt.Printf("Successful: %d\n", replaySession.SuccessCount)
	fmt.Printf("Failed: %d\n", replaySession.FailureCount)
	fmt.Printf("Duration: %v\n", replaySession.Duration)

	// Print detailed results if there were failures
	if replaySession.FailureCount > 0 {
		fmt.Printf("\nFailure Details:\n")
		for i, result := range replaySession.Results {
			if !result.Success {
				fmt.Printf("%d. %s %s\n", i+1, result.Interaction.Method, result.Interaction.Endpoint)
				if result.Error != nil {
					fmt.Printf("   Error: %v\n", result.Error)
				}
				if result.ValidationError != "" {
					fmt.Printf("   Validation: %s\n", result.ValidationError)
				}
				fmt.Printf("   Expected Status: %d, Actual Status: %d\n", result.ExpectedStatus, result.ActualStatus)
				fmt.Printf("   Response Time: %v\n", result.ResponseTime)
				fmt.Printf("\n")
			}
		}
	}

	// Exit with error code if there were failures
	if replaySession.FailureCount > 0 {
		os.Exit(1)
	}
}
