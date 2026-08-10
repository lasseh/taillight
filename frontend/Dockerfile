FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginxinc/nginx-unprivileged:alpine
COPY --from=build /app/dist /usr/share/nginx/html

# The entrypoint hook rewrites config.js at start-up and this image runs as
# UID 101, so that one file (and only that one) has to be writable by it.
COPY --from=build --chown=101:0 /app/dist/config.js /usr/share/nginx/html/config.js

# Entrypoint script for runtime config injection
COPY docker-entrypoint.sh /docker-entrypoint.d/40-inject-config.sh

# Static-only nginx config (no API proxy)
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 8080
