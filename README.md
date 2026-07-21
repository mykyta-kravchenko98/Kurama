# Kurama

Kurama is a Kubernetes operator for declarative, repeatable API traffic
generation. It creates controlled workload and observability data in a cluster
without putting load-generation logic into the target application's Helm
chart.

The name comes from Kurama, the Nine-Tailed Fox from Naruto: a persistent
inner source of power that can produce measured bursts when needed.

## Design boundary

Kurama has two deliberately separate responsibilities:

```text
TrafficScenario custom resource
        |
Kurama controller -- creates and reconciles a runner Deployment
        |
Kurama runner -- schedules requests and records workload results
        |
Target API
```

The controller does not send API requests in its reconcile loop. The runner
is an ordinary workload Pod, so a failed or busy traffic generator cannot
block reconciliation of the custom resource.

The supported protocol is currently HTTP/HTTPS. A scenario declares a target,
weighted request operations, a fixed traffic rate and bounded value stores.
Authentication and Secret references are planned; secret values will never be
stored directly in the custom resource.

## Implementation status and roadmap

Each phase must be demonstrable in the local kind cluster before the next one
is started. This keeps the operator useful throughout development and avoids
turning it into an in-house replacement for a complete load-testing platform.

### Phase 0 — repository foundation (complete)

- Set up the Go module, manager entrypoint, lint configuration and CI.
- Run `go vet`, golangci-lint, tests and Linux/amd64 builds in CI.
- Build and publish the shared manager/runner image to ECR through GitHub
  Actions OIDC.
- Automatically synchronize the CRD and immutable image digest to
  `shorturl-gitops`.

### Phase 1 — scenario lifecycle (complete)

- Define the namespaced `TrafficScenario` CRD with Ready, Suspended and Failed
  phases.
- Reconcile one labelled runner `Deployment` and a ConfigMap-backed
  `scenario.json`.
- Roll out the runner when scenario configuration changes.
- Add RBAC, fake-client reconciler tests and the shared manager/runner image.
- Demonstrate create, update, suspend and resume in the local kind cluster.

### Phase 2 — useful ShortUrl workload (complete)

- Strictly decode and validate the runner configuration with bounded config,
  request, response and captured-value sizes.
- Execute HTTP `GET` and `POST` operations with timeouts, expected status
  validation and redirects disabled.
- Support restricted path/body templates with `randomUUID`, `randomBase62`
  and value-store variables.
- Extract string values from JSON responses with JSON Pointer.
- Store captured values in independent, thread-safe, bounded in-memory pools
  with FIFO eviction and random selection.
- Schedule a fixed requests-per-minute rate and select operations with weighted
  randomness.
- Re-select an operation when its value store is temporarily empty without
  sending an invalid request.
- Shut down gracefully on SIGINT and SIGTERM.
- Validate the complete flow in kind with the ShortUrl scenario:
  - create: `POST /api/v1/data/shorten` returns 200 and captures a hash;
  - resolve-valid: `GET /api/v1/<captured-hash>` returns 308;
  - resolve-invalid: `GET /api/v1/<random-hash>` returns 404.
- Confirm that structured runner logs reach Loki and are queryable in Grafana.

## Observability integration

- Kurama emits structured request results that are collected by Loki and can
  be queried in Grafana.
- Export Kurama metrics for attempts, completed requests, response status,
  latency, scheduler slots, configured versus achieved rate, pool size,
  misses and evictions.
- Propagate a scenario identifier in request headers and runner logs for
  correlation in Loki, Tempo and Prometheus.
- Grafana dashboards and the surrounding Loki, Tempo and Prometheus
  configuration belong to `shorturl-gitops`, not to the Kurama implementation
  roadmap.

### Phase 3 — persistent Redis value store (complete)

- Kept `MemoryStore` as the default backend and added a Redis implementation
  of the existing context-aware `ValueStore` interface.
- Isolated Redis keys by namespace, scenario and store name.
- Preserved bounded-capacity semantics, FIFO eviction and random value
  selection with atomic Redis operations.
- Deployed Redis through `shorturl-gitops` as a single-replica StatefulSet
  with AOF persistence and a PVC.
- Verified in kind that captured hashes survive both runner and Redis Pod
  restarts.
- Made Redis value pools shareable by runner replicas while keeping memory
  pools local to a single runner.
- Exported store operation counts, results and latency from the runner and
  exposed its Prometheus endpoint through the generated Deployment.
- Added a provisioned Kurama store dashboard to `shorturl-gitops` and verified
  that Prometheus scrapes the live runner metrics.

### Phase 4 — dynamic traffic profiles

- Add fixed, uniform random, normal and burst traffic profiles.
- Allow the requests-per-minute value to change between configured time
  windows.
- Make random generation reproducible with an optional scenario seed.
- Add per-operation rate caps, including protection for APIs with rate limits.
- Add a Redis-backed distributed rate limiter before scaling a scenario to
  multiple runner replicas, so replicas share one configured request budget.
- Compare dynamic load profiles through the dashboards maintained in
  `shorturl-gitops`.

### Phase 5 — reusable and hardened HTTP scenarios

- Add `none`, bearer-token, API-key and basic authentication, always through
  Kubernetes `Secret` references.
- Add OAuth2 client-credentials only when a real target requires it.
- Add restricted header templates where real scenarios require them.
- Keep the CRD protocol-neutral enough to add a separate executor later; do
  not claim gRPC, Kafka or WebSocket support before implementing it.
- Add target allow-lists, NetworkPolicy examples, resource limits and
  admission validation.
- Do not read a target application's database directly.

## Non-goals for the first release

- Replacing k6, Gatling or a general-purpose workflow engine.
- Executing arbitrary scripts or templates from a custom resource.
- Storing API credentials in Git or in `TrafficScenario.spec`.
- Supporting every transport before HTTP scenarios are reliable.

## Development

```bash
go vet ./...
go test ./...
golangci-lint run
go build ./cmd/manager
go build ./cmd/runner
```

The CI workflow targets Linux/amd64 because the initial deployment target is
the local kind cluster maintained by `shorturl-gitops`.
