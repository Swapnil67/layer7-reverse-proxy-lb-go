
---

### 🔄 Request Lifecycle & Communication Diagram

```text
  [ CLIENT ]            [ PROXY HANDLER ]             [ BALANCER ]               [ SERVER POOL ]            [ BACKEND ]
      │                        │                           │                           │                         │
  1.  │─── HTTP GET /api ─────►│                           │                           │                         │
      │                        │                           │                           │                         │
  2.  │                        │──── Next(req) ───────────►│                           │                         │
      │                        │                           │                           │                         │
  3.  │                        │                           │──── GetBackends() ───────►│                         │
      │                        │                           │◄─── []*Backend snapshot ──│                         │
      │                        │                           │                           │                         │
  4.  │                        │                           │─── Evaluates Healthy ──────────────────────────────►│
      │                        │                           │    b.IsAlive() == true?                             │
      │                        │                           │                                                     │
  5.  │                        │◄── Returns *Backend ──────│                                                     │
      │                        │                                                                                 │
  6.  │                        │────────────────────────────────────────────────────────────────────────────────►│
      │                        │    IncrActiveConns() (+1)                                                       │
      │                        │                                                                                 │
  7.  │                        │───────────────────── Forward Request & Stream Response ────────────────────────►│
      │                        │◄──────────────────── HTTP 200 OK Response ──────────────────────────────────────│
      │                        │                                                                                 │
  8.  │                        │────────────────────────────────────────────────────────────────────────────────►│
      │                        │    DecrActiveConns() (-1) via defer                                             │
  9.  │◄── HTTP 200 OK ────────│                                                                                 │
```

---


Complete architectural diagram in ASCII text. It details the **Data Plane** (how incoming client HTTP requests flow through the system) and the **Control Plane** (background health checkers, telemetry, and rate limiting).

```text
===================================================================================================
                                LAYER-7 LOAD BALANCER & REVERSE PROXY
===================================================================================================

[ CLIENT TRAFFIC ]
        │
        │ HTTP Request (e.g., GET /api/v1/game)
        ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│ MAIN HTTP SERVER (Port 8080)                                                                    │
│                                                                                                 │
│  ┌───────────────────────────────────────────────────────────────────────────────────────────┐  │
│  │ MIDDLEWARE PIPELINE (internal/middleware/)                                                │  │
│  │                                                                                           │  │
│  │   [ Request ] ──► 1. Trace ID Injector ──► 2. Panic Recovery ──► 3. Structured Logger     │  │
│  │                                                                          │                │  │
│  │                                                                          ▼                │  │
│  │                                                          4. Token-Bucket Rate Limiter     │  │
│  │                                                             (sync.Map per IP)             │  │
│  └──────────────────────────────────────────────┬────────────────────────────────────────────┘  │
│                                                 │                                               │
│                                                 ▼                                               │
│  ┌───────────────────────────────────────────────────────────────────────────────────────────┐  │
│  │ PROXY FORWARDING ENGINE (internal/proxy/)                                                 │  │
│  │                                                                                           │  │
│  │  1. Requests Backend Selection ─────────────────────┐                                     │  │
│  │  2. Clones & Rewrites Request                       │                                     │  │
│  │     - Header Scrubbing (Hop-by-hop)                 │                                     │  │
│  │     - Sets X-Forwarded-For, Host                    │                                     │  │
│  │  3. Forwards via custom http.Transport              │                                     │  │
│  │  4. Zero-Allocation Response Streaming              │                                     │  │
│  │     - Uses io.CopyBuffer + sync.Pool Byte Buffers   │                                     │  │
│  └──────────────────────────────────────────────┼────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┼───────────────────────────────────────────────┘
                                                  │
                                                  │  LoadBalancer Interface
                                                  ▼  internal/core/interface.go
┌─────────────────────────────────────────────────────────────────────────────────────────────────┐
│ ROUTING ENGINE & UPSTREAM REGISTRY (internal/core/ & internal/balancer/)                        │
│                                                                                                 │
│  ┌───────────────────────────────────────────────────────────────────────────────────────────┐  │
│  │ LoadBalancer Interface Contract                                                           │  │
│  │   Next(r *http.Request) (*Backend, error)                                                 │  │
│  └──────────────────────────────┬──────────────────────────────┬─────────────────────────────┘  │
│                                 │                              │                                │
│                                 ▼                              ▼                                │
│               ┌──────────────────────────────────┐ ┌──────────────────────────────────┐         │
│               │ Round Robin Strategy             │ │ Least Connections Strategy       │         │
│               │ (sync/atomic uint64 counter)     │ │ (Scans activeConns under RLock)  │         │
│               └─────────────────┬────────────────┘ └─────────────────┬────────────────┘         │
│                                 │                                    │                          │
│                                 └──────────────────┬─────────────────┘                          │
│                                                    │                                            │
│                                                    ▼                                            │
│  ┌───────────────────────────────────────────────────────────────────────────────────────────┐  │
│  │ ServerPool Aggregate (internal/core/)                                                     │  │
│  │   - Controlled by sync.RWMutex                                                            │  │
│  │   - Returns snapshot copies of []*Backend                                                 │  │
│  │                                                                                           │  │
│  │   [ Backend 1 ] ──► URL: http://127.0.0.1:8081 | Alive: 1 (Atomic) | ActiveConns: 3       │  │
│  │   [ Backend 2 ] ──► URL: http://127.0.0.1:8082 | Alive: 1 (Atomic) | ActiveConns: 1       │  │
│  │   [ Backend 3 ] ──► URL: http://127.0.0.1:8083 | Alive: 0 (Atomic) | ActiveConns: 0       │  │
│  └───────────────────────────────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────┬────────────────────────────────────────────────────────┘
                                         │
                   ┌─────────────────────┴─────────────────────┐
                   │                                           │
                   ▼                                           ▼
       ┌───────────────────────┐                   ┌───────────────────────┐
       │ UPSTREAM SERVER 1     │                   │ UPSTREAM SERVER 2     │
       │ (Skribbl Instance A)  │                   │ (Skribbl Instance B)  │
       │ http://127.0.0.1:8081 │                   │ http://127.0.0.1:8082 │
       └───────────────────────┘                   └───────────────────────┘

===================================================================================================
                               BACKGROUND CONTROL & MONITORING PLANE
===================================================================================================

  ┌─────────────────────────────────────────┐               ┌──────────────────────────────────┐
  │ BACKGROUND HEALTH CHECKER               │               │ TELEMETRY / METRICS SERVER       │
  │ (internal/health/checker.go)            │               │ (internal/metrics/registry.go)   │
  │                                         │               │                                  │
  │  - Runs background Ticker goroutine     │               │  - Separate Port Handler (9090)  │
  │  - Sends HTTP HEAD/GET to /health       │               │  - Serves GET /metrics           │
  │  - Calls b.SetAlive(bool) atomically    │               │  - Exposes active connections,   │
  │  - Zero blocking on client request path │               │    request rates, & error counts │
  └────────────────────┬────────────────────┘               └──────────────────────────────────┘
                       │
                       └───────────────────► Pings Upstream Servers periodically
```

