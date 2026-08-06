#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
CLUSTER="${KIND_CLUSTER_NAME:-vrp-demo}"
TAG="${IMAGE_TAG:-dev}"
SERVICES=(gateway merchant-svc consent-svc payment-svc risk-svc ledger-svc bank-adapter notification-svc)

echo "building images (tag=$TAG)..."
for svc in "${SERVICES[@]}"; do
  img="vrp-demo/${svc}:${TAG}"
  echo "→ $img"
  docker build -t "$img" --build-arg "SERVICE=$svc" .
done

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  echo "loading images into kind cluster $CLUSTER..."
  for svc in "${SERVICES[@]}"; do
    kind load docker-image "vrp-demo/${svc}:${TAG}" --name "$CLUSTER"
  done
else
  echo "kind cluster '$CLUSTER' not found — images built locally only"
fi
echo "done"
