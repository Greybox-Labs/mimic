#!/bin/bash
set -e

# Mimic Kubernetes Quick Start Script
echo "🚀 Mimic Kubernetes Quick Start"

# Check prerequisites
echo "📋 Checking prerequisites..."

if ! command -v kubectl &> /dev/null; then
    echo "❌ kubectl is required but not installed"
    exit 1
fi

if ! command -v helm &> /dev/null; then
    echo "❌ helm is required but not installed"
    exit 1
fi

if ! command -v docker &> /dev/null; then
    echo "❌ docker is required but not installed"
    exit 1
fi

# Check if kubectl is configured
if ! kubectl cluster-info &> /dev/null; then
    echo "❌ kubectl is not configured or cluster is not accessible"
    exit 1
fi

echo "✅ All prerequisites met"

# Build Docker image
echo "🔨 Building Mimic Docker image..."
./scripts/build-local.sh || {
    echo "❌ Failed to build Docker image"
    exit 1
}

# If using kind, load image into cluster
if kubectl config current-context | grep -q kind; then
    echo "📦 Loading image into kind cluster..."
    kind load docker-image mimic:latest
fi

# Create namespace
echo "🏠 Creating mimic-demo namespace..."
kubectl create namespace mimic-demo --dry-run=client -o yaml | kubectl apply -f -

# Deploy with Helm
echo "🚢 Deploying Mimic with Helm..."
helm upgrade --install mimic-demo ./helm/mimic \
    --namespace mimic-demo \
    --set image.tag=latest \
    --set image.pullPolicy=Never \
    --set config.mode=record \
    --set config.proxies.httpbin.protocol=https \
    --set config.proxies.httpbin.target_host=httpbin.org \
    --set config.proxies.httpbin.target_port=443 \
    --set config.proxies.httpbin.session_name=httpbin-demo \
    --wait

# Wait for deployment
echo "⏳ Waiting for deployment to be ready..."
kubectl wait --for=condition=available --timeout=300s deployment/mimic-demo -n mimic-demo

# Get status
echo "📊 Deployment status:"
kubectl get pods,svc,ingress -n mimic-demo

# Port forward for access
echo "🌐 Setting up port forwarding..."
echo "Access Mimic Web UI at: http://localhost:8080"
echo "HTTP Proxy endpoint: http://localhost:8080/proxy/httpbin/"
echo "gRPC endpoint: localhost:9090"
echo ""
echo "To test the proxy:"
echo "  curl http://localhost:8080/proxy/httpbin/get"
echo ""
echo "To stop port forwarding, press Ctrl+C"

kubectl port-forward svc/mimic-demo 8080:8080 9090:9090 -n mimic-demo