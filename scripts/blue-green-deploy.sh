#!/bin/bash
set -euo pipefail

# Blue-Green Deployment Script for VigilAgent
# Usage: ./blue-green-deploy.sh <new-image-tag>

NAMESPACE="vigilagent"
APP_NAME="vigilagent-api"
BLUE_DEPLOYMENT="${APP_NAME}-blue"
GREEN_DEPLOYMENT="${APP_NAME}-green"
SERVICE="${APP_NAME}"
NEW_IMAGE_TAG="${1:?Usage: $0 <image-tag>}"

echo "=== Blue-Green Deployment ==="
echo "New image tag: ${NEW_IMAGE_TAG}"

# Determine current active color
CURRENT_COLOR=$(kubectl get service ${SERVICE} -n ${NAMESPACE} -o jsonpath='{.spec.selector.deployment}')
echo "Current active: ${CURRENT_COLOR}"

if [ "${CURRENT_COLOR}" == "${BLUE_DEPLOYMENT}" ]; then
  NEW_COLOR="green"
  NEW_DEPLOYMENT="${GREEN_DEPLOYMENT}"
else
  NEW_COLOR="blue"
  NEW_DEPLOYMENT="${BLUE_DEPLOYMENT}"
fi

echo "Deploying to: ${NEW_COLOR}"

# Update the new deployment image
kubectl set image deployment/${NEW_DEPLOYMENT} \
  api=vigilagent/api:${NEW_IMAGE_TAG} \
  -n ${NAMESPACE}

# Wait for rollout
echo "Waiting for rollout..."
kubectl rollout status deployment/${NEW_DEPLOYMENT} -n ${NAMESPACE} --timeout=300s

# Verify new deployment is healthy
echo "Health check..."
kubectl exec -n ${NAMESPACE} deployment/${NEW_DEPLOYMENT} -- \
  wget -qO- http://localhost:8080/api/v1/health || {
    echo "Health check failed! Aborting switch."
    exit 1
  }

# Switch traffic to new deployment
echo "Switching traffic to ${NEW_COLOR}..."
kubectl patch service ${SERVICE} -n ${NAMESPACE} -p \
  "{\"spec\":{\"selector\":{\"deployment\":\"${NEW_DEPLOYMENT}\"}}}"

echo "=== Deployment complete ==="
echo "Active: ${NEW_COLOR} (${NEW_IMAGE_TAG})"
echo ""
echo "To rollback: kubectl patch service ${SERVICE} -n ${NAMESPACE} -p '{\"spec\":{\"selector\":{\"deployment\":\"${CURRENT_COLOR}\"}}}'"
