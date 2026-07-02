# sekai-server — Functional & Technical Specification

## Overview

`sekai-server` is a standalone Go binary that runs on the user's machine. It establishes a persistent outbound WebSocket connection to the web (sekailink.dev), allowing the web to route incoming API requests to the user's localhost without opening any inbound ports.

This is the same architectural pattern as Cloudflare Tunnel, Tailscale Funnel, and ngrok agent — but self-hosted and tightly integrated with the sekailink credential system.

---

## Functional Flow

### 1. User creates a credential on the web

User A logs into sekailink.dev, creates a credential:

- Web generates `code` (random), `apiKey` (HMAC), `urlProvider`
- Web stores credential in Neon with hashed apiKey
- Web shows user: `urlProvider` + `apiKey` (shown once, never again)

**User receives:**
```
urlProvider: https://sekailink.dev/t_kAnIy
apiKey:      sk-abc123def456...
```

### 2. User installs and runs sekai-server

```
curl -sL https://sekailink.dev/install | sh -s -- \
  --url-provider=https://sekailink.dev/t_kAnIy \
  --api-key=sk-abc123def456... \
  --local-port=3000
```

The installer:
- Downloads the Go binary for the correct OS/arch
- Creates `~/.sekai-server/config.json`
- Optionally installs as a system service (systemd / launchd)
- Runs the binary

On start, `sekai-server`:

1. Reads config (or flags)
2. Extracts the `code` from `urlProvider` (e.g., `t_kAnIy`)
3. Connects to `wss://sekailink.dev/api/gateway/connect?code=t_kAnIy`
4. Authenticates the WebSocket connection by sending the `apiKey` in the first message
5. Enters a persistent event loop: wait for requests from the web, proxy to localhost, send responses back
6. Also sends periodic heartbeats to keep the connection alive and signal "I'm online"

If the connection drops, it retries with exponential backoff.

### 3. User configures an AI tool

User opens opencode / claude code:
```
OpenAI Compatible Provider
  URL:   https://sekailink.dev/t_kAnIy
  API Key: sk-abc123def456...
```

All AI requests go to this URL.

### 4. Request flow (runtime)

```
opencode                    sekailink.dev              user machine
   │                              │                               │
   │── POST /t_kAnIy ────────────►│                               │
   │   (with apiKey in header)    │                               │
   │                              │── lookup code ───────────────►│
   │                              │   find active WebSocket       │
   │                              │                               │
   │                              │── msg(REQUEST, payload) ─────►│ sekai-server
   │                              │                               │
   │                              │                               │── proxy to
   │                              │                               │   localhost:3000
   │                              │                               │
   │                              │◄── msg(RESPONSE, result) ─────│
   │◄── 200 JSON response ────────│                               │
   │                              │                               │
```

**Key detail:** the web validates the apiKey before forwarding. If invalid, returns 401 immediately.

### 5. Heartbeat & offline detection

- `sekai-server` sends a heartbeat every 30 seconds
- If the web doesn't receive a heartbeat for >90 seconds, marks the credential as `offline`
- Adminboard shows status (Online / Offline) based on this
- When `sekai-server` disconnects gracefully, it sends a `DISCONNECT` message

---

## Technical Architecture

### WebSocket Protocol

Messages use JSON over a single persistent WebSocket:

```typescript
// Client → Server (sekai-server → web)
interface ClientMessage {
  type: "AUTH" | "HEARTBEAT" | "DISCONNECT";
  // AUTH only sent once, right after connection opens
}

// Server → Client (web → sekai-server for each incoming request)
interface IncomingRequest {
  type: "REQUEST";
  id: string;        // uuid to correlate response
  method: string;    // GET, POST, etc.
  path: string;      // the path after /t_kAnIy (e.g., /v1/chat/completions)
  headers: Record<string, string>;
  body: string | null;  // base64 if binary
}

// Client → Server (response back)
interface OutgoingResponse {
  type: "RESPONSE";
  id: string;           // matches the request id
  statusCode: number;   // 200, 404, 500, etc.
  headers: Record<string, string>;
  body: string | null;  // base64 if binary
}
```

