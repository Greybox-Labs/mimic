package server

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"mimic/config"
	"mimic/mock"
	"mimic/proxy"
	"mimic/storage"
	"mimic/web"
)

type MultiProxyServer struct {
	config      *config.Config
	database    *storage.Database
	webServer   *web.Server
	proxies     map[string]ProxyHandler
	grpcServers map[string]*grpc.Server
}

type ProxyHandler interface {
	HandleRequest(w http.ResponseWriter, r *http.Request)
}

func NewMultiProxyServer(cfg *config.Config, db *storage.Database) (*MultiProxyServer, error) {
	webServer := web.NewServer(cfg, db)

	server := &MultiProxyServer{
		config:      cfg,
		database:    db,
		webServer:   webServer,
		proxies:     make(map[string]ProxyHandler),
		grpcServers: make(map[string]*grpc.Server),
	}

	// Initialize all configured proxies
	for name, proxyConfig := range cfg.Proxies {
		var handler ProxyHandler

		switch proxyConfig.Mode {
		case "record":
			if proxyEngine, err := proxy.NewProxyEngineWithBroadcaster(proxyConfig, db, webServer); err == nil {
				handler = proxyEngine
				// Extract gRPC server if this is a gRPC proxy
				if proxyConfig.Protocol == "grpc" {
					server.grpcServers[name] = proxyEngine.GetGRPCServer()
				}
			} else {
				return nil, fmt.Errorf("failed to create proxy engine for '%s': %w", name, err)
			}
		case "mock":
			if mockEngine, err := mock.NewMockEngineWithBroadcaster(proxyConfig, db, webServer); err == nil {
				handler = mockEngine
				// Extract gRPC server if this is a gRPC mock
				if proxyConfig.Protocol == "grpc" {
					server.grpcServers[name] = mockEngine.GetGRPCServer()
				}
			} else {
				return nil, fmt.Errorf("failed to create mock engine for '%s': %w", name, err)
			}
		default:
			return nil, fmt.Errorf("invalid proxy mode for '%s': %s", name, proxyConfig.Mode)
		}

		server.proxies[name] = handler
		log.Printf("Initialized proxy '%s' in %s mode (protocol: %s)", name, proxyConfig.Mode, proxyConfig.Protocol)
	}

	return server, nil
}

func (s *MultiProxyServer) Start() error {
	// Start gRPC servers on separate ports if any exist
	grpcPort := s.config.Server.ListenPort + 1000 // Use a different port range for gRPC
	grpcPortMapping := make(map[string]int) // Track which proxy gets which port
	
	for name, grpcServer := range s.grpcServers {
		if grpcServer != nil {
			currentPort := grpcPort
			grpcPortMapping[name] = currentPort
			
			go func(serverName string, server *grpc.Server, port int) {
				address := fmt.Sprintf("%s:%d", s.config.Server.ListenHost, port)
				lis, err := net.Listen("tcp", address)
				if err != nil {
					log.Printf("Failed to start gRPC server for '%s' on %s: %v", serverName, address, err)
					return
				}
				log.Printf("gRPC proxy '%s' listening on %s", serverName, address)
				if err := server.Serve(lis); err != nil {
					log.Printf("gRPC server for '%s' failed: %v", serverName, err)
				}
			}(name, grpcServer, currentPort)
			grpcPort++
		}
	}

	// Set up HTTP server for web UI and HTTP proxies
	mux := http.NewServeMux()

	// Register web UI routes at top level
	s.webServer.RegisterRoutes(mux)

	// Register proxy routes at /proxy/<name>/
	httpProxyCount := 0
	grpcProxyCount := 0
	baseGRPCPort := s.config.Server.ListenPort + 1000

	for name, proxyHandler := range s.proxies {
		proxyPath := fmt.Sprintf("/proxy/%s/", name)
		
		// Check if this is a gRPC proxy
		if _, isGRPC := s.grpcServers[name]; isGRPC {
			// For gRPC proxies, provide an informational HTTP endpoint
			currentGRPCPort := grpcPortMapping[name] // Use the correct mapped port
			mux.HandleFunc(proxyPath, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, `{
	"message": "This is a gRPC proxy endpoint", 
	"protocol": "grpc",
	"grpc_address": "%s:%d",
	"usage": "Connect your gRPC client to %s:%d",
	"example": "buf curl --schema <your-schema> --protocol grpc %s:%d/your.service/Method"
}`, s.config.Server.ListenHost, currentGRPCPort, s.config.Server.ListenHost, currentGRPCPort, s.config.Server.ListenHost, currentGRPCPort)
			})
			log.Printf("Registered gRPC proxy info '%s' at HTTP path %s", name, proxyPath)
			grpcProxyCount++
		} else {
			// Regular HTTP proxy
			mux.HandleFunc(proxyPath, func(w http.ResponseWriter, r *http.Request) {
				// Strip the proxy path prefix and forward to the proxy handler
				originalPath := r.URL.Path
				r.URL.Path = strings.TrimPrefix(originalPath, fmt.Sprintf("/proxy/%s", name))
				if r.URL.Path == "" {
					r.URL.Path = "/"
				}
				proxyHandler.HandleRequest(w, r)
			})
			log.Printf("Registered HTTP proxy '%s' at path %s", name, proxyPath)
			httpProxyCount++
		}
	}

	httpAddress := fmt.Sprintf("%s:%d", s.config.Server.ListenHost, s.config.Server.ListenPort)
	
	log.Printf("Starting multi-proxy server")
	log.Printf("Web UI available at http://%s/", httpAddress)
	
	if httpProxyCount > 0 {
		log.Printf("HTTP proxies (%d) available at http://%s/proxy/<name>/", httpProxyCount, httpAddress)
	}
	
	if grpcProxyCount > 0 {
		log.Printf("gRPC proxies (%d) available on ports %d-%d", grpcProxyCount, baseGRPCPort, baseGRPCPort+grpcProxyCount-1)
		for name := range s.grpcServers {
			if s.grpcServers[name] != nil {
				currentPort := grpcPortMapping[name]
				log.Printf("  → gRPC proxy '%s' at %s:%d", name, s.config.Server.ListenHost, currentPort)
			}
		}
	}

	return http.ListenAndServe(httpAddress, mux)
}
