# Database-as-a-Service Backend

Backend API for a DBaaS platform. Provisions PostgreSQL and MongoDB instances via Kubernetes operators (**CloudNativePG**, **MongoDB Community Operator**) and exposes project management, query execution, schema, and table APIs.

---

## Quick Start (Local Development)

### Prerequisites

- **Go** 1.24+ ([go.dev/dl](https://go.dev/dl/))
- **Docker** and **Docker Compose** (for the meta-DB PostgreSQL container)
- **PostgreSQL** client (`psql`) — optional, used by setup for DB creation

### Option 1: One-command start

```bash
./start.sh
```

This will:

1. Check Go and Docker are available
2. Start PostgreSQL via Docker Compose
3. Create `.env` if missing (edit secrets before production)
4. Create the app database
5. Build and run the API

Server runs at `http://localhost:${PORT}` (default `8080`). Press **Ctrl+C** to stop.

### Option 2: Step-by-step

```bash
# 1. Start the meta-DB
docker compose up -d

# 2. Create and configure environment
make setup        # creates .env if missing
# Edit .env and set required variables (see table below)

# 3. Install Go dependencies and build
make deps
make build

# 4. Run the application
make run          # or: make build && ./bin/api
```

### Run tests

```bash
make test
```

---

## Required Environment Variables

Create a `.env` file in the project root (created automatically by `./start.sh` or `make setup`):

| Variable | Description | Example |
|----------|-------------|---------|
| `PORT` | HTTP server port | `8080` |
| `DB_HOST` | PostgreSQL host (meta-DB) | `localhost` |
| `DB_PORT` | PostgreSQL port | `5432` |
| `DB_USERNAME` | DB user | `postgres` |
| `DB_PASSWORD` | DB password | *(your password)* |
| `DB_DATABASE` | App database name | `dbaas` |
| `DB_ADMIN_USER` | Admin DB user | `postgres` |
| `DB_ADMIN_PASSWORD` | Admin DB password | *(your password)* |
| `ACCESS_TOKEN_SECRET` | JWT access token secret | *(long random string)* |
| `REFRESH_TOKEN_SECRET` | JWT refresh token secret | *(long random string)* |
| `GOOGLE_CLIENT_ID` | Google OAuth client ID | *(from Google Cloud Console)* |
| `GOOGLE_CLIENT_SECRET` | Google OAuth client secret | *(from Google Cloud Console)* |
| `GOOGLE_REDIRECT_URL` | Google OAuth redirect URL | `http://localhost:8080/api/v1/auth/google/callback` |

**Optional:**

| Variable | Description | Default |
|----------|-------------|---------|
| `DB_CRED_ENCRYPTION_KEY` | AES key for encrypting stored DB credentials (must be stable across restarts) | — |
| `DB_INSTANCES_NAMESPACE` | Namespace for operator-created DB instances | `default` |
| `KUBECONFIG` | Path to kubeconfig file (unset = in-cluster config) | — |

---

## Kubernetes (DB Instance Provisioning)

Project databases are provisioned via **CloudNativePG** (PostgreSQL) and **MongoDB Community Operator** (MongoDB) running inside an EKS cluster. The backend must have access to the same cluster.

- **Inside the cluster** (recommended): deploy the backend as a Kubernetes Deployment — it uses in-cluster config automatically.
- **Outside the cluster** (dev only): set `KUBECONFIG` to a file with cluster access. Note that in-cluster DNS (e.g. `db-xxx-rw.default.svc.cluster.local`) will not resolve from the host.

Without a valid K8s config, project creation (provisioning new DB instances) will fail; the rest of the API works if the meta-DB and env vars are set.

---

Deploy manifests live in `deploy/`:

| File | Purpose |
|------|---------|
| `deployment.yaml` | Backend Deployment (pulls from ECR) |
| `service.yaml` | ClusterIP service |
| `ingress.yaml` | ALB Ingress |
| `configmap.yaml` | Non-secret configuration |
| `secret.yaml` | Sensitive env vars |
| `rbac.yaml` | ServiceAccount + RBAC for operator interaction |
| `meta-db-cluster.yaml` | CloudNativePG Cluster for the meta-DB |

---

## Make Targets

| Target | Description |
|--------|-------------|
| `make help` | List all targets |
| `make setup` | Create `.env` and run initial setup |
| `make deps` | Download and tidy Go modules |
| `make build` | Build binary to `bin/api` |
| `make run` | Run with `go run` (dev) |
| `make test` | Run tests |
| `make docker-build` | Build Docker image |
| `make docker-push` | Push image to ECR |
| `make docker-down` | Stop Docker Compose services |
| `make deploy` | Apply Kubernetes manifests to current cluster |

---

## Production Checklist

1. **Secrets** — Set strong, unique values for `ACCESS_TOKEN_SECRET`, `REFRESH_TOKEN_SECRET`, and `DB_CRED_ENCRYPTION_KEY`; never commit them.
2. **Meta-Database** — In production the meta-DB runs as a CloudNativePG cluster inside EKS (see `deploy/meta-db-cluster.yaml`). Point `DB_HOST` to the CNPG service (e.g. `meta-db-cluster-rw.default.svc.cluster.local`).
3. **HTTPS** — Use the ALB Ingress with an ACM certificate for TLS termination.
4. **RBAC** — The backend ServiceAccount needs permission to create/delete `Cluster` (cnpg) and `MongoDBCommunity` resources and read Secrets in the DB instances namespace.
5. **Monitoring** — Add structured logging and metrics; use health endpoints (`/health`) for ALB target group health checks and K8s probes.