The protocol is intentionally simple. No streaming yet — each request gets one response.

### Authentication flow on WebSocket connect

1. `sekai-server` opens `wss://sekailink.dev/api/gateway/connect?code=t_kAnIy`
2. Web looks up the credential by `code`
3. Web generates a **one-time challenge** (random nonce) and sends it to the client
4. `sekai-server` responds with `{ type: "AUTH", apiKey: "<hmac>" }` — the user signs the challenge
5. Web verifies the apiKey hash against the stored hash
6. If valid, connection is established and WebSocket stays open
7. If invalid, WebSocket is closed with 4001

This prevents replay attacks on the WebSocket endpoint.

### Web server (Next.js)

The web needs two new API routes:

| Route | Method | Purpose |
|-------|--------|---------|
| `/api/gateway/connect` | WebSocket | Persistent tunnel connection |
| `/[code]` | Catch-all | API proxy: receives AI tool requests, routes to WebSocket |

### Edge Functions & Vercel limitations

Vercel Hobby/Pro plans have constraints:

| Constraint | Value | Impact |
|-----------|-------|--------|
| Serverless function timeout | 10s (Hobby) / 60s (Pro) | AI streaming responses may exceed this |
| WebSocket support | ✅ Edge Functions + Pages Functions | WebSocket connections are supported on Vercel Edge Functions |
| WebSocket idle timeout | ~1 minute on Edge | Heartbeats keep it alive indefinitely |
| Concurrent connections | 100 (Hobby) / 1000 (Pro) | More than enough per user |

**Solution for streaming (SSE):**
For v1, requests are non-streaming (request → proxy → response). For streaming AI responses:
- V2 can use Vercel's streaming response + chunked transfer
- Each chunk is forwarded over the WebSocket and written to the HTTP response stream
- This avoids timeout issues because streaming keeps the connection open

For v1, we handle only non-streaming requests. If the AI tool sends a streaming request, `sekai-server` buffers the full response from localhost and returns it as a single response. This works but the user won't see tokens arrive one by one. Acceptable for v1.

### State Management (the tricky part)

The challenge: when opencode sends a request to `/[code]`, how does the web know which WebSocket to forward it to?

**Solution: In-memory map on Vercel Edge Function**

```typescript
// global state (per-edge-function-instance)
const connections = new Map<string, WebSocket>();
// key: code (e.g., "t_kAnIy")
// value: active WebSocket connection
```

**Problems with this approach:**

1. **Vercel Edge Functions are stateless** — each request can hit a different instance
2. **Horizontal scaling** — WebSocket is on instance A, HTTP request goes to instance B → fails

**Solutions considered:**

| Solution | Complexity | Robustness |
|----------|-----------|------------|
| **D1:** Global Map (single instance) | Low | Fragile — loses connections on deploy |
| **D2:** Upstash Redis (shared state) | Medium | Works across instances, but adds latency |
| **D3:** WebSocket on a fixed endpoint (single Edge Function) | Low | Works if we force WebSocket + HTTP to same edge region |
| **D4:** Use Vercel's `waitUntil` to keep the function alive and route within the same invocation | Medium | Complex edge case handling |

**Recommended approach for v1: D1 + D3 combined.**

- Deploy the WebSocket handler + the proxy route as a **single Edge Function** in a single region
- Vercel Edge Functions can hold WebSocket connections in-memory
- Since both WebSocket and HTTP requests hit the same deployed function, and vercel routes by path, we need them on the **same function**
- We can use Vercel's `middleware.ts` or a catch-all edge route

**Alternative (and likely better for v1): skip Vercel for the tunnel endpoint entirely.**

Instead of routing through `sekailink.dev`, we deploy a **separate lightweight relay** on a cheap VPS ($4/mo Hetzner) or use Fly.io (free tier) — but this contradicts the no-VPS constraint.

