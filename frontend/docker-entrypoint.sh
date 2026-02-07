#!/bin/sh
set -e

# Inject runtime config into index.html
# API_URL env var configures the API endpoint (empty = same origin proxy mode)
if [ -n "$API_URL" ]; then
  CONFIG_SCRIPT="<script>window.__CONFIG__={apiUrl:\"$API_URL\"}<\/script>"
  sed -i "s|</head>|$CONFIG_SCRIPT</head>|" /usr/share/nginx/html/index.html
fi

exec "$@"
