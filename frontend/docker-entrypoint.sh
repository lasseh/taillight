#!/bin/sh
set -e

# Inject runtime config into index.html
# API_URL env var configures the API endpoint (empty = same origin proxy mode)
if [ -n "$API_URL" ]; then
  # Validate: must start with http:// or https://
  case "$API_URL" in
    http://*|https://*)
      # Escape characters that are special in sed replacement strings
      SAFE_URL=$(printf '%s' "$API_URL" | sed 's/[&/\]/\\&/g; s/"/\\"/g')
      CONFIG_SCRIPT="<script>window.__CONFIG__={apiUrl:\"$SAFE_URL\"}<\/script>"
      sed -i "s|</head>|$CONFIG_SCRIPT</head>|" /usr/share/nginx/html/index.html
      ;;
    *)
      echo "ERROR: API_URL must start with http:// or https://" >&2
      exit 1
      ;;
  esac
fi

exec "$@"
