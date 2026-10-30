# Beginner setup guide (GitHub + Kubernetes)

This guide is for **this project only**. Do the steps in order. Skip anything marked **Later**.

## Big picture (simple)

| Piece | What it is | Your project today |
|-------|------------|--------------------|
| **Local** | App on your PC | Docker (Postgres/Redis) + Go API + Next.js |
| **GitHub** | Code storage + automatic checks | Already pushing to GitHub |
| **GitHub Actions** | Robots that test your code on every push | Workflows in `.github/workflows/` |
| **Kubernetes (K8s)** | Runs containers on a cluster (server farm) | Manifests in `deploy/k8s/` — **optional until you deploy** |

You do **not** need Kubernetes to keep developing locally.

```
Your laptop (now)          GitHub (next)              Kubernetes (later)
─────────────────          ──────────────             ──────────────────
Docker Postgres/Redis  →   Actions: CI/CodeQL/SAST →  Staging / Production
Go API :8080 (`services/api`)  Builds Docker image   Runs API pods
Next.js :3000 (`apps/web`)
```

Cloud deploy (Render + Netlify, VPS, K8s): **[docs/deploy.md](deploy.md)**.

---

## Part A — What you should do this week (GitHub only)

### A1. Open Actions and watch a run

1. Browser: https://github.com/a2-nabil/Image-Infrastructure-Platform  
2. Click **Actions**  
3. Click the latest workflow run (from your recent push)  
4. Click each job (CI, CodeQL, SAST) and see green ✅ or red ❌  

If something is red, open the failed step log and fix that error (or ask for help with the error text).

### A2. Create Environments (names the workflows expect)

1. Repo → **Settings** → **Environments**  
2. **New environment** → name exactly: `staging` → Create  
3. **New environment** → name exactly: `production` → Create  
4. Open `production` → under **Deployment protection rules**:  
   - Enable **Required reviewers**  
   - Add yourself (and a friend later if you have one)  
   - Save  

> Note: On **private** repos, required reviewers need a paid GitHub plan. On **public** repos it works on free. If the option is greyed out, skip reviewers for now — Environments still help organize deploys.

You can leave secrets empty for now.

### A3. Branch protection on `main` (recommended)

1. **Settings** → **Branches** → **Add branch ruleset** (or classic Branch protection)  
2. Branch name: `main`  
3. Enable:  
   - Require a pull request before merging (good habit)  
   - Require status checks to pass  
4. After at least one successful Actions run, add required checks such as:  
   - `Secret scan (gitleaks)`  
   - `API (Go)`  
   - `Web (Next.js)`  

If you cannot find exact check names yet, push a tiny change, wait for Actions, then pick from the list GitHub shows.

### A4. Daily workflow as a beginner (no K8s)

```bash
# 1) Start local deps (Docker Desktop must be running)
docker compose -f infra/docker/docker-compose.yml up -d

# 2) Root .env (never commit real secrets)
cp .env.example .env
# Set JWT_SECRET, GOOGLE_CLIENT_ID, optional GALLERY_SUPER_ADMIN_EMAIL / AWS

# 3) Start API
cd services/api
go run ./cmd/server

# 4) Other terminal: start UI
cd apps/web
cp .env.local.example .env.local
# Set NEXT_PUBLIC_GOOGLE_CLIENT_ID, IMAGE_API_ORIGIN, GEMINI_API_KEY
npm install && npm run dev

# 5) Browser
# http://localhost:3000
# Converter / Identify: everyone
# Dashboard: signed-in tools library
# Gallery: super admin + allowlisted emails only (⋮ menu)

# 6) After code changes
bash scripts/ci-local.sh
bash scripts/smoke-test.sh

# 7) Save to GitHub (do not add .env or .env.local)
git add .
git commit -m "Describe your change"
git push
```

Security overview: [docs/security.md](security.md).

Then check **Actions** again.

---

## Part B — What Kubernetes is (for this repo)

Think of Kubernetes as:

1. You build a **Docker image** of the Go API  
2. You tell the cluster: “run 2 copies of this image” (`deployment.yaml`)  
3. The cluster gives them a stable name/port (`service.yaml`)  
4. If CPU is high, add more copies (`hpa.yaml`)  
5. Config goes in ConfigMap; passwords go in Secrets  

Your files:

