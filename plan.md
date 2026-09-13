**Layer-7 Reverse Proxy & Load Balancer** using exclusively the **Go Standard Library**.

---

### 🌐 Architectural Strategies & Trade-Off Analysis

#### Strategy 1: Request Forwarding Architecture
* **Option A: Standard Library `httputil.ReverseProxy` Wrapper**
  * *Pros:* Battle-tested out-of-the-box handling of edge cases (e.g., hop-by-hop headers, `X-Forwarded-For`, connection upgrading).
  * *Cons:* Abstracts away low-level buffer management and request cloning, missing our core learning objective.
* **Option B: Custom Manual Forwarding Engine using `http.Client` & `io.CopyBuffer` (Selected Strategy)**
  * *Pros:* Total control over request header scrubbing/rewriting, explicit memory management with `sync.Pool`, zero unnecessary allocations during response body streaming.
  * *Cons:* Requires manual management of hop-by-hop headers (`Connection`, `Keep-Alive`, `Te`, `Trailers`) and HTTP response header copying.

#### Strategy 2: Upstream State & Routing Concurrency
* **Option A: Mutex-Guarded Backend Registry (`sync.RWMutex`) (Selected Strategy for Initial Phases)**
  * *Pros:* Simple to reason about, allows safe read operations for routing while providing write exclusivity when health checks modify server status.
  * *Cons:* Can introduce lock contention under extreme high-throughput parallel requests.
* **Option B: Atomic Lock-Free Registry (`sync/atomic` with Pointer Snapshots)**
  * *Pros:* Near-zero lock contention for read-heavy routing traffic.
  * *Cons:* Requires immutable slice updates (copy-on-write) during status changes, slightly increasing memory allocation overhead during frequent health state toggles.

#### Strategy C: Thread-Safe Rate Limiting
* **Option A: Token Bucket via `sync.Map` (Selected Strategy)**
  * *Pros:* High throughput for disjoint key lookups (IP addresses) without holding a global lock.
  * *Cons:* `sync.Map` can incur memory overhead over time if inactive keys churn; requires an active background janitor process to purge stale IP buckets.
* **Option B: Fixed Window Counter with Sharded Mutexes**
  * *Pros:* Predictable heap memory footprint; easy lock contention profiling.
  * *Cons:* Less accurate traffic smoothing compared to Token Bucket (suffers from boundary bursts).

---

### 📋 Phase-by-Phase Execution Plan

#### **Phase 1: Domain Abstractions & OOP Core Interfaces**
* **Goal:** Establish clean, extensible object-oriented Go abstractions (interfaces, structs, and type contracts) to decouple the proxy engine from specific load-balancing strategies.
* **Key Functions & Component Architecture:**
  * `type Backend struct`: Encapsulates upstream server details (`URL`, `Alive` boolean, `ActiveConns` counter, `ReverseProxy` reference).
  * `type LoadBalancer interface`: Defines the strategy contract containing `Next() (*Backend, error)` and `UpdateHealth(url *url.URL, alive bool)`.
  * `type ServerPool struct`: Aggregates backends and embeds strategy logic with thread-safe controls.
* **OOP Principle:** **Abstraction & Dependency Inversion** — the HTTP forwarding handler relies on the `LoadBalancer` interface, allowing hot-swapping algorithms without modifying proxy execution code.

---

#### **Phase 2: Manual Request Forwarding & Zero-Allocation Streaming Engine**
* **Goal:** Build a manual request forwarding engine that clones client requests, cleans hop-by-hop headers, forwards traffic via a custom `http.Transport`, and streams responses back to clients using recycled buffers.
* **Key Functions & Component Architecture:**
  * `CloneRequest(req *http.Request, target *url.URL) *http.Request`: Performs a deep copy of incoming headers, updates `Host` and `URL` fields, strips hop-by-hop headers, and appends `X-Forwarded-For` and trace metadata.
  * `ServeHTTP(w http.ResponseWriter, r *http.Request)`: The core `http.Handler` implementation. Fetches a backend from the pool, forwards the request, sets response status, and streams the body.
  * `StreamResponseBody(dst io.Writer, src io.Reader)`: Employs `io.CopyBuffer` combined with a `sync.Pool` of fixed-size byte slices (e.g., 32KB) to stream response chunks back to the client without heap allocation thrashing.