**Practical reality check:**

For v1, the simplest working approach:

1. WebSocket handler + proxy handler live in the same Vercel project
2. Both are Edge Functions (Vercel Edge, not Node.js Serverless)
3. In-memory `Map` holds connections
4. Deploy pin to a single region
5. If connections drop due to redeploy, `sekai-server` reconnects automatically

This works reliably for a small number of users (<100 concurrent). In practice, redeploys are infrequent and reconnection is fast (<1s).

For v2 (scaling), we'd migrate to an external relay server or add Upstash Redis.

### Firewall & NAT considerations

Because `sekai-server` only makes **outbound** connections:

| Scenario | Works? | Why |
|----------|--------|-----|
| Home WiFi behind NAT | ✅ | Only needs outbound HTTPS (443) |
| Corporate firewall | ✅ | Unless they block WebSocket (rare) |
| CGNAT (mobile, Starlink) | ✅ | Same as NAT — outbound only |
| Air-gapped network | ❌ | No outbound internet at all |

No user configuration needed. No port forwarding. No firewall rules.

### HTTPS

- `wss://` ensures the WebSocket is encrypted (TLS)
- `sekailink.dev` handles TLS termination
- The HTTP request from opencode to the web is HTTPS
- The WebSocket from sekai-server to the web is WSS
- The local connection (sekai-server → localhost:3000) is plain HTTP (internal)
- **End-to-end encryption:** ✅ — from opencode to localhost, fully encrypted except the last hop on the user's own machine

### Bigger encryption detail: end-to-end

```
opencode ──HTTPS──► sekailink.dev ──WSS──► sekai-server ──HTTP──► localhost:3000
```

The user's internal localhost can be HTTP since it's the same machine. If the user wants HTTPS to localhost, they can use a self-signed cert (configurable in the future).

---

## sekai-server (Go binary) — Internal Design

### Entry point

```
sekai-server \
  --url-provider=https://sekailink.dev/t_kAnIy \
  --api-key=sk-abc123def456... \
  --local-port=3000
```

Optional flags:

```
  --config=~/.sekai-server/config.json   # config file (default)
  --host=localhost                        # local host to proxy to
  --local-port=3000                       # local port to proxy to
  --relay=wss://sekailink.dev      # web server URL
```

### Config file (`~/.sekai-server/config.json`)

```json
{
  "relay": "wss://sekailink.dev",
  "url_provider": "https://sekailink.dev/t_kAnIy",
  "api_key": "sk-abc123def456...",
  "local_host": "localhost",
  "local_port": 3000
}
```

### Main loop

```
1. parse flags → load config (merge, flags override file)
2. extract code from url_provider
3. connect to wss://{relay}/api/gateway/connect?code={code}
4. authenticate (send apiKey, receive challenge, sign, validate)
5. loop:
   - receive message from WebSocket
   - if HEARTBEAT: send pong
   - if REQUEST (id, method, path, headers, body):
     - parse method/path
     - forward to localhost:{local_port}{path}
     - collect response
     - send RESPONSE(id, statusCode, headers, body) through WebSocket
   - if connection drops: exponential backoff, goto step 3
6. on SIGINT/SIGTERM: send DISCONNECT, clean up
```

### Retry strategy

```
max_backoff = 60 seconds
backoff = 1s → 2s → 4s → 8s → 16s → 30s → 60s (capped)
reset after successful connection
```

### Packaging

```
binaries per release:
  sekai-server-linux-amd64
  sekai-server-linux-arm64
  sekai-server-darwin-amd64
  sekai-server-darwin-arm64
  sekai-server-windows-amd64.exe
```

### Local proxy behavior

- Forwards `method path` to `http://{local_host}:{local_port}{path}`
- Passes all headers (except `host` and `api-key` which are consumed/removed)
- Passes body as-is
- Returns status, headers, body

---

## Installer (curl | sh)

