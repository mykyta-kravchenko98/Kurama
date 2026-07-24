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
weighted request operations, a traffic profile and bounded value stores.
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
  misses and evictions. Rate limiter metrics distinguish acquisition results
  from the number of requested and granted permits.
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

- Kept fixed request timing as the default and added a uniform-random delay
  profile using the current schedule RPM as its mean rate.
- Added a Redis-backed distributed rate limiter and replica configuration, so
  multiple runners share one request budget.
- Added fixed and Redis-coordinated uniform schedules, allowing one shared RPM
  value to be selected for each configured time window.
- Exported the current selected RPM, schedule resolution results and resolution
  latency through the runner Prometheus endpoint.
- Added manager and runner health/readiness endpoints and generated startup,
  liveness and readiness probes for runner Deployments.
- Kept existing runner replicas available during updates with an explicit
  zero-unavailable rolling strategy and bounded rollout history/deadline.
- Kept uniform-random timing as the normal background traffic profile and
  added an explicit burst profile that preserves the selected mean RPM.
- Reserved burst permits atomically with partial grants, so concurrent runner
  replicas cannot exceed the shared window budget or fragment a granted group.
- Compare dynamic load profiles through the dashboards maintained in
  `shorturl-gitops`.

### Phase 5 — future backlog for reusable and hardened HTTP scenarios

- Add `none`, bearer-token, API-key and basic authentication, always through
  Kubernetes `Secret` references.
- Add OAuth2 client-credentials only when a real target requires it.
- Add restricted header templates where real scenarios require them.
- Make random generation reproducible with an optional scenario seed.
- Add per-operation rate caps, including protection for APIs with rate limits.
- Move cross-field `TrafficScenario` invariants, such as schedule/profile field
  compatibility and burst ranges, into CRD CEL admission rules so Kubernetes
  rejects invalid resources before reconciliation. Retain Go validation as a
  defense-in-depth layer for standalone runner configuration.
- Define stable structured log event codes for scheduler, executor, store and
  limiter events so Loki queries and future model-training data do not depend
  on human-readable log messages. Keep those messages local to their call sites
  instead of promoting one-off text to constants.
- Keep the CRD protocol-neutral enough to add a separate executor later; do
  not claim gRPC, Kafka or WebSocket support before implementing it.
- Add target allow-lists, NetworkPolicy examples and resource limits.
- Do not read a target application's database directly.

## Non-goals for the first release

- Replacing k6, Gatling or a general-purpose workflow engine.
- Executing arbitrary scripts or templates from a custom resource.
- Storing API credentials in Git or in `TrafficScenario.spec`.
- Supporting every transport before HTTP scenarios are reliable.

## Rate schedule configuration

The rate schedule owns the request rate. A constant schedule is configured as:

```yaml
rate:
  schedule:
    type: fixed
    requestsPerMinute: 45
  limiter:
    type: redis
  profile:
    type: uniform
```

A dynamic schedule selects one inclusive uniformly distributed RPM value for
each window. Redis time defines the window boundary and all runner replicas use
the same selected value:

```yaml
rate:
  schedule:
    type: uniform
    minRequestsPerMinute: 2
    maxRequestsPerMinute: 56
    windowMinutes: 1
  limiter:
    type: redis
  profile:
    type: uniform
```

Uniform schedules require the controller Redis address. `profile` controls
when attempts occur within the selected rate, while `limiter` enforces the
shared request budget.

A burst profile chooses an inclusive group size for every burst. Requests
inside the group use short delays followed by a compensating pause, so the
long-term mean remains the RPM selected by the schedule. The effective group
size is capped at the current RPM so a low-rate burst cycle never spans more
than one minute:

```yaml
rate:
  schedule:
    type: uniform
    minRequestsPerMinute: 2
    maxRequestsPerMinute: 128
    windowMinutes: 1
  limiter:
    type: redis
  profile:
    type: burst
    minBurstSize: 5
    maxBurstSize: 15
    delayDivisor: 10
```

Each runner chooses its burst sizes independently. The distributed Redis
limiter atomically reserves the requested group and remains the authority for
the shared request budget across replicas. If only part of the window budget
is available, the granted group is reduced rather than discarded. A runner
failure after reservation can leave unused permits until that limiter window
expires, but it cannot exceed the configured rate.
`delayDivisor` makes the interval inside a burst that many times shorter than
the mean request interval. The default is `10`; a compensating pause after the
burst preserves the selected average RPM.

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
