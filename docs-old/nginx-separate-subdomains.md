# Nginx Configuration: Separate Subdomains

This guide configures taillight with separate subdomains:
- `taillight.example.com` — Frontend (static files)
- `api.taillight.example.com` — Backend API

## Production nginx.conf

```nginx
http {
    # Rate limiting zones
    limit_req_zone $binary_remote_addr zone=ingest:10m rate=100r/s;
    limit_req_zone $binary_remote_addr zone=api:10m rate=50r/s;

    # ─────────────────────────────────────────────────────────────────
    # API Backend: api.taillight.example.com
    # ─────────────────────────────────────────────────────────────────
    upstream taillight_api {
        server 127.0.0.1:8080;
        keepalive 32;
    }

    server {
        listen 443 ssl http2;
        server_name api.taillight.example.com;

        ssl_certificate     /etc/ssl/certs/api.taillight.crt;
        ssl_certificate_key /etc/ssl/private/api.taillight.key;

        # Security headers
        add_header X-Content-Type-Options nosniff always;
        add_header X-Frame-Options DENY always;

        # CORS is handled by the Go backend, not nginx

        # Ingest endpoint: rate limited
        location /api/v1/applog/ingest {
            limit_req zone=ingest burst=200 nodelay;
            client_max_body_size 5m;

            proxy_pass http://taillight_api;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Request-ID $request_id;
        }

        # SSE streams: long-lived connections, no buffering
        location ~ ^/api/v1/(syslog|applog)/stream$ {
            proxy_pass http://taillight_api;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header Connection '';

            # Disable buffering for SSE
            proxy_buffering off;
            proxy_cache off;

            # Long timeout for SSE connections
            proxy_read_timeout 24h;
            proxy_send_timeout 24h;
        }

        # REST API
        location /api/ {
            limit_req zone=api burst=100 nodelay;

            proxy_pass http://taillight_api;
            proxy_http_version 1.1;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Request-ID $request_id;
        }

        # Health check
        location /health {
            proxy_pass http://taillight_api;
        }

        # Metrics (internal only)
        location /metrics {
            allow 10.0.0.0/8;
            allow 172.16.0.0/12;
            allow 192.168.0.0/16;
            deny all;
            proxy_pass http://taillight_api;
        }
    }

    # ─────────────────────────────────────────────────────────────────
    # Frontend: taillight.example.com
    # ─────────────────────────────────────────────────────────────────
    server {
        listen 443 ssl http2;
        server_name taillight.example.com www.taillight.example.com;

        ssl_certificate     /etc/ssl/certs/taillight.crt;
        ssl_certificate_key /etc/ssl/private/taillight.key;

        root /var/www/taillight;
        index index.html;

        # Security headers
        add_header X-Content-Type-Options nosniff always;
        add_header X-Frame-Options SAMEORIGIN always;

        # SPA routing
        location / {
            try_files $uri $uri/ /index.html;
        }

        # Static assets caching
        location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2)$ {
            expires 1y;
            add_header Cache-Control "public, immutable";
        }

        # Health check for load balancers
        location /health {
            access_log off;
            return 200 "OK\n";
            add_header Content-Type text/plain;
        }
    }

    # ─────────────────────────────────────────────────────────────────
    # HTTP → HTTPS redirects
    # ─────────────────────────────────────────────────────────────────
    server {
        listen 80;
        server_name taillight.example.com www.taillight.example.com api.taillight.example.com;
        return 301 https://$host$request_uri;
    }
}
```

## API Configuration

Configure CORS to allow the frontend subdomain:

```yaml
# api/config.yaml
cors_allowed_origins:
  - "https://taillight.example.com"
  - "https://www.taillight.example.com"
```

Or via environment variable:

```bash
CORS_ALLOWED_ORIGINS="https://taillight.example.com,https://www.taillight.example.com"
```

## Frontend Configuration

Deploy the frontend with the API URL configured:

### Docker

```bash
docker run -d \
  -p 80:80 \
  -e API_URL=https://api.taillight.example.com \
  -v ./nginx-standalone.conf:/etc/nginx/conf.d/default.conf:ro \
  taillight-frontend
```

### Static Deployment

When deploying to a CDN or static host, inject the config script into `index.html`:

```html
<!-- Add before </head> -->
<script>window.__CONFIG__={apiUrl:"https://api.taillight.example.com"}</script>
```

## Docker Compose

Use `docker-compose.separate.yml`:

```bash
docker compose -f docker-compose.separate.yml up -d
```

## Benefits

1. **Independent scaling** — Frontend on CDN, API on dedicated servers
2. **Independent deployments** — Update frontend without touching API
3. **API reusability** — Other clients (mobile, scripts) call API directly
4. **Clear separation** — Each subdomain has its own TLS cert and rate limits

## Trade-offs vs Proxy Mode

| Aspect | Separate Subdomains | Single Domain (Proxy) |
|--------|--------------------|-----------------------|
| CORS | Required | Not needed |
| TLS Certs | Two (or wildcard) | One |
| CDN | Easy for frontend | Harder to split |
| Complexity | Higher | Lower |
| SSE | Works (with CORS) | Works |
