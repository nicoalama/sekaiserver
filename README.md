# sekai-server

**Sekai Link tunnel agent** — a lightweight Go binary that runs on your machine and connects your local LLM (Ollama, LM Studio, etc.) to the cloud via a persistent WebSocket relay.

## How it works

```
   OpenAI-compatible client
         │
         ▼
  ┌─────────────────┐
  │  sekailink.dev  │  Web dashboard — manage credentials
  └────────┬────────┘
           │ POST /api/validate
           ▼
  ┌─────────────────┐
  │   sekai-core    │  WebSocket relay (self-hosted on your VPS)
  └────────┬────────┘
           │ wss://core.sekailink.dev
           ▼
  ┌─────────────────┐
  │  sekai-server   │  ← You are here
  └────────┬────────┘
           │ HTTP
           ▼
  ┌─────────────────┐
  │  Ollama / LLM   │  Local, private, no data leaves your machine
  └─────────────────┘
```

## Quick start

1. Go to **[sekailink.dev](https://sekailink.dev)** and log in with Google.
2. Generate a credential — you'll get a **URL Provider** and an **API Key**.
3. Install and run sekai-server:

### Option A: Binary (recommended)

```bash
curl -sL https://sekailink.dev/install | sh -s -- \
  --relay=wss://core.sekailink.dev \
  --url-provider=https://sekailink.dev/t_YOUR_CODE \
  --api-key=sk_YOUR_API_KEY
```

Or run interactively (the script will prompt you):

```bash
curl -sL https://sekailink.dev/install | sh
```

### Option B: Docker

```yaml
# docker-compose.yml
services:
  ollama:
    image: ollama/ollama:latest
    restart: unless-stopped
    volumes:
      - ollama_data:/root/.ollama
    ports:
      - "11434:11434"

  sekai-server:
    image: ghcr.io/nicoalama/sekai-server:latest
    restart: unless-stopped
    depends_on:
      - ollama
    environment:
      - RELAY=wss://core.sekailink.dev
      - URL_PROVIDER=https://sekailink.dev/t_YOUR_CODE
      - API_KEY=sk_YOUR_API_KEY
      - LOCAL_HOST=ollama
      - LOCAL_PORT=11434

volumes:
  ollama_data:
```

```bash
docker compose up -d
```

4. Use any OpenAI-compatible client pointing to `https://sekailink.dev/t_YOUR_CODE/v1/chat/completions` with your API key.

## Configuration

sekai-server accepts configuration via three sources (in order of priority):

| Source | Example |
|--------|---------|
| Environment variable | `RELAY=wss://...` `URL_PROVIDER=https://...` `API_KEY=sk_...` `LOCAL_HOST=localhost` `LOCAL_PORT=11434` |
| CLI flag | `--relay wss://... --url-provider https://... --api-key sk_...` |
| Config file | `~/.config/sekai-server/config.json` (auto-created on first run) |

### All flags

| Flag | Default | Description |
|------|---------|-------------|
| `--relay` | `ws://localhost:8080` | WebSocket address of sekai-core |
| `--url-provider` | — | URL provider from dashboard |
| `--api-key` | — | API key from dashboard |
| `--local-host` | `localhost` | Host where your LLM is running |
| `--local-port` | `11434` | Port where your LLM is listening |
| `--allow-external-host` | `false` | Allow proxying to non-loopback addresses |
| `--max-body-size` | `10` | Max response body in MB (1–100) |
| `--config` | `~/.config/sekai-server/config.json` | Path to config file |

## Use from anywhere

Once sekai-server is running, your local LLM is available at:

```
https://sekailink.dev/t_YOUR_CODE/v1/models
https://sekailink.dev/t_YOUR_CODE/v1/chat/completions
https://sekailink.dev/t_YOUR_CODE/v1/completions
```

Just pass your API key in the `x-api-key` header (or `Authorization: Bearer`).

### Tools that work out of the box

- [opencode](https://opencode.ai)
- [Cursor](https://cursor.com)
- [Continue](https://continue.dev)
- [LangChain](https://langchain.com)
- Any OpenAI-compatible SDK

## Architecture

- **sekai-server** establishes a persistent outbound WebSocket connection to **sekai-core**. It authenticates with your credential's code + API key, then receives incoming HTTP requests in real-time.
- Requests are proxied to your local LLM over plain HTTP.
- Responses are streamed back through the WebSocket connection to sekai-core, which relays them to the original caller.
- No inbound ports, no public IP, no VPN needed.

## Related

- **sekai-core** — WebSocket relay server (part of the stack, separate binary)
- **[sekailink.dev](https://sekailink.dev)** — Web dashboard for credential management
