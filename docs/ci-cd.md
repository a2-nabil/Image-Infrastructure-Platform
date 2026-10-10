# CI/CD, security gates, and environments

Pipeline stages for this repository (GitHub Actions):

```
pre-commit → CI → CodeQL + SAST → Staging → DAST → Production
```

| Stage | Workflow | Purpose |
|-------|----------|---------|
| Pre-commit | `.pre-commit-config.yaml` / `scripts/git-hooks/pre-commit` | Block secrets, format, lint before commit |
| CI (must) | `.github/workflows/ci.yml` | gitleaks, go test/vet/build, Next lint/build, Docker build |
| CodeQL | `.github/workflows/codeql.yml` | Semantic SAST for Go + JS/TS |
| SAST | `.github/workflows/sast.yml` | GoSec, Semgrep (OWASP), npm audit |
| Staging | `.github/workflows/staging.yml` | Build/push GHCR image, optional kubectl apply |
| DAST | `.github/workflows/dast.yml` | OWASP ZAP baseline against staging URL |
| Production (must) | `.github/workflows/production.yml` | Tag/`workflow_dispatch` + Environment approval + preflight |

## Install pre-commit hooks

```bash
# Preferred (full hook set)
pip install pre-commit
make install-hooks
pre-commit run --all-files

# Or fallback git hook only
bash scripts/install-hooks.sh
```

## GitHub setup (required)

### Branch protection (`main`)

Require status checks before merge:

- `Secret scan (gitleaks)`
- `Image API (Go)`
- `Object Identifier (Next.js)`
- `Analyze (go)` / `Analyze (javascript-typescript)` (CodeQL)
- `GoSec (image-api)` / `Semgrep` (SAST)

### Environments

Create Environments in repo **Settings → Environments**:

| Environment | Protection |
|-------------|------------|
| `staging` | Optional reviewers |
| `production` | **Required reviewers** + wait timer recommended |

### Secrets

| Secret | Used by |
|--------|---------|
| `STAGING_KUBECONFIG` | Staging apply (base64 kubeconfig) |
| `PRODUCTION_KUBECONFIG` | Production apply (base64 kubeconfig) |

### Variables

| Variable | Example |
|----------|---------|
| `STAGING_BASE_URL` | `https://api.staging.example.com` |
| `PRODUCTION_BASE_URL` | `https://api.example.com` |

## Production must-checks (enforced in code + workflow)

Runtime (`APP_ENV=staging|production`):

- `JWT_SECRET` required
- `CORS_ALLOWED_ORIGINS` required (comma-separated)
- `POSTGRES_SSLMODE` must not be `disable` (defaults to `require`)
- `MIGRATE_ON_START` defaults to `false` (use migrate Job)

Manifests (`production.yml` preflight):

- ConfigMap `APP_ENV: production`
- ConfigMap `POSTGRES_SSLMODE: require`
- No committed `deploy/k8s/secret.yaml`
- Dockerfile runs as non-root `appuser`

Migrate Job example: `deploy/k8s/migrate-job.yaml.example`.

## Promote flow

1. Merge to `develop`/`staging` → CI + SAST + Staging image.
2. DAST runs after successful Staging (needs `STAGING_BASE_URL`).
3. Cut a release tag `vX.Y.Z` or run **Production** workflow with confirmation `deploy-production`.
4. Production Environment requires human approval.

## Local commands

```bash
make install-hooks
make test
make ci-local
```
