package mock

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"mimic/config"
	"mimic/proxy"
	"mimic/storage"
)

// GRPCMockRoute represents a mock routing rule for gRPC services
type GRPCMockRoute struct {
	Name           string              // Route name for identification
	ServicePattern *regexp.Regexp      // Pattern to match service names
	MethodPattern  *regexp.Regexp      // Pattern to match method names
	Config         *config.ProxyConfig // Configuration for this route
	Session        *storage.Session    // Session for this route
}

// GRPCMockRouter handles routing gRPC mock calls based on service/method patterns
type GRPCMockRouter struct {
	routes       []*GRPCMockRoute
	database     *storage.Database
	grpcHandler  *proxy.GRPCHandler
	webServer    proxy.WebBroadcaster
	defaultRoute *GRPCMockRoute // Fallback route if no patterns match
}

// NewGRPCMockRouter creates a new gRPC mock router with multiple routes
func NewGRPCMockRouter(routeConfigs map[string]config.ProxyConfig, db *storage.Database, webServer proxy.WebBroadcaster) (*GRPCMockRouter, error) {
	router := &GRPCMockRouter{
		routes:      make([]*GRPCMockRoute, 0),
		database:    db,
		grpcHandler: proxy.NewGRPCHandler([]string{}), // Use empty redact patterns for now
		webServer:   webServer,
	}

	for name, proxyConfig := range routeConfigs {
		session, err := db.GetOrCreateSession(proxyConfig.SessionName, fmt.Sprintf("Mock session for %s", name))
		if err != nil {
			return nil, fmt.Errorf("failed to create session for mock route %s: %w", name, err)
		}

		route := &GRPCMockRoute{
			Name:    name,
			Config:  &proxyConfig,
			Session: session,
		}

		// Parse service and method patterns from config
		if servicePattern := proxyConfig.ServicePattern; servicePattern != "" {
			if pattern, err := regexp.Compile(servicePattern); err == nil {
				route.ServicePattern = pattern
			} else {
				log.Printf("Invalid service pattern for mock route %s: %v", name, err)
			}
		}

		if methodPattern := proxyConfig.MethodPattern; methodPattern != "" {
			if pattern, err := regexp.Compile(methodPattern); err == nil {
				route.MethodPattern = pattern
			} else {
				log.Printf("Invalid method pattern for mock route %s: %v", name, err)
			}
		}

		// Set as default route if specified
		if proxyConfig.IsDefault {
			router.defaultRoute = route
		} else {
			router.routes = append(router.routes, route)
		}

		log.Printf("Added gRPC mock route '%s' for session '%s' (service: %s, method: %s)",
			name, session.SessionName, proxyConfig.ServicePattern, proxyConfig.MethodPattern)
	}

	return router, nil
}

// GetUnknownServiceHandler returns a handler that routes gRPC mock calls based on service/method patterns
func (r *GRPCMockRouter) GetUnknownServiceHandler() grpc.StreamHandler {
	proxy.RegisterRawCodec()

	return func(srv interface{}, stream grpc.ServerStream) error {
		fullMethodName, ok := grpc.MethodFromServerStream(stream)
		if !ok {
			return status.Errorf(codes.Internal, "failed to get method from stream")
		}

		log.Printf("gRPC Mock Router: routing %s", fullMethodName)

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
			return status.Errorf(codes.Unimplemented, "no mock route found for service %s method %s", serviceName, methodName)
		}

		log.Printf("gRPC Mock Router: matched route '%s' for %s", route.Name, fullMethodName)

		// Handle the mock request using the found route's session
		return handleGRPCMockRequest(stream, r.database, route.Session, r.grpcHandler, r.webServer)
	}
}

// findRoute finds the best matching route for a service/method combination
func (r *GRPCMockRouter) findRoute(serviceName, methodName, fullMethodName string) *GRPCMockRoute {
	// Try to find exact pattern matches first
	for _, route := range r.routes {
		if r.routeMatches(route, serviceName, methodName, fullMethodName) {
			return route
		}
	}

	// Fall back to default route if available
	if r.defaultRoute != nil {
		log.Printf("gRPC Mock Router: using default route '%s' for %s", r.defaultRoute.Name, fullMethodName)
		return r.defaultRoute
	}

	return nil
}

// routeMatches checks if a route matches the given service/method
func (r *GRPCMockRouter) routeMatches(route *GRPCMockRoute, serviceName, methodName, fullMethodName string) bool {
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
		log.Printf("Warning: mock route '%s' has no patterns - matches all", route.Name)
		return true
	}

	return true
}

// GetRoutes returns all configured routes for debugging/monitoring
func (r *GRPCMockRouter) GetRoutes() []*GRPCMockRoute {
	routes := make([]*GRPCMockRoute, len(r.routes))
	copy(routes, r.routes)

	if r.defaultRoute != nil {
		routes = append(routes, r.defaultRoute)
	}

	return routes
}
