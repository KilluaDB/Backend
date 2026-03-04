# Database-as-a-Service Backend

Backend API for a DBaaS platform. Provisions PostgreSQL and MongoDB instances via Kubernetes operators (CloudNativePG, MongoDB Community Operator) and exposes project management, query execution, schema, and table APIs.

---

## How to start the project professionally

### Prerequisites

- **Go** 1.24+ ([go.dev/dl](https://go.dev/dl/))
- **Docker** and **Docker Compose** (for Postgres and Redis)
- **PostgreSQL** client (`psql`) optional — used by setup for DB creation

### Option 1: One-command start (recommended for local dev)

From the project root:

```bash
./start.sh
```

This will:

1. Check Go and Docker
2. Start Postgres and Redis via Docker Compose
3. Create `.env` if missing (you must edit secrets before production)
4. Create the app database and run migrations
5. Build and run the API

Server runs at `http://localhost:${PORT}` (default `8080`). Use **Ctrl+C** to stop the server and containers.

---

### Option 2: Step-by-step (professional / CI-friendly)

**1. Start infrastructure**

```bash
docker compose up -d
```

**2. Create and configure environment**

```bash
make setup
# Edit .env and set required variables (see below)
```

**3. Install Go dependencies and build**

```bash
make deps
make build
```

**4. Run the application**

```bash
make run
# Or for a production-style run: make start
```

**5. (Optional) Run tests**

```bash
make test
```

---

### Required environment variables

Create a `.env` file in the project root. The following are **required** at startup:

| Variable | Description | Example |
|----------|-------------|---------|
| `PORT` | HTTP server port | `8080` |
| `DB_HOST` | PostgreSQL host (meta-DB for the app) | `localhost` |
| `DB_PORT` | PostgreSQL port | `5432` |
| `DB_USERNAME` | DB user | `postgres` |
| `DB_PASSWORD` | DB password | (your password) |
| `DB_DATABASE` | App database name | `dbaas` |
| `DB_ADMIN_USER` | Admin DB user | `postgres` |
| `DB_ADMIN_PASSWORD` | Admin DB password | (your password) |
| `ACCESS_TOKEN_SECRET` | JWT access token secret | (long random string) |
| `REFRESH_TOKEN_SECRET` | JWT refresh token secret | (long random string) |
| `GOOGLE_CLIENT_ID` | Google OAuth client ID | (from Google Cloud Console) |
| `GOOGLE_CLIENT_SECRET` | Google OAuth client secret | (from Google Cloud Console) |
| `GOOGLE_REDIRECT_URL` | Google OAuth redirect URL | `http://localhost:8080/api/v1/auth/google/callback` |

Optional for development:

- `DB_CRED_ENCRYPTION_KEY` — AES key for encrypting stored DB credentials (must be stable across restarts).

---

### Kubernetes (DB instance provisioning)

Project and DB instances are provisioned via **CloudNativePG** (PostgreSQL) and **MongoDB Community Operator** (MongoDB). The backend needs access to the cluster where these operators run.

**Optional environment variables:**

| Variable | Description | Default |
|---------|-------------|---------|
| `DB_INSTANCES_NAMESPACE` | Namespace for operator-created DB instances | `default` |
| `KUBECONFIG` | Path to kubeconfig file | Unset = in-cluster config |

**To start professionally with K8s:**

1. Install **CloudNativePG** and **MongoDB Community Operator** in your cluster.
2. Configure RBAC so the backend ServiceAccount can create/delete `Cluster` (postgresql.cnpg.io) and `MongoDBCommunity` resources and read Secrets in the DB instances namespace.
3. Run the backend either:
   - **Inside the cluster** (Deployment with in-cluster config), or  
   - **Outside the cluster** with `KUBECONFIG` set to a file that has access to the cluster.
4. Ensure the backend can reach the DB Services (e.g. same cluster DNS, or exposed via NodePort/LoadBalancer).

Without a valid K8s config, project creation (provisioning new DB instances) will fail; the rest of the API can still run if the meta-DB and env vars are set.

---

### Run Kubernetes locally with k3s (k3d)

To test DB provisioning (CloudNativePG and MongoDB operators) on your machine, this project uses a local **k3s** cluster provided by **k3d**.

**Prerequisites**

- `kubectl`
- `helm`
- `docker`
- `k3d` ([installation guide](https://k3d.io/))

**Step 1 – Create local k3s cluster and install operators**

From the project root:

```bash
./scripts/k3s-local-setup.sh
```

This script will:

1. Ensure `kubectl`, `helm`, `docker`, and `k3d` are available.
2. Create (or reuse) a `k3d` cluster named `dbaas-local` with context `k3d-dbaas-local`.
3. Install **CloudNativePG** into the `cnpg-system` namespace.
4. Install **cert-manager** if it is not present (required by MongoDB operator).
5. Install **MongoDB Community Operator** into the `mongodb-operator` namespace.

**Step 2 – Run the backend inside the k3s cluster**

From the project root:

```bash
./start.sh --k8s
```

This will:

1. Start the meta-DB (Postgres and Redis) in Docker via `docker compose`.
2. Ensure `.env` exists and the app database is created.
3. Build the backend Docker image `backend-api:latest`.
4. Import the image into the `k3d` cluster (when the current context is `k3d-dbaas-local`).
5. Apply `deploy/rbac.yaml`, `deploy/deployment.yaml`, and `deploy/service.yaml`.
6. Create/update the `backend-config` ConfigMap and `backend-secrets` Secret in the cluster.
7. Port-forward the backend Service to a local port (default `8081`), so you can access the API at `http://localhost:8081`.

Inside the cluster, the backend connects to the meta PostgreSQL instance running in Docker using the special hostname `host.k3d.internal`, which k3d exposes from pods to the Docker host.

**Using the backend with a local cluster**

- **Option A — Backend in cluster (recommended):** Run the backend as a Deployment in the same cluster so it can reach DB instances via in-cluster DNS. **One command:** `./start.sh --k8s` (or `./start.sh -k`). This starts meta-DB (Postgres/Redis) in Docker, builds the backend image, applies the deploy manifests, creates the Secret from `.env`, and port-forwards the API to `http://localhost:8080`. Prerequisites: cluster running (e.g. `./scripts/k3s-local-setup.sh`), `kubectl` and `docker` available. For manual steps see **[deploy/README.md](deploy/README.md)**.
- **Option B — Backend on host:** Run the backend on your host with `KUBECONFIG` set. DB instances run inside the cluster; in-cluster DNS (e.g. `db-xxx-rw.default.svc.cluster.local`) does not resolve from the host. You would need to port-forward each project’s DB Service and configure the app to use that external address (not implemented by default).

---

### Make targets

| Target | Description |
|--------|-------------|
| `make help` | List all targets |
| `make setup` | Create `.env` and run initial setup |
| `make deps` | Download and tidy Go modules |
| `make build` | Build binary to `bin/api` |
| `make run` | Run with `go run` (dev) |
| `make start` | Build then run `bin/api` |
| `make test` | Run tests |
| `make docker-down` | Stop Docker Compose services |
| `make k8s-local` | Start local K8s (minikube) and install CloudNativePG + MongoDB operators |

---

### Production checklist

1. **Secrets** — Set strong, unique values for `ACCESS_TOKEN_SECRET`, `REFRESH_TOKEN_SECRET`, and `DB_CRED_ENCRYPTION_KEY`; never commit them.
2. **Database** — Use a managed Postgres instance or a dedicated server; restrict network access.
3. **HTTPS** — Put the API behind a reverse proxy (e.g. nginx, Traefik) with TLS.
4. **Kubernetes** — Run the backend in the cluster when using operator-based provisioning; use a dedicated namespace and least-privilege RBAC.
5. **Logging and monitoring** — Add structured logging and metrics; consider health endpoints for load balancers and K8s probes.