---

#### **Phase 3: Thread-Safe Load Balancing Algorithms**
* **Goal:** Implement and validate two thread-safe routing strategies capable of handling concurrent client traffic.
* **Key Functions & Component Architecture:**
  * `RoundRobinStrategy`: Uses atomic integer operations (`sync/atomic.AddUint64`) to cycle through healthy backends sequentially.
  * `LeastConnectionsStrategy`: Traverses the pool under `sync.RWMutex.RLock` to identify and return the backend with the lowest `ActiveConns` value.
  * `TrackConnection(b *Backend) func()`: Increments active connections upon selection and returns a completion closure (`defer`) to atomically decrement the counter once the response stream terminates.

---

#### **Phase 4: Asynchronous Health Checkers & Registry State Updates**
* **Goal:** Maintain accurate upstream registry state using concurrent background worker tickers that monitor backend vitality without blocking request processing.
* **Key Functions & Component Architecture:**
  * `StartHealthChecker(ctx context.Context, interval time.Duration)`: Launches a goroutine with `time.NewTicker` to trigger periodic health checks across all registered backends.
  * `PingBackend(b *Backend) bool`: Uses an `http.Client` configured with a short timeout (e.g., 2 seconds) to perform HTTP `HEAD` or `GET` checks against backend `/health` endpoints.
  * `SetAlive(alive bool)`: Thread-safely updates the `Alive` flag on a `Backend` and updates pool availability counts under a write lock (`sync.RWMutex.Lock`).

---

#### **Phase 5: Middleware Chaining & Token-Bucket Rate Limiter**
* **Goal:** Wrap the proxy handler in a composable middleware pipeline providing trace generation, panic recovery, structured logging, and IP-based rate limiting.
* **Key Functions & Component Architecture:**
  * `type Middleware func(http.Handler) http.Handler`: Defines standard middleware composition type.
  * `Chain(h http.Handler, middlewares ...Middleware) http.Handler`: Sequentially chains middleware functions into a single execution stack.
  * `TraceIDMiddleware`: Injects or propagates a unique UUID/Trace-ID into request contexts and response headers.
  * `PanicRecoveryMiddleware`: Uses `defer` and `recover()` to capture runtime panics, log stack traces via `log/slog`, and gracefully return HTTP 500 errors.
  * `TokenBucketLimiter`: Manages IP-to-bucket mapping using `sync.Map`. Each bucket tracks tokens and refill timestamps atomically.
  * `JanitorCleanup()`: Periodic background task purging inactive IP buckets from `sync.Map` to prevent memory leaks.

---

#### **Phase 6: Metrics Endpoint, Go Benchmarking & Integration**
* **Goal:** Expose proxy operational telemetry on a dedicated HTTP handler and benchmark proxy throughput and allocations.
* **Key Functions & Component Architecture:**
  * `MetricsHandler(w http.ResponseWriter, r *http.Request)`: Serves stats (active connections per backend, total processed requests, error rates, average latency) on a isolated `/metrics` port.
  * `BenchmarkProxyForwarding(b *testing.B)`: Micro-benchmarks request handling under high concurrency using `httptest.Server`, evaluating throughput (`ns/op`) and heap allocations (`B/op` and `allocs/op`).
  * **Integration Validation:** Route incoming traffic through the proxy to multiple instances of your Skribbl clone to ensure full compatibility with stateful/long-polling HTTP communication.

---

#### Strategy 3: Idiomatic Standard Go Project Layout with `internal/` (Recommended)
* **Structure:**
  ```text
  lb-proxy/
  ├── cmd/
  │   └── proxy/
  │       └── main.go              # Application entry point & flag parsing
  ├── internal/
  │   ├── core/                    # Domain models (Backend, LoadBalancer interface)
  │   ├── balancer/                # RoundRobin & LeastConnections implementations
  │   ├── proxy/                   # Forwarding engine & io.CopyBuffer streaming
  │   ├── health/                  # Background health ticker and pingers
  │   ├── middleware/              # Logging, Panic recovery, Rate limiter
  │   └── metrics/                 # Telemetry & HTTP /metrics handler
  ├── benchmark/                   # Go performance benchmarks (testing.B)
  ├── go.mod
  └── README.md
  ```
