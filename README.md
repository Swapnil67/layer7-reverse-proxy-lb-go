# Layer-7 Reverse Proxy & Load Balancer

## To Run main.go
```sh
go run ./cmd/proxy/main.go
```

## To Run test 
```sh
go test -v -race ./...
```


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

### Complete architectural diagram

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

