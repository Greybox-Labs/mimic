# Kubernetes Deployment for Mimic

This document provides everything needed to deploy Mimic in a Kubernetes cluster using Docker and Helm.

## 📁 Files Created

### Docker

- `Dockerfile` - Multi-architecture build supporting amd64 and arm64
- `.dockerignore` - Excludes unnecessary files from build context

### Helm Chart (`helm/mimic/`)

- `Chart.yaml` - Chart metadata
- `values.yaml` - Default configuration values
- `templates/` - Kubernetes resource templates
  - `deployment.yaml` - Main application deployment
  - `service.yaml` - Service for HTTP and gRPC endpoints
  - `configmap.yaml` - Configuration file
  - `serviceaccount.yaml` - Service account
  - `pvc.yaml` - Persistent volume claim for database
  - `ingress.yaml` - Ingress configuration
  - `hpa.yaml` - Horizontal Pod Autoscaler
  - `pdb.yaml` - Pod Disruption Budget
  - `servicemonitor.yaml` - Prometheus ServiceMonitor
  - `podmonitor.yaml` - Prometheus PodMonitor
  - `_helpers.tpl` - Template helpers

### Examples
- `examples/k8s-deployment.yaml` - Example configurations for dev/test/prod
- `examples/quick-start.sh` - Quick start script
- `README-K8S.md` - Detailed deployment guide

## 🚀 Quick Start

1. **Build and deploy:**
   ```bash
   ./examples/quick-start.sh
   ```

2. **Access the application:**
   ```bash
   # Web UI
   http://localhost:8080
   
   # Test proxy
   curl http://localhost:8080/proxy/httpbin/get
   ```

## 🔧 Configuration Highlights

### Multi-Environment Support
- **Development**: Recording mode with minimal resources
- **Testing**: Mock mode with ingress for integration tests  
- **Production**: High availability with autoscaling and monitoring

### Security Features
- Non-root container execution
- Read-only root filesystem option
- Security contexts and pod security standards
- RBAC with minimal permissions

### Scalability
- Horizontal Pod Autoscaler support
- Pod Disruption Budgets for availability
- Resource limits and requests
- Anti-affinity rules for distribution

### Persistence
- SQLite database stored in persistent volumes
- Configurable storage classes and sizes
- Backup-friendly volume configurations

### Monitoring
- Prometheus ServiceMonitor and PodMonitor
- Health checks (liveness, readiness, startup)
- Structured logging

## 📋 Key Configuration Options

| Feature | Values Path | Description |
|---------|-------------|-------------|
| Mode | `config.mode` | record/mock/replay |
| Proxies | `config.proxies` | Target service configurations |
| Persistence | `persistence.enabled` | Enable database persistence |
| Scaling | `autoscaling.enabled` | Enable horizontal scaling |
| Monitoring | `serviceMonitor.enabled` | Enable Prometheus monitoring |
| Security | `podSecurityContext` | Pod security settings |

## 🏗️ Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Applications  │────│      Mimic      │────│ Target Services │
│                 │    │                 │    │                 │
│ - HTTP clients  │    │ - Record/Mock   │    │ - APIs          │
│ - gRPC clients  │    │ - Web UI        │    │ - gRPC services │
│                 │    │ - Database      │    │                 │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                              │
                              ▼
                       ┌─────────────────┐
                       │ Persistent      │
                       │ Volume          │
                       │ (SQLite DB)     │
                       └─────────────────┘
```

## 🔍 Use Cases

### 1. Development Environment
```bash
helm install mimic-dev ./helm/mimic \
  --set config.mode=record \
  --set config.proxies.api.target_host=api.example.com
```

### 2. Testing Environment
```bash
helm install mimic-test ./helm/mimic \
  --set config.mode=mock \
  --set ingress.enabled=true
```

### 3. Production Mock Server
```bash
helm install mimic-prod ./helm/mimic \
  -f examples/values-prod.yaml
```

## ✅ Validation

The Helm chart has been validated and passes linting:
```bash
$ helm lint helm/mimic
==> Linting mimic
[INFO] Chart.yaml: icon is recommended

1 chart(s) linted, 0 chart(s) failed
```

## 📚 Next Steps

1. **Customize** the values.yaml for your environment
2. **Build** the Docker image and push to your registry
3. **Deploy** using Helm with your custom configuration
4. **Configure** your applications to use Mimic as a proxy
5. **Monitor** using the web UI and Prometheus metrics

For detailed instructions, see `README-K8S.md`.
