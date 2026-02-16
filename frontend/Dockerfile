FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY --from=build /app/dist /usr/share/nginx/html

# Entrypoint script for runtime config injection
COPY docker-entrypoint.sh /docker-entrypoint.d/40-inject-config.sh
RUN chmod +x /docker-entrypoint.d/40-inject-config.sh

# Static-only nginx config (no API proxy)
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
