#!/bin/sh
set -e

# Write runtime config for the SPA. API_URL configures the API endpoint
# (empty = same-origin proxy mode). The image ships an inert config.js that
# index.html always loads; we overwrite it here rather than injecting an inline
# <script> into index.html, so the CSP needs no 'unsafe-inline'.
if [ -n "$API_URL" ]; then
  # The value is interpolated into a double-quoted JS string literal, so reject
  # anything that could terminate it or start something else.
  case "$API_URL" in
    *'"'* | *'\'* | *"'"* | *' '* | *'<'* | *'>'*)
      echo "ERROR: API_URL must not contain quotes, backslashes, spaces or angle brackets" >&2
      exit 1
      ;;
  esac

  case "$API_URL" in
    http://* | https://*)
      printf 'window.__CONFIG__={apiUrl:"%s"}\n' "$API_URL" \
        >/usr/share/nginx/html/config.js
      ;;
    *)
      echo "ERROR: API_URL must start with http:// or https://" >&2
      exit 1
      ;;
  esac
fi

exec "$@"
