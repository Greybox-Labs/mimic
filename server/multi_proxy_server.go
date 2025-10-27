package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"mimic/config"
	"mimic/mock"
	"mimic/proxy"
	"mimic/storage"
	"mimic/web"

	"google.golang.org/grpc"
)

type MultiProxyServer struct {
	config         *config.Config
	database       *storage.Database
	webServer      *web.Server
	proxies        map[string]ProxyHandler
	grpcServer     *grpc.Server         // Single gRPC server with routing
	grpcRouter     *proxy.GRPCRouter    // For gRPC record proxies
	grpcMockRouter *mock.GRPCMockRouter // For gRPC mock proxies
}

type ProxyHandler interface {
	HandleRequest(w http.ResponseWriter, r *http.Request)
}

func NewMultiProxyServer(cfg *config.Config, db *storage.Database) (*MultiProxyServer, error) {
	webServer := web.NewServer(cfg, db)

	server := &MultiProxyServer{
		config:    cfg,
		database:  db,
		webServer: webServer,
		proxies:   make(map[string]ProxyHandler),
	}

	// Separate HTTP and gRPC proxies
	httpProxies := make(map[string]config.ProxyConfig)
	grpcProxies := make(map[string]config.ProxyConfig)

	for name, proxyConfig := range cfg.Proxies {
		if proxyConfig.Protocol == "grpc" {
			grpcProxies[name] = proxyConfig
		} else {
			// HTTP/HTTPS proxies - handle individually
			httpProxies[name] = proxyConfig
		}
	}

	// Initialize HTTP proxies (existing logic)
	for name, proxyConfig := range httpProxies {
		var handler ProxyHandler

		switch cfg.Mode {
		case "record":
			if proxyEngine, err := proxy.NewProxyEngineWithBroadcaster(proxyConfig, db, webServer); err == nil {
				handler = proxyEngine
			} else {
				return nil, fmt.Errorf("failed to create proxy engine for '%s': %w", name, err)
			}
		case "mock":
			if mockEngine, err := mock.NewMockEngineWithBroadcaster(proxyConfig, cfg.Mock, db, webServer); err == nil {
				handler = mockEngine
			} else {
				return nil, fmt.Errorf("failed to create mock engine for '%s': %w", name, err)
			}
		case "replay":
			// For replay mode, we create a special handler that provides replay endpoints
			if replayHandler, err := NewReplayHandler(&cfg.Replay, db, webServer); err == nil {
				handler = replayHandler
			} else {
				return nil, fmt.Errorf("failed to create replay handler for '%s': %w", name, err)
			}
		default:
			return nil, fmt.Errorf("invalid global mode: %s", cfg.Mode)
		}

		server.proxies[name] = handler
		log.Printf("Initialized HTTP proxy '%s' in %s mode", name, cfg.Mode)
	}

	// Initialize single gRPC server with routing (if any gRPC proxies exist and not in replay mode)
	if len(grpcProxies) > 0 && cfg.Mode != "replay" {
		var unknownServiceHandler grpc.StreamHandler

		// Create gRPC router for record mode
		if cfg.Mode == "record" {
			router, err := proxy.NewGRPCRouter(grpcProxies, cfg.Mode, db, webServer)
			if err != nil {
				return nil, fmt.Errorf("failed to create gRPC router: %w", err)
			}
			server.grpcRouter = router
			unknownServiceHandler = router.GetUnknownServiceHandler()

		}

		// Create gRPC mock router for mock mode
		if cfg.Mode == "mock" {
			mockRouter, err := mock.NewGRPCMockRouter(grpcProxies, db, webServer)
			if err != nil {
				return nil, fmt.Errorf("failed to create gRPC mock router: %w", err)
			}
			server.grpcMockRouter = mockRouter
			// If we don't have a record router, use the mock router as the handler
			if unknownServiceHandler == nil {
				unknownServiceHandler = mockRouter.GetUnknownServiceHandler()
			}
		}
		log.Printf("Initialized gRPC router with %d %s routes", len(grpcProxies), cfg.Mode)

		// Create single gRPC server with routing
		server.grpcServer = grpc.NewServer(
			grpc.MaxRecvMsgSize(64*1024*1024),        // 64MB max receive message size
			grpc.MaxSendMsgSize(64*1024*1024),        // 64MB max send message size
			grpc.MaxHeaderListSize(64*1024*1024),     // 64MB max header list size
			grpc.InitialWindowSize(64*1024*1024),     // 64MB initial window
			grpc.InitialConnWindowSize(64*1024*1024), // 64MB connection window
			grpc.UnknownServiceHandler(unknownServiceHandler),
		)

		log.Printf("Created single gRPC server with routing")
	}

	return server, nil
}

