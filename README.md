# Agentic workflows for Rancher

A Rancher UI extension and Kubernetes controller that catalog the **gh-aw agentic workflows** in your
GitHub repositories, run the ones that can be dispatched, and show run, cost and outcome stats.

- **Workflows run on GitHub Actions.** The cluster holds only the catalog, run records and rolled-up stats.
- Everything is a Kubernetes resource. Rancher lists, detail pages, events and RBAC work as usual.

Context: [rancher/dashboard#18949](https://github.com/rancher/dashboard/issues/18949) (agent automation epic).

## How it works

| Resource | Written by | What it is |
|---|---|---|
| `AgentRepository` | you | A GitHub repo plus a Secret holding a token. Synced every `syncInterval`. |
| `Agent` | controller | One per gh-aw workflow, i.e. `.github/workflows/X.md` that has a sibling `X.lock.yml`. Holds triggers, inputs and stats. |
| `AgentRun` | both | One GitHub Actions run. **Create one with `origin: rancher` to dispatch the workflow.** The controller imports runs started on GitHub with `origin: github`. |

All resources are `agentic.rancher.io/v1alpha1` and namespaced. The CRDs are in
[charts/agentic-controller/crds](charts/agentic-controller/crds).

**Where the stats come from:**
- Run state and duration come from the Actions API.
- AI credits and tokens come from the gh-aw `usage` artifact (`agent_usage.json`).
- Outputs come from the `safe-outputs-items` artifact: PRs, issues, comments and labels.
- PR merged/closed state is refreshed while the PR is open.

**What counts as a run:**
- Only runs where the gh-aw `agent` job actually ran.
- Gate-only runs are ignored: `skipped`, `action_required`, and runs where activation decided not to run the agent.

**Stats storage:**
- Each finished run is added once to `Agent.status.daily[]`. That holds one bucket per UTC day, kept for 90 days.
- `AgentRun` objects are pruned to `maxRunsPerAgent`, and older than 30 days. The stats survive pruning.

**Permissions** are plain Kubernetes RBAC:

| Who | Needs |
|---|---|
| View stats | `get`/`list` on `agents` and `agentruns` |
| Run an agent | `create` on `agentruns` |
| Register repositories | `create` on `agentrepositories`, `create` on `secrets` |

> **The GitHub token never leaves the cluster.** Only the controller reads the Secret. The UI only sees its name.

## Repository layout

| Path | What |
|---|---|
| [pkg/agentic](pkg/agentic) | UI extension: product, models, pages, charts, l10n |
| [controller](controller) | Go controller (controller-runtime) |
| [charts/agentic-controller](charts/agentic-controller) | Helm chart: CRDs, controller, RBAC |
| [examples](examples) | Sample `AgentRepository` |
| [test](test) | Unit tests for the UI stats helpers |

## Run it locally

Prerequisites:
- A Rancher (v2.10+) whose local cluster you can `kubectl` into.
- Node 24 and Yarn 1.
- Go 1.22 and Docker, for the controller.

### 1. Controller and CRDs

```sh
cd controller
make dev        # build, import the image into k3s, apply the chart, restart
make logs
```

The Makefile defaults to the local single-container Rancher (`RANCHER=rancher-16486`). Override
`RANCHER`, `IMG` and `ARCH` for another setup, or install the chart with Helm:

```sh
helm install agentic-controller charts/agentic-controller -n cattle-agentic-system --create-namespace
```

### 2. Add a repository

Either:
- Use the UI: **Agents → Repositories → Create**. Paste a token and the UI stores it in a new Secret.
- Or apply the example:

```sh
kubectl create namespace agentic-demo
kubectl -n agentic-demo create secret generic github-token --from-literal=token="$(gh auth token)"
kubectl apply -f examples/agentrepository-dashboard.yaml
```

**Token scopes:**
- **Read:** contents, actions, pull requests.
- **Write:** actions. Only needed to run agents.

### 3. UI extension

Dev server with the extension built in:

```sh
yarn install
API=https://<rancher-host> yarn dev       # https://127.0.0.1:8005/agentic/c/_/overview
```

Or build the package and load it into a running Rancher:

```sh
yarn build-pkg agentic
yarn serve-pkgs
```

Then in Rancher go to **Extensions → ⋮ → Developer load**, using the URL that `serve-pkgs` prints.

## Checks

```sh
yarn lint && yarn test                  # UI
cd controller && make vet test          # controller
```

## Known limits

- History before the first sync is limited to `backfillDays` (max 90).
- **A controller crash between counting a run and marking it `counted` can count it twice.**
- Runs started through gh-aw slash commands and labels are imported, but can only be triggered on GitHub.
