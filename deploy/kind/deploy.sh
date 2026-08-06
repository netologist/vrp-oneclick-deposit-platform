#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
CLUSTER="${KIND_CLUSTER_NAME:-vrp-demo}"
TAG="${IMAGE_TAG:-dev}"

kubectl config use-context "kind-${CLUSTER}"

# Build + load images
"$ROOT/deploy/kind/build-images.sh"

# Apply base manifests
kubectl apply -k "$ROOT/k8s"

# Wait for postgres then run migrations via one-shot job
echo "waiting for postgres..."
kubectl -n vrp-demo rollout status deploy/postgres --timeout=180s
kubectl -n vrp-demo wait --for=condition=ready pod -l app=postgres --timeout=180s

# Apply migrate job (recreate)
kubectl -n vrp-demo delete job migrate --ignore-not-found
kubectl apply -f "$ROOT/k8s/jobs/migrate.yaml"
kubectl -n vrp-demo wait --for=condition=complete job/migrate --timeout=180s

# Rollout apps
for d in merchant-svc consent-svc risk-svc ledger-svc bank-adapter payment-svc notification-svc gateway; do
  kubectl -n vrp-demo rollout status "deploy/$d" --timeout=180s || true
done

echo
echo "pods:"
kubectl -n vrp-demo get pods -o wide
echo
echo "Gateway via ingress: http://api.vrp.local/  (add '127.0.0.1 api.vrp.local' to /etc/hosts)"
echo "Or: kubectl -n vrp-demo port-forward svc/gateway 8080:80"
