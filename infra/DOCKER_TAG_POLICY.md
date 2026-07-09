# Docker Image Tagging Policy

## Overview

This document explains our Docker image tagging strategy and why we don't use `latest` tags.

## Problem with `latest` Tag

**Re-pulling a mutable tag is unreliable and untraceable.**

- `docker-compose up -d` won't recreate a container whose image tag hasn't
  changed, and cached `latest` layers can mask a failed pull
  (this bit us hard on the old Yandex Container Optimized Image host)
- You can't tell which code version is running, and rollback has no target
- Unique tags per deployment guarantee updates and give rollbacks a name

## Our Solution: Commit SHA Tags

We use Git commit SHA (short form) as unique identifiers for each deployment.

### Benefits

1. **Guaranteed Updates**: Each deployment has a unique tag, ensuring containers always update
2. **Traceability**: Easy to track which code version is deployed
3. **Reproducibility**: Can easily redeploy any previous version
4. **Rollback Safety**: Clear version history for rollbacks

### Tag Format

Registry is currently `cr.yandex` — `TODO(P6.4)`: moving to ghcr.io.

```
Frontend: cr.yandex/<registry-id>/openmentor-frontend:<commit-sha>
Backend:  cr.yandex/<registry-id>/openmentor-backend:<commit-sha>

Example:
- cr.yandex/crpXXXXXX/openmentor-frontend:abc123f
- cr.yandex/crpXXXXXX/openmentor-backend:abc123f
```

## Implementation

### Local Deployment (`deploy.sh`)

```bash
# Automatically uses the monorepo's git commit SHA, per service:
FRONTEND_IMAGE_TAG=$(git rev-parse --short HEAD)   # example: abc123f
BACKEND_IMAGE_TAG=$(git rev-parse --short HEAD)

# deploy-dev.sh uses the same scheme with a dev- prefix (local images,
# never pushed): openmentor-frontend:dev-abc123f
```

Services not being deployed keep their currently deployed tag (fetched from
the VM's `.env`), so a `./deploy.sh frontend` never disturbs the backend.

### GitHub Actions Workflow

```yaml
# Uses full commit SHA
IMAGE_TAG: ${{ github.sha }}

# Example: abc123f456d789e012f345a678b901c234d567e
```

### Docker Compose

```yaml
services:
  frontend:
    image: cr.yandex/${YANDEX_REGISTRY_ID}/openmentor-frontend:${FRONTEND_IMAGE_TAG:-${IMAGE_TAG}}
    # No fallback to 'latest' — per-service tags, written to .env by the
    # deploy scripts (IMAGE_TAG is only a legacy fallback)
```

## Deployment Process

1. **Build**: Images are built with commit SHA tag
2. **Push**: Only the commit SHA tag is pushed (no `latest`)
3. **Deploy**: VM pulls image by specific SHA
4. **Update**: Container is recreated with new image

## Rollback Process

To rollback, specify a previous commit SHA:

```bash
# Rollback to specific version
./rollback.sh abc123f

# Or interactively
./rollback.sh
# Enter image tag to rollback to (commit SHA): abc123f
```

## Best Practices

### ✅ DO

- Always commit changes before deploying
- Use `./deploy.sh` for local deployments (auto-generates SHA)
- Use GitHub Actions for automated deployments
- Keep track of deployed SHAs for rollback purposes
- Reference specific commit SHAs in rollback scripts

### ❌ DON'T

- Don't use `latest` tag anywhere
- Don't manually tag images without a unique identifier
- Don't skip commits before deploying
- Don't assume containers auto-update with the same tag

## Troubleshooting

### Container not updating after deployment

**Problem**: Deployed new code but container still runs old version

**Cause**: Likely used the same tag (e.g., `latest`)

**Solution**:
```bash
# Check current running tag
docker ps --format "{{.Image}}"

# Deploy with new unique tag
./deploy.sh
```

### Can't find previous version for rollback

**Problem**: Don't know which SHA was previously deployed

**Solution**:
```bash
# Check git log for recent deployments
git log --oneline -10

# Check container image history
docker images | grep openmentor

# Check the previous tags saved during deployment
grep IMAGE_TAG /opt/openmentor/infra/.env.backup  # On VM
```

## Migration Notes

If migrating from `latest` tags:

1. Deploy once with commit SHA to establish baseline
2. Note the commit SHA in your deployment logs
3. All future deployments will use unique SHAs
4. Old `latest` images can be cleaned up:
   ```bash
   docker image rm cr.yandex/<registry>/openmentor-frontend:latest
   docker image rm cr.yandex/<registry>/openmentor-backend:latest
   ```

## References

- [Yandex Cloud Container Registry Documentation](https://cloud.yandex.com/en/docs/container-registry/)
- [Docker Image Tag Best Practices](https://docs.docker.com/develop/dev-best-practices/)
- [Semantic Versioning](https://semver.org/)

## Related Files

- `deploy.sh` - Local deployment script
- `rollback.sh` - Rollback script
- `.github/workflows/deploy.yml` - GitHub Actions workflow
- `docker-compose.yml` - Production compose configuration
- `DEPLOYMENT.md` - Full deployment guide
