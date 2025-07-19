# Kubernetes Deployment Guide for Mimic

This guide covers deploying Mimic in Kubernetes using Docker and Helm.

## Prerequisites

- Kubernetes cluster (1.19+)
- Helm 3.x
- Docker (for building images)
- kubectl configured for your cluster

## Building the Docker Image

1. **Build the image locally:**
   ```bash
   ./scripts/build-local.sh
   ```

   Or manually:
   ```bash
   docker build -t mimic:latest .
   ```

2. **For production, build multi-architecture and push to your registry:**
   ```bash
   # Multi-architecture build (requires Docker buildx)
   REGISTRY=your-registry.com TAG=v1.0.0 ./scripts/build-multiarch.sh
   
   # Or single architecture
   docker tag mimic:latest your-registry.com/mimic:v1.0.0
   docker push your-registry.com/mimic:v1.0.0
   ```

## Quick Start with Helm

1. **Install Mimic with default values:**
   ```bash
   helm install mimic ./helm/mimic
   ```

2. **Install with custom values:**
   ```bash
   helm install mimic ./helm/mimic -f custom-values.yaml
   ```

3. **Access the application:**
   ```bash
   kubectl port-forward svc/mimic 8080:8080
   # Open http://localhost:8080 in your browser
   ```

## Configuration Examples

### Basic Recording Setup

```yaml
# values.yaml
config:
  mode: "record"
  proxies:
    api-service:
      protocol: "https"
      target_host: "api.example.com"
      target_port: 443
      session_name: "api-recording"
    
    grpc-service:
      protocol: "grpc"
      target_host: "grpc.example.com"
      target_port: 443
      session_name: "grpc-recording"
      service_pattern: "example\\..*"

# Install
helm install mimic ./helm/mimic -f values.yaml
```

### Mock Server Setup

```yaml
# values.yaml
config:
  mode: "mock"
  mock:
    matching_strategy: "exact"
    sequence_mode: "ordered"
  proxies:
    api-service:
      protocol: "https" 
      session_name: "api-recordings"
      
ingress:
  enabled: true
  hosts:
    - host: mimic.example.com
      paths:
        - path: /
          pathType: Prefix

# Install
helm install mimic ./helm/mimic -f values.yaml
```

### Production Setup with Persistence

```yaml
# values.yaml
replicaCount: 2

resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 200m
    memory: 256Mi

persistence:
  enabled: true
  storageClass: "gp2"
  size: 5Gi

podDisruptionBudget:
  enabled: true
  minAvailable: 1

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70

service:
  type: LoadBalancer

# Install
helm install mimic ./helm/mimic -f values.yaml
```

## Deployment Scenarios

### 1. API Testing in Development

Deploy Mimic as a recording proxy for capturing API interactions:

```yaml
config:
  mode: "record"
  proxies:
    external-api:
      protocol: "https"
      target_host: "external-api.com"
      target_port: 443
      session_name: "dev-testing"

# Your application should point to:
# http://mimic.default.svc.cluster.local:8080/proxy/external-api/
```

### 2. Integration Testing

Use recorded sessions for integration tests:

```yaml
config:
  mode: "mock"
  proxies:
    test-api:
      session_name: "integration-tests"
      
# Tests can hit: http://mimic.test.svc.cluster.local:8080/proxy/test-api/
```

### 3. gRPC Service Mocking

Mock gRPC services for testing:

```yaml
config:
  mode: "mock"
  proxies:
    user-service:
      protocol: "grpc"
      session_name: "user-service-mocks"
      service_pattern: "user\\..*"
      
# gRPC clients should connect to: mimic.default.svc.cluster.local:9090
```

## Configuration Reference

### Core Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `config.mode` | Operation mode (record/mock/replay) | `"record"` |
| `config.server.listen_port` | HTTP server port | `8080` |
| `config.server.grpc_port` | gRPC server port | `9090` |

### Persistence

| Parameter | Description | Default |
|-----------|-------------|---------|
| `persistence.enabled` | Enable persistent storage | `true` |
| `persistence.size` | Storage size | `"1Gi"` |
| `persistence.storageClass` | Storage class | `""` |

### Security

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podSecurityContext.runAsUser` | User ID | `1001` |
| `podSecurityContext.runAsNonRoot` | Run as non-root | `true` |
| `securityContext.readOnlyRootFilesystem` | Read-only root filesystem | `false` |

## Monitoring

### Prometheus Integration

Enable ServiceMonitor for Prometheus Operator:

```yaml
serviceMonitor:
  enabled: true
  interval: 30s
  labels:
    prometheus: default
```

### Health Checks

The deployment includes:
- **Liveness Probe**: Checks if the application is running
- **Readiness Probe**: Checks if the application is ready to serve traffic  
- **Startup Probe**: Handles application startup

## Troubleshooting

### Check Pod Status
```bash
kubectl get pods -l app.kubernetes.io/name=mimic
kubectl describe pod <pod-name>
```

### View Logs
```bash
kubectl logs -f deployment/mimic
```

### Debug Configuration
```bash
kubectl get configmap mimic-config -o yaml
```

### Test Connectivity
```bash
# Test HTTP endpoint
kubectl run debug --image=curlimages/curl -it --rm -- curl http://mimic:8080/

# Test gRPC endpoint (if grpcurl is available)
kubectl run debug --image=fullstorydev/grpcurl -it --rm -- grpcurl -plaintext mimic:9090 list
```

### Common Issues

1. **Database permission issues**: Ensure persistence is enabled and storage class exists
2. **Image pull errors**: Verify image name and registry credentials
3. **Service connectivity**: Check service and ingress configuration
4. **Config parsing errors**: Validate YAML syntax in values.yaml

## Upgrading

```bash
# Upgrade to new version
helm upgrade mimic ./helm/mimic --set image.tag=v1.1.0

# Upgrade with new configuration
helm upgrade mimic ./helm/mimic -f new-values.yaml
```

## Uninstalling

```bash
# Remove the deployment (keeps PVC by default)
helm uninstall mimic

# Remove PVC as well (this will delete all recorded data)
kubectl delete pvc mimic-data
```

## Security Considerations

1. **Network Policies**: Implement network policies to restrict traffic
2. **RBAC**: The service account has minimal permissions by default
3. **Secrets**: Use Kubernetes secrets for sensitive configuration
4. **TLS**: Enable TLS for production deployments via ingress
5. **Image Security**: Scan images for vulnerabilities before deployment

## Performance Tuning

1. **Resource Limits**: Set appropriate CPU/memory limits based on traffic
2. **Replica Count**: Scale horizontally for high availability
3. **Storage**: Use SSD storage classes for better database performance
4. **Autoscaling**: Enable HPA based on CPU/memory metrics