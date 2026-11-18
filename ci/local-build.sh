#!/usr/bin/env bash
# Local replication of .github/workflows/docker-image.yml (build + push step).
# After a successful push, also regenerates the deployment manifest and commits
# it to the shared deployment-manifests repo.
# Usage: ci/local-build.sh <git-sha>
set -euo pipefail

SHA="${1:?usage: local-build.sh <git-sha>}"
DEPLOYMENT_DIR="${DEPLOYMENT_DIR:-/Users/rjain/Documents/github/rahulkj/deployment-manifests}"
cd "$(dirname "$0")/.."

# Resolve secrets/config from the keyring-backed .envrc (values never printed).
set -a
source ./.envrc
set +a

REGISTRY="${HARBOR_REGISTRY_URL}/${HARBOR_PROJECT}/${IMAGE_NAME}"

# CI note: `docker login` gets a 403 from Harbor/Cloudflare on its daemon
# preflight ping; a hand-built auth config is what BuildKit's push needs.
AUTH_DIR="$(mktemp -d)"
trap 'rm -rf "${AUTH_DIR}"' EXIT
cat > "${AUTH_DIR}/config.json" <<EOF
{
  "auths": {
    "${HARBOR_REGISTRY_URL}": {
      "auth": "$(printf '%s' "${HARBOR_USERNAME}:${HARBOR_PASSWORD}" | base64)"
    }
  }
}
EOF
export DOCKER_CONFIG="${AUTH_DIR}"
# Overriding DOCKER_CONFIG also loses the active context (e.g. colima's
# socket), which would fall back to the nonexistent /var/run/docker.sock.
# Point at the colima socket explicitly when no daemon is on the default path.
if [ -S "${HOME}/.colima/default/docker.sock" ] && [ ! -S /var/run/docker.sock ]; then
  export DOCKER_HOST="unix://${HOME}/.colima/default/docker.sock"
fi

# Pointing DOCKER_CONFIG at the temp dir hides ~/.docker/cli-plugins, so the
# buildx plugin would vanish. Mirror it into the temp config.
if [ -d "${HOME}/.docker/cli-plugins" ]; then
  mkdir -p "${AUTH_DIR}/cli-plugins"
  cp -n "${HOME}"/.docker/cli-plugins/* "${AUTH_DIR}/cli-plugins/" 2>/dev/null || true
fi

echo ">>> buildx multi-arch build+push: ${REGISTRY}:{latest,${SHA}}"
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --push \
  --provenance=false \
  -t "${REGISTRY}:latest" \
  -t "${REGISTRY}:${SHA}" \
  .

echo ">>> verifying manifest"
docker buildx imagetools inspect "${REGISTRY}:${SHA}" | tee "${AUTH_DIR}/manifest.txt"
grep -q 'linux/amd64' "${AUTH_DIR}/manifest.txt"
grep -q 'linux/arm64' "${AUTH_DIR}/manifest.txt"
echo "OK: multi-arch manifest contains both amd64 and arm64"

echo "==> Generating deployment manifest"
bash ci/local-manifest.sh "${DEPLOYMENT_DIR}" "${SHA}"

echo "==> Committing updated deployment manifest"
cd "${DEPLOYMENT_DIR}"
git add -A
git commit -m "Update ${IMAGE_NAME} deployments to version ${SHA}" || echo "==> No changes to commit in ${DEPLOYMENT_DIR}"
