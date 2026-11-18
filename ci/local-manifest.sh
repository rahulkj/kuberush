#!/usr/bin/env bash
set -euo pipefail

DIR="${1:?usage: local-manifest.sh <deployment-manifests-dir> <git-sha>}"
SHA="${2:?usage: local-manifest.sh <deployment-manifests-dir> <git-sha>}"
cd "$(dirname "$0")/.."

set -a
source ./.envrc
set +a

export VERSION="${SHA}"
export HARBOR_REGISTRY_URL HARBOR_PROJECT IMAGE_NAME

mkdir -p "${DIR}/vm"
envsubst < ci/deployment.yml > "${DIR}/vm/${IMAGE_NAME}-deployment.yaml"
echo ">>> generated ${DIR}/vm/${IMAGE_NAME}-deployment.yaml for version ${SHA}"

mkdir -p "${DIR}/pi"
envsubst < ci/deployment.yml > "${DIR}/pi/${IMAGE_NAME}-deployment.yaml"
echo ">>> generated ${DIR}/pi/${IMAGE_NAME}-deployment.yaml for version ${SHA}"
