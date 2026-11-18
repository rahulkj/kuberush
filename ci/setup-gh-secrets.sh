#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

source "${ROOT_DIR}/.envrc"

REPO="rahulkj/${SITE_NAME}"

echo "==> Setting up GitHub secrets and variables for ${REPO}"

# Delete existing variables
echo "==> Cleaning existing variables..."
gh variable list --repo "$REPO" --json name -q '.[].name' | while read -r name; do
    gh variable delete "$name" --repo "$REPO" || true
done

# Delete existing secrets
echo "==> Cleaning existing secrets..."
gh secret list --repo "$REPO" --json name -q '.[].name' | while read -r name; do
    gh secret delete "$name" --repo "$REPO" || true
done

# Set variables
echo "==> Setting variables..."
gh variable set HARBOR_REGISTRY_URL --repo "$REPO" --body "$HARBOR_REGISTRY_URL"
gh variable set HARBOR_PROJECT --repo "$REPO" --body "$HARBOR_PROJECT"
gh variable set DOMAIN_NAME --repo "$REPO" --body "$DOMAIN_NAME"
gh variable set IMAGE_NAME --repo "$REPO" --body "$IMAGE_NAME"
gh variable set SITE_NAME --repo "$REPO" --body "$SITE_NAME"
gh variable set VM_PLATFORM --repo "$REPO" --body "vm"
gh variable set PI_PLATFORM --repo "$REPO" --body "pi"

# Set secrets
echo "==> Setting secrets..."
gh secret set DEPLOY_PAT --repo "$REPO" --body "$DEPLOY_PAT"
gh secret set HARBOR_USERNAME --repo "$REPO" --body "$HARBOR_USERNAME"
gh secret set HARBOR_PASSWORD --repo "$REPO" --body "$HARBOR_PASSWORD"

echo "==> Done! GitHub secrets and variables configured for ${REPO}"
