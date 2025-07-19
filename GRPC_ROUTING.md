# gRPC Path-Based Routing

Mimic uses a single gRPC server with intelligent path-based routing to handle multiple backend services. This approach is more efficient and easier to manage than running multiple gRPC server instances.

## How gRPC Routing Works
```yaml
# config-grpc-routing.yaml
proxies:
  user-service:
    mode: "record"
    protocol: "grpc"
    target_host: "user-api.internal.com"
    target_port: 9090
    session_name: "user-session"
    service_pattern: "com\\.example\\.userservice\\..*"  # Routes based on service name
    
  order-service:
    mode: "record"
    protocol: "grpc"
    target_host: "order-api.internal.com"
    target_port: 9091
    session_name: "order-session"
    service_pattern: "com\\.example\\.orderservice\\..*"
    
  default-backend:
    mode: "record"
    protocol: "grpc"
    target_host: "default-api.internal.com"
    target_port: 9090
    session_name: "default-session"
    is_default: true  # Catches unmatched requests
```

**Result**: Creates 1 gRPC server on configurable port (defaults to 9080) that routes based on service patterns

## How It Works

### Service/Method Routing

gRPC method names follow the format: `/package.ServiceName/MethodName`

Examples:
- `/com.example.userservice.UserService/GetUser` → routed to user-api.internal.com:9090
- `/com.example.orderservice.OrderService/CreateOrder` → routed to order-api.internal.com:9091
- `/grpc.health.v1.Health/Check` → routed to default-api.internal.com:9090

### Pattern Types

1. **Service Pattern**: Routes based on service name
   ```yaml
   service_pattern: "com\\.example\\.userservice\\..*"
   ```

2. **Method Pattern**: Routes based on method name
   ```yaml
   method_pattern: "Get.*|List.*"  # Only GET and LIST methods
   ```

3. **Combined Patterns**: Both service and method must match
   ```yaml
   service_pattern: "com\\.example\\..*"
   method_pattern: "Health.*"
   ```

4. **Default Route**: Catches everything that doesn't match other patterns
   ```yaml
   is_default: true
   ```

## Usage Examples

### Start the Server with gRPC Routing
```bash
# Start mimic with gRPC routing (routing is automatic for gRPC proxies)
mimic --config config-grpc-routing.yaml

# Output:
# Starting multi-proxy server
# Web UI available at http://0.0.0.0:8080/
# gRPC router server listening on 0.0.0.0:9080
#   → Route 'user-service': com\.example\.userservice\..* -> user-api.internal.com:9090
#   → Route 'order-service': com\.example\.orderservice\..* -> order-api.internal.com:9091
#   → Route 'default-backend': (default) -> default-api.internal.com:9090
# gRPC router server available at 0.0.0.0:9080
```

### Test Routing
```bash
# This goes to user-api.internal.com:9090
grpcurl -plaintext localhost:9080 com.example.userservice.UserService/GetUser

# This goes to order-api.internal.com:9091
grpcurl -plaintext localhost:9080 com.example.orderservice.OrderService/CreateOrder

# This goes to default-api.internal.com:9090 (default route)
grpcurl -plaintext localhost:9080 some.other.service.SomeService/SomeMethod
```

## Benefits of gRPC Routing

1. **Resource Efficiency**: Single gRPC server handles all routes
2. **Simplified Deployment**: One configurable gRPC port to manage
3. **Flexible Routing**: Route by service name, method patterns, or any combination
4. **Session Isolation**: Different sessions per route for organized recording
5. **Fallback Support**: Default routes for unmatched requests
6. **Automatic**: No special configuration needed - just define gRPC proxies with patterns

## Advanced Examples

### Method-Specific Routing
```yaml
# Route only health checks to a dedicated service
health-service:
  mode: "record"
  protocol: "grpc"
  target_host: "health.internal.com"
  target_port: 9093
  session_name: "health-session"
  method_pattern: "Check|Watch"  # Only health check methods
```

### Environment-Based Routing
```yaml
# Route staging services to staging backends
staging-services:
  mode: "record"
  protocol: "grpc"
  target_host: "staging-api.internal.com"
  target_port: 9090
  session_name: "staging-session"
  service_pattern: ".*\\.staging\\..*"
  
# Route production services to production backends
prod-services:
  mode: "record"
  protocol: "grpc"
  target_host: "prod-api.internal.com"
  target_port: 9090
  session_name: "prod-session"
  service_pattern: ".*\\.prod\\..*"
```

### Mock Mode with Routing
```yaml
# Same routing works for mock mode
proxies:
  user-service-mock:
    mode: "mock"  # Switch to mock mode
    protocol: "grpc"
    session_name: "user-session"  # Use recorded session
    service_pattern: "com\\.example\\.userservice\\..*"
    
  order-service-mock:
    mode: "mock"
    protocol: "grpc"
    session_name: "order-session"
    service_pattern: "com\\.example\\.orderservice\\..*"
```

## Implementation Details

The routing is implemented using:

1. **GRPCRouter**: Manages multiple routes and pattern matching
2. **GRPCRoute**: Individual route with target configuration and patterns  
3. **UnknownServiceHandler**: Intercepts all gRPC calls and routes them
4. **Pattern Matching**: Uses regex patterns to match service/method names
5. **MultiProxyServer**: Automatically creates routing when gRPC proxies are configured

The router extracts the service and method from the full gRPC method name (`/package.ServiceName/MethodName`) and matches against configured patterns to determine the target backend.

This approach provides the flexibility of multiple gRPC proxies while maintaining the simplicity of a single server endpoint. Routing is enabled automatically when you configure gRPC proxies - no special commands or setup required!