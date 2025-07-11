package cmd

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"mimic/config"
	"mimic/export"
	"mimic/mock"
	"mimic/proxy"
	"mimic/storage"

	"github.com/spf13/cobra"
)

var (
	cfgFile        string
	mode           string
	sessionName    string
	outputFile     string
	inputFile      string
	mergeStrategy  string
)

var rootCmd = &cobra.Command{
	Use:   "mimic",
	Short: "API Mimic - Record and replay API interactions",
	Long: `A transparent proxy for intercepting, recording, and mocking API calls.
Supports both REST and gRPC protocols with SQLite storage and JSON export/import.`,
	Run: func(cmd *cobra.Command, args []string) {
		runProxy()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is config.yaml)")
	rootCmd.Flags().StringVar(&mode, "mode", "", "operation mode: record or mock")
	
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(importCmd)
	rootCmd.AddCommand(listSessionsCmd)
	rootCmd.AddCommand(clearCmd)
}

func runProxy() {
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	if mode != "" {
		cfg.Proxy.Mode = mode
	}

	if err := cfg.Validate(); err != nil {
		log.Fatal("Invalid configuration:", err)
	}

	db, err := storage.NewDatabase(cfg.Database.Path)
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	var server interface{ Start() error }
	
	switch cfg.Proxy.Mode {
	case "record":
		server, err = proxy.NewProxyEngine(cfg, db)
		if err != nil {
			log.Fatal("Failed to create proxy engine:", err)
		}
	case "mock":
		server, err = mock.NewMockEngine(cfg, db)
		if err != nil {
			log.Fatal("Failed to create mock engine:", err)
		}
	default:
		log.Fatalf("Invalid mode: %s (must be 'record' or 'mock')", cfg.Proxy.Mode)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	if err := server.Start(); err != nil {
		log.Fatal("Server failed:", err)
	}
}

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export session data to JSON",
	Long:  `Export recorded session data to JSON format for backup or CI/CD integration.`,
	Run: func(cmd *cobra.Command, args []string) {
		if sessionName == "" {
			log.Fatal("Session name is required (--session)")
		}
		if outputFile == "" {
			log.Fatal("Output file is required (--output)")
		}

		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			log.Fatal("Failed to load config:", err)
		}

		db, err := storage.NewDatabase(cfg.Database.Path)
		if err != nil {
			log.Fatal("Failed to initialize database:", err)
		}
		defer db.Close()

		exportManager := export.NewExportManager(cfg, db)
		
		if err := exportManager.ExportSession(sessionName, outputFile); err != nil {
			log.Fatal("Failed to export session:", err)
		}

		fmt.Printf("Session '%s' exported to '%s'\n", sessionName, outputFile)
	},
}

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import session data from JSON",
	Long:  `Import session data from JSON format to restore or load test data.`,
	Run: func(cmd *cobra.Command, args []string) {
		if inputFile == "" {
			log.Fatal("Input file is required (--input)")
		}

		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			log.Fatal("Failed to load config:", err)
		}

		db, err := storage.NewDatabase(cfg.Database.Path)
		if err != nil {
			log.Fatal("Failed to initialize database:", err)
		}
		defer db.Close()

		exportManager := export.NewExportManager(cfg, db)
		
		if err := exportManager.ImportSession(inputFile, sessionName, mergeStrategy); err != nil {
			log.Fatal("Failed to import session:", err)
		}

		fmt.Printf("Session imported from '%s'\n", inputFile)
	},
}

var listSessionsCmd = &cobra.Command{
	Use:   "list-sessions",
	Short: "List all recorded sessions",
	Long:  `List all recorded sessions in the database with their metadata.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			log.Fatal("Failed to load config:", err)
		}

		db, err := storage.NewDatabase(cfg.Database.Path)
		if err != nil {
			log.Fatal("Failed to initialize database:", err)
		}
		defer db.Close()

		sessions, err := db.ListSessions()
		if err != nil {
			log.Fatal("Failed to list sessions:", err)
		}

		if len(sessions) == 0 {
			fmt.Println("No sessions found.")
			return
		}

		fmt.Printf("%-20s %-20s %-30s %s\n", "ID", "Name", "Created", "Description")
		fmt.Println(string(make([]byte, 90)))
		for _, session := range sessions {
			fmt.Printf("%-20d %-20s %-30s %s\n", 
				session.ID, 
				session.SessionName, 
				session.CreatedAt.Format("2006-01-02 15:04:05"), 
				session.Description)
		}
	},
}

var clearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear a specific session",
	Long:  `Clear all data for a specific session, removing all recorded interactions.`,
	Run: func(cmd *cobra.Command, args []string) {
		if sessionName == "" {
			log.Fatal("Session name is required (--session)")
		}

		cfg, err := config.LoadConfig(cfgFile)
		if err != nil {
			log.Fatal("Failed to load config:", err)
		}

		db, err := storage.NewDatabase(cfg.Database.Path)
		if err != nil {
			log.Fatal("Failed to initialize database:", err)
		}
		defer db.Close()

		if err := db.ClearSession(sessionName); err != nil {
			log.Fatal("Failed to clear session:", err)
		}

		fmt.Printf("Session '%s' cleared successfully\n", sessionName)
	},
}

func init() {
	exportCmd.Flags().StringVar(&sessionName, "session", "", "session name to export")
	exportCmd.Flags().StringVar(&outputFile, "output", "", "output file path")
	exportCmd.MarkFlagRequired("session")
	exportCmd.MarkFlagRequired("output")

	importCmd.Flags().StringVar(&inputFile, "input", "", "input file path")
	importCmd.Flags().StringVar(&sessionName, "session", "", "target session name (optional)")
	importCmd.Flags().StringVar(&mergeStrategy, "merge-strategy", "append", "merge strategy: append or replace")
	importCmd.MarkFlagRequired("input")

	clearCmd.Flags().StringVar(&sessionName, "session", "", "session name to clear")
	clearCmd.MarkFlagRequired("session")
}