---

### 🧩 Component Breakdown & Data Flow Explanation

1. **Middleware Stack (`internal/middleware/`):**
   * Every incoming request first hits our middleware chain. It assigns a unique **Trace ID**, sets up a **Panic Recovery** boundary (`defer recover()`), logs request metadata, and evaluates the IP against a **Token-Bucket Rate Limiter** stored in a `sync.Map`.
2. **Proxy Forwarding Engine (`internal/proxy/`):**
   * If the middleware approves the request, the forwarding engine queries the `LoadBalancer` interface to get a healthy `Backend`.
   * It creates a shallow clone of `*http.Request`, strips hop-by-hop headers (e.g., `Connection`, `Keep-Alive`), adds `X-Forwarded-For` headers, and transmits it via `http.Transport`.
   * The response body is streamed back to the client using `io.CopyBuffer` with byte buffers retrieved from `sync.Pool` to eliminate heap allocation thrashing.
3. **Domain Core & Routing Algorithms (`internal/core/` & `internal/balancer/` - Phase 1 & 3):**
   * The proxy relies purely on the abstract `LoadBalancer` interface (`Dependency Inversion`).
   * Concrete implementations (`RoundRobin` using atomic operations or `LeastConnections` scanning active connection counters) select the optimal target `Backend`.
   * The `ServerPool` manages the collection of backends behind a `sync.RWMutex`, handing out safe snapshot copies to routing queries.
4. **Background Control & Telemetry (`internal/health/` & `internal/metrics/` - Phase 4 & 6):**
   * **Health Checker:** Runs ticker goroutines in the background, pinging each backend's `/health` endpoint and updating `b.SetAlive()` atomically without blocking incoming client requests.
   * **Metrics Endpoint:** Listens on an isolated port (e.g., `:9090`) to expose telemetry data (active connections, throughput, error rates) without exposing administrative statistics on the public proxy port.

---

This visual reference serves as our architectural blueprint.