| File | Meaning |
|------|---------|
| `deploy/k8s/deployment.yaml` | How to run `image-api` pods |
| `deploy/k8s/service.yaml` | How other apps reach the API |
| `deploy/k8s/configmap.yaml` | Non-secret settings (`APP_ENV`, region, …) |
| `deploy/k8s/secret.yaml.example` | Template for passwords/keys (**never commit real secrets**) |
| `deploy/k8s/hpa.yaml` | Auto scale replicas |
| `deploy/k8s/migrate-job.yaml.example` | One-time DB migration job |

You already have **kind** (Kubernetes in Docker) from earlier. That is enough to **learn**, not required for local Go development.

---

## Part C — Learn K8s later (optional lab on your PC)

Only when you want to practice deploy (not required for coding features).

### C1. Prerequisites

- Docker Desktop running  
- `kubectl` installed  
- A cluster: you already used **kind** (`kind-control-plane`)

Check:

```bash
docker ps
kubectl cluster-info
kubectl get nodes
```

If `kubectl` fails, install: https://kubernetes.io/docs/tasks/tools/

### C2. Build the API image locally

```bash
cd services/api
docker build -t image-api:local .
```

### C3. Load image into kind (so the cluster can see it)

```bash
kind load docker-image image-api:local
```

### C4. Create a secret from the example (local practice only)

```bash
# Copy example and edit placeholders — use fake values for learning
cp deploy/k8s/secret.yaml.example deploy/k8s/secret.yaml
# Edit secret.yaml in an editor, then:
kubectl apply -f deploy/k8s/secret.yaml
kubectl apply -f deploy/k8s/configmap.yaml
```

Edit `deployment.yaml` temporarily to use `image: image-api:local` and `imagePullPolicy: IfNotPresent`, then:

```bash
kubectl apply -f deploy/k8s/deployment.yaml
kubectl apply -f deploy/k8s/service.yaml
kubectl get pods
kubectl logs -l app=image-api
```

### C5. Clean up practice

```bash
kubectl delete -f deploy/k8s/deployment.yaml --ignore-not-found
kubectl delete -f deploy/k8s/service.yaml --ignore-not-found
# Keep or delete secret/configmap as you prefer
```

**Do not** put real AWS keys into git. Keep `secret.yaml` gitignored.

---

## Part D — Staging / Production on GitHub (when you have a real cluster)

Skip until you have a cloud cluster (EKS, GKE, AKS) or a shared staging server.

Then:

1. Get a `kubeconfig` file from your cloud provider  
2. Encode it:

```bash
base64 -w0 < /path/to/kubeconfig
```

3. GitHub → **Settings** → **Secrets and variables** → **Actions**  
   - Secret `STAGING_KUBECONFIG` = that base64 string  
   - Secret `PRODUCTION_KUBECONFIG` = prod base64 string  
4. **Variables**:  
   - `STAGING_BASE_URL` = `https://your-staging-api.example.com`  
   - `PRODUCTION_BASE_URL` = `https://your-api.example.com`  

Until those exist, Staging/Production workflows will **build the image** but **skip cluster apply** — that is OK.

---

## What NOT to worry about yet

- DAST / ZAP scans without a public staging URL  
- Production tags (`v0.1.0`) before staging works  
- IRSA, Ingress, TLS certificates  
- Full multi-environment AWS accounts  

## Security habits (do these early)

- Never commit `.env`, `.env.local`, AWS keys, JWT secrets, or Gemini keys  
- Gallery is private: only the super admin email + allowlist — API returns `403 GALLERY_FORBIDDEN` otherwise  
- After login, non-allowlisted users still get Converter / Identify / Dashboard — not Gallery  
- Read [docs/security.md](security.md) before sharing the app with others  

---

## Checklist

**Now**

- [ ] Actions tab shows CI green after push  
- [ ] Environments `staging` and `production` created  
- [ ] Local: Docker + API + UI still work  
- [ ] Use `bash scripts/ci-local.sh` (no `make` needed on Windows)  
- [ ] Confirmed `.env` / `.env.local` are gitignored and not staged

**Later**

- [ ] Branch protection on `main`  
- [ ] Optional kind lab (Part C)  
- [ ] Real cluster + kubeconfig secrets (Part D)  

---

## If you get stuck

Copy-paste into chat:

1. What step you were on (A1, A2, …)  
2. The exact error text  
3. Screenshot or Actions job name that failed  
