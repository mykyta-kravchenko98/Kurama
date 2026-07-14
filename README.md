# Kurama

Kurama is a Kubernetes operator for declarative, repeatable API traffic
generation. It is intended to create controlled workload and observability
data in a cluster without putting load-generation logic into the target
application's Helm chart.

The name comes from Kurama, the Nine-Tailed Fox from Naruto: a persistent
inner source of power that can produce measured bursts when needed.

## Design boundary

Kurama has two deliberately separate responsibilities:

```text
TrafficScenario custom resource
        |
Kurama controller -- creates and reconciles a runner Deployment
        |
Kurama runner -- sends requests and reports workload metrics
        |
Target API
```

The controller does not send API requests in its reconcile loop. The runner
is an ordinary workload Pod, so a failed or busy traffic generator cannot
block reconciliation of the custom resource.

The first supported protocol will be HTTP/HTTPS. A scenario will declare a
target, request operations, a traffic profile and references to credentials;
secret values are never stored in the custom resource itself.

## Incremental implementation plan

Each phase must be demonstrable in the local kind cluster before the next one
is started. This keeps the operator useful throughout development and avoids
turning it into an in-house replacement for a complete load-testing platform.

### Phase 0 — repository foundation (current)

- Go module, minimal manager entrypoint, lint configuration and CI.
- CI runs `go vet`, golangci-lint, tests and a Linux/amd64 build.
- No Kubernetes resources, image publishing or GitOps changes yet.

### Phase 1 — scenario lifecycle

- Define the namespaced `TrafficScenario` CRD and status conditions.
- Implement a controller that validates the smallest fixed HTTP scenario and
  reconciles one labelled runner `Deployment` and its configuration.
- Add RBAC, CRD generation, fake-client reconciler tests and a container image.
- Demonstrate create, update, suspend and delete in kind.

### Phase 2 — useful ShortUrl workload

- Implement the runner with HTTP `GET` and `POST`, timeouts and bounded
  concurrency.
- Add a local bounded in-memory value pool: successful `POST /shorten`
  extracts hashes and `GET` operations use them.
- Support a controlled percentage of random hashes to produce expected 404s.
- Start with a fixed, explicitly configured request rate.

### Phase 3 — observable, repeatable traffic

- Add constant, uniform, Poisson and burst traffic profiles.
- Make random generation reproducible with a scenario seed.
- Export runner metrics: attempts, response status, latency, active workers,
  pool size/hits/misses and configured versus achieved rate.
- Propagate a scenario identifier in request headers and runner logs for
  correlation in Loki, Tempo and Prometheus.

### Phase 4 — reusable HTTP scenarios

- Add weighted operations, restricted body/path/header templates and JSON
  response extraction.
- Add `none`, bearer-token, API-key and basic authentication, always through
  Kubernetes `Secret` references.
- Add OAuth2 client-credentials only when a real target requires it.
- Keep the CRD protocol-neutral enough to add a separate executor later; do
  not claim gRPC, Kafka or WebSocket support before implementing it.

### Phase 5 — scale and hardening

- Add an optional shared state store only when multiple runner replicas or
  restart-persistent pools are required. Do not read a target application's
  database directly.
- Add target allow-lists, NetworkPolicy examples, resource limits and
  admission validation.
- Add GitOps publishing and a documented ShortUrl scenario.

## Non-goals for the first release

- Replacing k6, Gatling or a general-purpose workflow engine.
- Executing arbitrary scripts or templates from a custom resource.
- Storing API credentials in Git or in `TrafficScenario.spec`.
- Supporting every transport before HTTP scenarios are reliable.

## Development

```bash
go vet ./...
go test ./...
go build ./cmd/manager
```

The CI workflow targets Linux/amd64 because the initial deployment target is
the local kind cluster maintained by `shorturl-gitops`.