func (s *MultiProxyServer) Start() error {
	// Start single gRPC server with routing if any gRPC proxies exist
	var grpcAddress string
	if s.grpcServer != nil {
		grpcAddress = fmt.Sprintf("%s:%d", s.config.Server.ListenHost, s.config.Server.GRPCPort)

		go func() {
			lis, err := net.Listen("tcp", grpcAddress)
			if err != nil {
				log.Printf("Failed to start gRPC router server on %s: %v", grpcAddress, err)
				return
			}

			log.Printf("gRPC router server listening on %s", grpcAddress)

			// Log route information
			if s.grpcRouter != nil {
				routes := s.grpcRouter.GetRoutes()
				for _, route := range routes {
					if route.Config.ServicePattern != "" {
						log.Printf("  → Route '%s': %s -> %s:%d",
							route.Name,
							route.Config.ServicePattern,
							route.Config.TargetHost,
							route.Config.TargetPort)
					} else {
						log.Printf("  → Route '%s': (default) -> %s:%d",
							route.Name,
							route.Config.TargetHost,
							route.Config.TargetPort)
					}
				}
			}

			if s.grpcMockRouter != nil {
				routes := s.grpcMockRouter.GetRoutes()
				for _, route := range routes {
					if route.Config.ServicePattern != "" {
						log.Printf("  → Mock Route '%s': %s -> session '%s'",
							route.Name,
							route.Config.ServicePattern,
							route.Session.SessionName)
					} else {
						log.Printf("  → Mock Route '%s': (default) -> session '%s'",
							route.Name,
							route.Session.SessionName)
					}
				}
			}

			if err := s.grpcServer.Serve(lis); err != nil {
				log.Printf("gRPC router server failed: %v", err)
			}
		}()
	}

	// Set up HTTP server for web UI and HTTP proxies
	mux := http.NewServeMux()

	// Register HTTP proxy routes FIRST (before web UI catch-all routes)
	httpProxyCount := 0

	for name, proxyHandler := range s.proxies {
		// Capture the name and handler in the closure
		proxyName := name
		handler := proxyHandler

		proxyPath := fmt.Sprintf("/proxy/%s/", proxyName)

		// Regular HTTP proxy
		mux.HandleFunc(proxyPath, func(w http.ResponseWriter, r *http.Request) {
			// Strip the proxy path prefix and forward to the proxy handler
			originalPath := r.URL.Path
			r.URL.Path = strings.TrimPrefix(originalPath, fmt.Sprintf("/proxy/%s", proxyName))
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
			handler.HandleRequest(w, r)
		})
		log.Printf("Registered HTTP proxy '%s' at path %s", proxyName, proxyPath)
		httpProxyCount++
	}

	// Register web UI routes at top level AFTER proxy routes
	s.webServer.RegisterRoutes(mux)

	// Add gRPC info endpoint if gRPC server exists
	if s.grpcServer != nil {
		mux.HandleFunc("/grpc/info", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{
	"message": "gRPC router server with path-based routing",
	"grpc_address": "%s",
	"usage": "Connect your gRPC client to %s",
	"example": "grpcurl -plaintext %s your.service/Method"
}`, grpcAddress, grpcAddress, grpcAddress)
		})
	}

	httpAddress := fmt.Sprintf("%s:%d", s.config.Server.ListenHost, s.config.Server.ListenPort)

	log.Printf("Starting multi-proxy server")
	log.Printf("Web UI available at http://%s/", httpAddress)

	if httpProxyCount > 0 {
		log.Printf("HTTP proxies (%d) available at http://%s/proxy/<name>/", httpProxyCount, httpAddress)
	}

	if s.grpcServer != nil {
		log.Printf("gRPC router server available at %s", grpcAddress)
		log.Printf("gRPC info available at http://%s/grpc/info", httpAddress)
	}

	return http.ListenAndServe(httpAddress, mux)
}

// Stop gracefully stops the server
func (s *MultiProxyServer) Stop() error {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
	return nil
}
