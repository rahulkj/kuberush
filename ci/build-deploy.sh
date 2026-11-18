#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

source "${ROOT_DIR}/.envrc"

usage() {
    echo "Usage: $0 [build_and_push|deploy|undeploy] [pi|vm]"
    exit 1
}

build_and_push() {
    echo "==> Building multi-arch image: ${IMAGE_NAME}"
    cd "$ROOT_DIR"

    export DOCKER_CONFIG="${DOCKER_CONFIG:-$PWD/.docker-harbor}"
    mkdir -p "$DOCKER_CONFIG"
    cat > "$DOCKER_CONFIG/config.json" <<JSON
{
  "auths": {
    "$HARBOR_REGISTRY_URL": {
      "auth": "$(printf '%s' "$HARBOR_USERNAME:$HARBOR_PASSWORD" | base64)"
    }
  }
}
JSON

    docker buildx create --use --name kuberush-builder 2>/dev/null || docker buildx use kuberush-builder
    docker run --privileged --rm tonistiigi/binfmt --install all

    docker buildx build \
        --platform linux/amd64,linux/arm64 \
        --provenance=false \
        --tag "${HARBOR_REGISTRY_URL}/${HARBOR_PROJECT}/${IMAGE_NAME}:latest" \
        --tag "${HARBOR_REGISTRY_URL}/${HARBOR_PROJECT}/${IMAGE_NAME}:${GITHUB_SHA:-$(git rev-parse --short HEAD)}" \
        --push .

    echo "==> Verifying pushed manifest"
    docker buildx imagetools inspect "${HARBOR_REGISTRY_URL}/${HARBOR_PROJECT}/${IMAGE_NAME}:latest" | tee /tmp/manifest.txt
    grep -q 'linux/amd64' /tmp/manifest.txt
    grep -q 'linux/arm64' /tmp/manifest.txt
    echo "✅ Multi-arch manifest contains both amd64 and arm64"

    echo "==> Done. Image pushed as :latest and :${GITHUB_SHA:-$(git rev-parse --short HEAD)}"
}

deploy() {
    local platform="${1:-vm}"
    local context="kubernetes-admin-${platform}@kubernetes-${platform}"

    echo "==> Deploying ${IMAGE_NAME} to ${platform}"
    kubectl config use-context "$context"

    VERSION="${GITHUB_SHA:-$(git rev-parse --short HEAD)}"
    export VERSION
    export HARBOR_REGISTRY_URL HARBOR_PROJECT IMAGE_NAME

    if kubectl get deployment "kuberush" &>/dev/null; then
        echo "==> Deployment exists, rolling restart"
        kubectl rollout restart deployment/kuberush
    else
        echo "==> Creating new deployment"
        envsubst < "${SCRIPT_DIR}/deployment.yml" | kubectl apply -f -
    fi

    echo "==> Waiting for rollout (300s timeout)"
    kubectl rollout status deployment/kuberush --timeout=300s
    echo "==> Deployed successfully"
}

undeploy() {
    local platform="${1:-vm}"
    local context="kubernetes-admin-${platform}@kubernetes-${platform}"

    echo "==> Undeploying kuberush from ${platform}"
    kubectl config use-context "$context"
    envsubst < "${SCRIPT_DIR}/deployment.yml" | kubectl delete -f - --ignore-not-found
    echo "==> Undeployed"
}

case "${1:-}" in
    build_and_push)
        build_and_push
        ;;
    deploy)
        deploy "${2:-vm}"
        ;;
    undeploy)
        undeploy "${2:-vm}"
        ;;
    *)
        usage
        ;;
esac
