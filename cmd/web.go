package cmd

import (
	"log"

	"mimic/config"
	"mimic/server"
	"mimic/storage"
	"mimic/web"

	"github.com/spf13/cobra"
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

	// If proxies are configured, use multi-proxy server
	if len(cfg.Proxies) > 0 {
		log.Printf("Starting multi-proxy server with %d proxy(ies)", len(cfg.Proxies))
		multiProxyServer, err := server.NewMultiProxyServer(cfg, db)
		if err != nil {
			log.Fatal("Failed to create multi-proxy server:", err)
		}
		if err := multiProxyServer.Start(); err != nil {
			log.Fatal("Multi-proxy server failed:", err)
		}
	} else {
		// No proxies configured, just start web UI
		webServer := web.NewServer(cfg, db)
		if err := webServer.Start(); err != nil {
			log.Fatal("Web server failed:", err)
		}
	}
}
