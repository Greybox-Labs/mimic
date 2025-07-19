package proxy

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"mimic/config"
	"mimic/storage"
)

// GRPCRoute represents a routing rule for gRPC services
type GRPCRoute struct {
	Name           string              // Route name for identification
	ServicePattern *regexp.Regexp      // Pattern to match service names
	MethodPattern  *regexp.Regexp      // Pattern to match method names
	Config         *config.ProxyConfig // Target configuration
	Proxy          *RawGRPCProxy       // Proxy instance for this route
}

// GRPCRouter handles routing gRPC calls to different backends based on service/method patterns
type GRPCRouter struct {
	routes       []*GRPCRoute
	database     *storage.Database
	webServer    WebBroadcaster
	defaultRoute *GRPCRoute // Fallback route if no patterns match
}

// NewGRPCRouter creates a new gRPC router with multiple routes
func NewGRPCRouter(routeConfigs map[string]config.ProxyConfig, mode string, db *storage.Database, webServer WebBroadcaster) (*GRPCRouter, error) {
	router := &GRPCRouter{
		routes:    make([]*GRPCRoute, 0),
		database:  db,
		webServer: webServer,
	}

	for name, proxyConfig := range routeConfigs {
		session, err := db.GetOrCreateSession(proxyConfig.SessionName, fmt.Sprintf("Proxy session for %s", name))
		if err != nil {
			return nil, fmt.Errorf("failed to create session for route %s: %w", name, err)
		}

		grpcHandler := NewGRPCHandler([]string{}) // Use empty redact patterns for now
		rawProxy := NewRawGRPCProxy(&proxyConfig, mode, db, session, grpcHandler)

		if webServer != nil {
			rawProxy.SetWebBroadcaster(webServer)
		}

		route := &GRPCRoute{
			Name:   name,
			Config: &proxyConfig,
			Proxy:  rawProxy,
		}

		// Parse service and method patterns from config
		if servicePattern := proxyConfig.ServicePattern; servicePattern != "" {
			if pattern, err := regexp.Compile(servicePattern); err == nil {
				route.ServicePattern = pattern
			} else {
				log.Printf("Invalid service pattern for route %s: %v", name, err)
			}
		}

		if methodPattern := proxyConfig.MethodPattern; methodPattern != "" {
			if pattern, err := regexp.Compile(methodPattern); err == nil {
				route.MethodPattern = pattern
			} else {
				log.Printf("Invalid method pattern for route %s: %v", name, err)
			}
		}

		// Set as default route if specified
		if proxyConfig.IsDefault {
			router.defaultRoute = route
		} else {
			router.routes = append(router.routes, route)
		}

		log.Printf("Added gRPC route '%s' -> %s:%d (service: %s, method: %s)",
			name, proxyConfig.TargetHost, proxyConfig.TargetPort,
			proxyConfig.ServicePattern, proxyConfig.MethodPattern)
	}

	return router, nil
}

// GetUnknownServiceHandler returns a handler that routes gRPC calls based on service/method patterns
func (r *GRPCRouter) GetUnknownServiceHandler() grpc.StreamHandler {
	RegisterRawCodec()

	return func(srv interface{}, stream grpc.ServerStream) error {
		fullMethodName, ok := grpc.MethodFromServerStream(stream)
		if !ok {
			return status.Errorf(codes.Internal, "failed to get method from stream")
		}

		log.Printf("gRPC Router: routing %s", fullMethodName)

		// Extract service and method from full method name
		// Format: /package.ServiceName/MethodName
		parts := strings.Split(strings.TrimPrefix(fullMethodName, "/"), "/")
		if len(parts) != 2 {
			return status.Errorf(codes.InvalidArgument, "invalid method name format: %s", fullMethodName)
		}

		serviceName := parts[0]
		methodName := parts[1]

		// Find matching route
		route := r.findRoute(serviceName, methodName, fullMethodName)
		if route == nil {
			return status.Errorf(codes.Unimplemented, "no route found for service %s method %s", serviceName, methodName)
		}

		log.Printf("gRPC Router: matched route '%s' for %s", route.Name, fullMethodName)

		// Delegate to the route's proxy handler
		return route.Proxy.GetUnknownServiceHandler()(srv, stream)
	}
}

// findRoute finds the best matching route for a service/method combination
func (r *GRPCRouter) findRoute(serviceName, methodName, fullMethodName string) *GRPCRoute {
	// Try to find exact pattern matches first
	for _, route := range r.routes {
		if r.routeMatches(route, serviceName, methodName, fullMethodName) {
			return route
		}
	}

	// Fall back to default route if available
	if r.defaultRoute != nil {
		log.Printf("gRPC Router: using default route '%s' for %s", r.defaultRoute.Name, fullMethodName)
		return r.defaultRoute
	}

	return nil
}

// routeMatches checks if a route matches the given service/method
func (r *GRPCRouter) routeMatches(route *GRPCRoute, serviceName, methodName, fullMethodName string) bool {
	// Check service pattern
	if route.ServicePattern != nil {
		if !route.ServicePattern.MatchString(serviceName) {
			return false
		}
	}

	// Check method pattern
	if route.MethodPattern != nil {
		if !route.MethodPattern.MatchString(methodName) {
			return false
		}
	}

	// If no patterns are specified, this route matches everything (shouldn't happen with proper config)
	if route.ServicePattern == nil && route.MethodPattern == nil {
		log.Printf("Warning: route '%s' has no patterns - matches all", route.Name)
		return true
	}

	return true
}

// GetRoutes returns all configured routes for debugging/monitoring
func (r *GRPCRouter) GetRoutes() []*GRPCRoute {
	routes := make([]*GRPCRoute, len(r.routes))
	copy(routes, r.routes)

	if r.defaultRoute != nil {
		routes = append(routes, r.defaultRoute)
	}

	return routes
}
