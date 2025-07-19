package cmd

import (
	"log"

	"github.com/spf13/cobra"
	"mimic/config"
	"mimic/storage"
	"mimic/web"
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the web UI server",
	Long:  `Start the web UI server to view sessions and live request/response traffic`,
	Run: func(cmd *cobra.Command, args []string) {
		runWebServer()
	},
}

func runWebServer() {
	cfg, err := config.LoadConfig(cfgFile)
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatal("Invalid configuration:", err)
	}

	db, err := storage.NewDatabase(cfg.Database.Path)
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	webServer := web.NewServer(cfg, db)
	if err := webServer.Start(); err != nil {
		log.Fatal("Web server failed:", err)
	}
}
