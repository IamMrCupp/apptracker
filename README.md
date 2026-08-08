# apptracker

A self-hosted **job applications & networking tracker** — a privacy-first tool
in the spirit of Arch & Bridge's tracker, but backed by a real database so your
data lives in *your* cluster and syncs across every device you log in from,
instead of being trapped in one browser.

- Single static Go binary with the web UI embedded (nothing external to serve)
- Pure-Go SQLite (`modernc.org/sqlite`) — no CGO, tiny distroless image
- Two modes: **Applications** and **Networking**
- JSON + CSV import/export (full-snapshot round-trip)
- Optional single-password auth (signed cookie sessions); runs open by default
- One file to back up: the SQLite database

## Data model

One `entries` table, discriminated by `kind` (`application` / `networking`):

`lane · entity (company/contact) · context (role/context) · date · channel ·
comp · follow_up · status · link · notes` plus `created_at` / `updated_at`.

Dropdown defaults (edit in `web/static/app.js`): **Lane** Priority/Active/Backburner/Archived,
**Channel** LinkedIn/Referral/Company site/Recruiter/Email/Event/Other,
**Status** Draft/Applied/Screening/Interviewing/Offer/Rejected/Ghosted.

## Configuration (env vars)

| Var               | Default           | Purpose                                             |
|-------------------|-------------------|-----------------------------------------------------|
| `PORT`            | `8080`            | Listen port                                         |
| `DB_PATH`         | `apptracker.db`   | SQLite file path                                    |
| `APP_PASSWORD`    | *(empty)*         | Empty = open access. Set to require login.          |
| `APP_SESSION_KEY` | *(random)*        | Stable HMAC key so sessions survive restarts.       |

## Run locally

```sh
# straight from source
go run ./cmd/apptracker            # http://localhost:8080

# or with Docker
docker compose up --build          # http://localhost:8080
```

## Test & vet

```sh
go vet ./...
go test ./...
```

## HTTP API

| Method | Path                         | Notes                              |
|--------|------------------------------|------------------------------------|
| GET    | `/healthz`                   | Liveness/readiness (public)        |
| GET    | `/api/session`               | `{authRequired, authed}`           |
| POST   | `/api/login` `/api/logout`   | Only meaningful when password set  |
| GET    | `/api/entries?kind=`         | List (filter by kind)              |
| POST   | `/api/entries`               | Create                             |
| PUT    | `/api/entries/{id}`          | Update                             |
| DELETE | `/api/entries/{id}`          | Delete                             |
| POST   | `/api/clear?kind=`           | Clear a mode (or all)              |
| GET    | `/api/export?format=json     | csv`  Download snapshot            |
| POST   | `/api/import?format=json     | csv`  Replace-all from snapshot    |

## Deploy to Kubernetes (Flux)

Manifests live in [`deploy/`](deploy/):

- `deploy/base/` — Kustomize base (Namespace, PVC, Deployment, Service, Ingress)
- `deploy/flux/` — Flux `Kustomization` + `GitRepository`, and a SOPS secret example

The Deployment is intentionally `replicas: 1` with `strategy: Recreate` and a
`ReadWriteOnce` PVC: SQLite is a single file with exactly one writer, so a
rolling update must never start a second pod against it.

```sh
# point the Flux Kustomization/GitRepository at your repo + registry,
# set the image and Ingress host, then commit — Flux reconciles the rest.
kubectl apply -k deploy/base            # (or let Flux do it)
```

Password: reference a Secret named `apptracker-auth` (key `password`). Don't
commit it in plaintext — encrypt with SOPS or use a SealedSecret (see
`deploy/flux/secret.sops.example.yaml`). Or drop the `APP_PASSWORD` env block to
run open inside the cluster.

## Backups

```sh
# consistent online backup of the single DB file
sqlite3 /data/apptracker.db ".backup /backup/apptracker-$(date +%F).db"
```

Or just use the UI's **Export JSON** to grab a portable snapshot.

## License

MIT — see [LICENSE](LICENSE).
