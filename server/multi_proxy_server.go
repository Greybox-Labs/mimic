package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"mimic/config"
	"mimic/mock"
	"mimic/proxy"
	"mimic/storage"
	"mimic/web"
)

type MultiProxyServer struct {
	config    *config.Config
	database  *storage.Database
	webServer *web.Server
	proxies   map[string]ProxyHandler
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

	// Initialize all configured proxies
	for name, proxyConfig := range cfg.Proxies {
		var handler ProxyHandler
		var err error

		switch proxyConfig.Mode {
		case "record":
			handler, err = proxy.NewProxyEngineWithBroadcaster(proxyConfig, db, webServer)
		case "mock":
			handler, err = mock.NewMockEngineWithBroadcaster(proxyConfig, db, webServer)
		default:
			return nil, fmt.Errorf("invalid proxy mode for '%s': %s", name, proxyConfig.Mode)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to create proxy handler for '%s': %w", name, err)
		}

		server.proxies[name] = handler
		log.Printf("Initialized proxy '%s' in %s mode", name, proxyConfig.Mode)
	}

	return server, nil
}

func (s *MultiProxyServer) Start() error {
	mux := http.NewServeMux()

	// Register web UI routes at top level
	s.webServer.RegisterRoutes(mux)

	// Register proxy routes at /proxy/<name>/
	for name, proxyHandler := range s.proxies {
		proxyPath := fmt.Sprintf("/proxy/%s/", name)
		mux.HandleFunc(proxyPath, func(w http.ResponseWriter, r *http.Request) {
			// Strip the proxy path prefix and forward to the proxy handler
			originalPath := r.URL.Path
			r.URL.Path = strings.TrimPrefix(originalPath, fmt.Sprintf("/proxy/%s", name))
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
			proxyHandler.HandleRequest(w, r)
		})
		log.Printf("Registered proxy '%s' at path %s", name, proxyPath)
	}

	address := fmt.Sprintf("%s:%d", s.config.Server.ListenHost, s.config.Server.ListenPort)
	log.Printf("Starting multi-proxy server on %s", address)
	log.Printf("Web UI available at http://%s/", address)
	
	for name := range s.proxies {
		log.Printf("Proxy '%s' available at http://%s/proxy/%s/", name, address, name)
	}

	return http.ListenAndServe(address, mux)
}