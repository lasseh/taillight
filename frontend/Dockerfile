FROM node:22-alpine AS build
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

# Default: proxy mode (API_BACKEND required)
# For standalone mode (separate subdomain), set API_URL and use nginx-standalone.conf
COPY nginx.conf.template /etc/nginx/templates/default.conf.template

EXPOSE 80
