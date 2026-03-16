# openai-proxy


## Deploy

`docker-compose.yml` example:

```
   openai-proxy:
    image: ghcr.io/orvice/openai-proxy:main
    restart: always
    container_name:   openai-proxy
    ports:
      - 8080
    environment:
      - OPENAI_API_KEY=sk-xxxx
```

## MCP Gateway

You can configure MCP upstreams and access them through the built-in gateway routes:

- `GET /mcp/servers`
- `ANY /mcp`
- `ANY /mcp/:server`
- `ANY /mcp/:server/*path`

Example config:

```yaml
defaultMcpServer: filesystem
mcpServers:
  - name: filesystem
    host: https://mcp.example.com/mcp
    key: sk-mcp-xxxx
    headers:
      X-Workspace-ID: demo
```

Use `x-mcp-server` on `/mcp` to select a target dynamically, or call `/mcp/:server` directly.

A fuller example is available in [config.example.yaml](/Users/orvice/workspace/go/orvice/openai-proxy/config.example.yaml).