* **Pros:** 
  * Enforces strict encapsulation via Go's `internal/` package rule (prevents external projects from importing private logic).
  * Decouples domain interfaces (`internal/core`) from concrete algorithms (`internal/balancer`) and networking (`internal/proxy`), eliminating circular import loops.
  * Dedicated `benchmark/` folder keeps micro-benchmarks cleanly isolated.
* **Cons:** Slightly higher initial setup overhead with Go package import paths.

---

### 📁 Proposed Recommended Directory Layout Details

Here is the detailed blueprint of what each directory and file will handle across our project phases:

```text
lb-proxy/
├── cmd/
│   └── proxy/
│       └── main.go               # Wires up configurations, initializes pool, starts servers
├── internal/
│   ├── core/
│   │   ├── backend.go            # Backend struct, active conn tracking, health status
│   │   ├── interface.go          # LoadBalancer interface & ServerPool definition
│   │   └── errors.go             # Domain-specific sentinel errors
│   ├── balancer/
│   │   ├── round_robin.go        # Thread-safe Round-Robin strategy (atomic counter)
│   │   └── least_connections.go  # Thread-safe Least Connections strategy (RWMutex)
│   ├── proxy/
│   │   ├── forwarder.go          # Manual Request cloning, header scrubbing, http.Transport
│   │   └── buffer_pool.go        # sync.Pool byte buffer allocation for io.CopyBuffer
│   ├── health/
│   │   └── checker.go            # Asynchronous ticker goroutines & HTTP health pingers
│   ├── middleware/
│   │   ├── chain.go              # Middleware chaining engine
│   │   ├── logging.go            # Structured logging & trace ID injection
│   │   ├── recovery.go           # Panic recovery middleware
│   │   └── rate_limiter.go       # Custom Token-Bucket rate limiter using sync.Map
│   └── metrics/
│       └── registry.go           # Active connection counters, request telemetry & /metrics handler
├── benchmark/
│   └── proxy_test.go             # Go benchmark tests measuring ns/op and B/op allocations
├── go.mod
└── README.md
```

---

### 🔍 How Each File Solves Our Core Goals

1. **`internal/core/backend.go` & `interface.go` (Phase 1):**
   * *Purpose:* Establishes object-oriented domain models.
   * *Function:* Encapsulates upstream server state and defines the `LoadBalancer` interface contract, keeping balancing logic separate from proxying.

2. **`internal/proxy/forwarder.go` & `buffer_pool.go` (Phase 2):**
   * *Purpose:* Low-level I/O handling and zero-allocation memory streaming.
   * *Function:* Manually clones incoming `*http.Request` instances, strips hop-by-hop headers, and streams response bodies back to clients using fixed-size byte pools (`sync.Pool`).

3. **`internal/balancer/` (Phase 3):**
   * *Purpose:* Concurrent load routing strategies.
   * *Function:* Implements `RoundRobin` using atomic operations (`sync/atomic`) and `LeastConnections` using read/write locks (`sync.RWMutex`).

4. **`internal/health/checker.go` (Phase 4):**
   * *Purpose:* Non-blocking status updates.
   * *Function:* Runs background `time.Ticker` loops in separate goroutines to ping upstream servers and update their health status in the shared pool.

5. **`internal/middleware/` (Phase 5):**
   * *Purpose:* Native HTTP middleware composition and resilience.
   * *Function:* Wraps standard `http.Handler` functions to add trace IDs, handle panics, and enforce rate limits using a thread-safe `sync.Map` token bucket.

6. **`internal/metrics/` & `benchmark/` (Phase 6):**
   * *Purpose:* Telemetry exposure and validation standards.
   * *Function:* Exposes internal statistics via an isolated `/metrics` endpoint and provides performance micro-benchmarking (`testing.B`) for execution speed and heap allocations.

---