# Kubernetes Deployment Guide

This directory contains Kubernetes manifests for deploying IZI SIPREC.

## Directory Structure

```
kubernetes/
├── base/                    # Base manifests
│   ├── deployment.yaml      # Deployment configuration
│   ├── service.yaml         # Service definitions
│   ├── configmap.yaml       # Configuration
│   ├── secret.yaml          # Secrets template
│   ├── pvc.yaml             # Persistent storage
│   ├── rbac.yaml            # RBAC configuration
│   ├── hpa.yaml             # Auto-scaling
│   └── kustomization.yaml   # Kustomize base
├── overlays/
│   ├── production/          # Production-specific config
│   └── staging/             # Staging-specific config
└── README.md
```

## Quick Start

### Prerequisites

- Kubernetes cluster (1.24+)
- kubectl configured
- Kustomize (or kubectl with kustomize support)

### Deploy to Staging

```bash
# Preview manifests
kubectl kustomize deploy/kubernetes/overlays/staging

# Apply
kubectl apply -k deploy/kubernetes/overlays/staging
```

### Deploy to Production

```bash
# Update image tag in production/kustomization.yaml first
kubectl apply -k deploy/kubernetes/overlays/production
```

## Configuration

### Environment Variables

All configuration is managed via ConfigMap. Key settings:

| Variable | Description | Default |
|----------|-------------|---------|
| `SIP_HOST` | SIP bind address | `0.0.0.0` |
| `PORTS` | SIP ports | `5060,5061` |
| `HTTP_PORT` | HTTP API port | `8080` |
| `RTP_PORT_MIN` | RTP port range start | `10000` |
| `RTP_PORT_MAX` | RTP port range end | `20000` |
| `LOG_LEVEL` | Logging level | `info` |
| `CLUSTER_ENABLED` | Enable clustering | `true` |

### Secrets

Update `secret.yaml` or use external secret management:

```bash
# Create secrets from literal values
kubectl create secret generic siprec-secrets \
  --from-literal=AUTH_API_KEY=your-api-key \
  --from-literal=AUTH_ADMIN_PASSWORD_HASH='$2y$...' \
  -n siprec-production
```

For TLS certificates:

```bash
kubectl create secret tls siprec-tls \
  --cert=path/to/tls.crt \
  --key=path/to/tls.key \
  -n siprec-production
```

## Networking

### SIP/RTP Considerations

SIP and RTP require special networking considerations in Kubernetes:

1. **UDP Load Balancing**: Use `externalTrafficPolicy: Local` to preserve source IP
2. **RTP Port Range**: Consider using `hostNetwork: true` or NodePort services
3. **NAT Traversal**: Set `BEHIND_NAT=true` and configure `EXTERNAL_IP`

### Service Types

- `siprec-http`: ClusterIP for internal HTTP access
- `siprec-sip-udp`: LoadBalancer for SIP UDP
- `siprec-sip-tcp`: LoadBalancer for SIP TCP/TLS
- `siprec-headless`: Headless service for pod discovery

## Storage

### Recordings Storage

The default PVC uses `ReadWriteOnce`. For multi-pod deployments:

1. **NFS/EFS**: Use `ReadWriteMany` storage class
2. **Object Storage**: Enable S3/GCS upload in configuration
3. **Local Storage**: Use StatefulSet with local persistent volumes

## Scaling

### Horizontal Pod Autoscaler

Configured to scale based on:
- CPU utilization (70% target)
- Memory utilization (80% target)

Adjust in `hpa-patch.yaml` for each environment.

### Manual Scaling

```bash
kubectl scale deployment siprec-server --replicas=5 -n siprec-production
```

## Monitoring

### Prometheus Metrics

Annotations are configured for automatic scraping:

```yaml
prometheus.io/scrape: "true"
prometheus.io/port: "8080"
prometheus.io/path: "/metrics"
```

### Health Checks

- Liveness: `/health/live`
- Readiness: `/health/ready`

## Troubleshooting

### Check Pod Status

```bash
kubectl get pods -n siprec-production
kubectl describe pod <pod-name> -n siprec-production
kubectl logs <pod-name> -n siprec-production
```

### Exec into Pod

```bash
kubectl exec -it <pod-name> -n siprec-production -- /bin/sh
```

### Check Services

```bash
kubectl get svc -n siprec-production
kubectl get endpoints -n siprec-production
```

## Cloud Provider Notes

### AWS EKS

- Use ALB Ingress Controller for HTTP
- Use NLB for SIP (UDP support)
- Consider EFS for shared storage

### GKE

- Use Cloud Load Balancer annotations
- Consider Filestore for shared storage

### Azure AKS

- Use Azure Load Balancer for SIP
- Consider Azure Files for shared storage