The installer script:

1. Detects OS and architecture
2. Downloads the correct binary from GitHub Releases or `https://sekailink.dev/download/{os}/{arch}/sekai-server`
3. Saves binary to `/usr/local/bin/sekai-server`
4. Creates `~/.sekai-server/config.json` with the provided `--url-provider` and `--api-key`
5. Optionally: installs systemd service (Linux) or launchd plist (macOS)
6. Starts `sekai-server`

Usage:

```bash
# Install and start (everything automatic)
curl -sL https://sekailink.dev/install | sh -s -- \
  --url-provider=https://sekailink.dev/t_kAnIy \
  --api-key=sk-abc123def456... \
  --local-port=3000

# Install later (config already saved)
curl -sL https://sekailink.dev/install | sh
```

---

## Web (Next.js) — New Components

### New API Route: `/api/gateway/connect`

```
WebSocket upgrade endpoint
  Path:  wss://sekailink.dev/api/gateway/connect?code={code}
  Auth:  challenge-response (apiKey signed with nonce)
  State: in-memory Edge Function Map<code, WebSocket>
```

### New API Route: Catch-all `/[code]`

```
Handles all incoming requests from AI tools
  Path:  https://sekailink.dev/{code}/**
  Auth:  x-api-key header (validated against DB hash)
  Logic: look up WebSocket for code → forward request → return response
```

---

## Edge Cases & Error Handling

| Scenario | Behavior |
|----------|----------|
| Invalid apiKey on WebSocket connect | WebSocket closed with 4001 |
| Invalid apiKey on HTTP request | 401 Unauthorized, no WebSocket lookup |
| WebSocket disconnected while processing request | HTTP returns 502 Bad Gateway |
| Code doesn't exist | 404 Not Found |
| sekai-server crashes | WebSocket drops → credential marked offline → adminboard shows Offline |
| Duplicate WebSocket for same code | Old connection is closed, new one takes over |
| Request body larger than 5MB | Reject with 413 (can be increased later) |
| localhost refuses connection | Return 502 Bad Gateway |
| Heartbeat timeout (90s no message) | Connection considered dead, force-close |
| Vercel redeploy (connections dropped) | sekai-server reconnects (max 60s backoff) |

---

## Advantages of the WebSocket Reverse Proxy Model

**For users:**
1. **No port forwarding** — zero network configuration
2. **Works behind any NAT** — home, office, mobile, CGNAT
3. **No firewall changes** — only outbound HTTPS/WSS (port 443)
4. **No public IP needed** — works on a laptop with a mobile hotspot
5. **No dynamic DNS** — the web always knows where to reach the server
6. **Shorter TTM** — install and go, no infrastructure setup
7. **Persistent** — survives network changes, sleep/wake, VPN toggles

**For security:**
1. **Zero attack surface** — no open ports, no IP to scan
2. **API key validated at edge** — unauthorized requests never reach the user
3. **TLS everywhere** — WebSocket is WSS, HTTP requests are HTTPS
4. **No machine exposure** — the user's IP is never visible to callers
5. **No certificate management** — Vercel handles TLS for the web

**For the product:**
1. **Controlled access** — all traffic flows through the web → we can enforce usage limits
2. **Accurate usage tracking** — web knows every request because it forwards them
3. **Graceful degradation** — offline detection is real-time via WebSocket drop
4. **Future streaming** — the WebSocket abstraction naturally extends to SSE/streaming

---

## v2 Ideas (not for v1)

- Streaming responses (chunked forwarding over WebSocket)
- Binary body support (base64 encoding)
- Concurrent request multiplexing
- Upstash Redis for horizontally scalable state
- Custom alias (Pro feature) — the `code` in the URL is the alias
- Usage reporting from sekai-server back to web (redundant since web already tracks)
- mTLS for localhost connection
- Health check endpoint on sekai-server itself
- Multiple credentials per sekai-server instance (run as gateway for multiple codes)
- Shared secret rotation
