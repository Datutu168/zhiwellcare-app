#!/usr/bin/env bash
# Install the production nginx vhosts (Lighthouse / single-host layout).
#
#   sudo bash deploy/nginx-sites/install.sh            # install all three
#   sudo bash deploy/nginx-sites/install.sh zwkl-admin # install one
#
# Backs up the previous file to /root/<name>.conf.bak-<timestamp>, runs
# nginx -t, and only reloads when the syntax test passes.
set -euo pipefail

SRC_DIR=$(cd "$(dirname "$0")" && pwd)
SITES=${*:-"zwkl-web zwkl-app zwkl-admin"}
STAMP=$(date +%Y%m%d-%H%M%S)

if [ "$(id -u)" -ne 0 ]; then
  echo "must run as root (sudo)" >&2
  exit 1
fi

for name in $SITES; do
  src="$SRC_DIR/$name.conf"
  if [ ! -f "$src" ]; then
    echo "missing template: $src" >&2
    exit 1
  fi
  dst="/etc/nginx/sites-available/$name"
  if [ -f "$dst" ]; then
    cp -a "$dst" "/root/$name.conf.bak-$STAMP"
    echo "backup: /root/$name.conf.bak-$STAMP"
  fi
  cp -f "$src" "$dst"
  chown root:root "$dst"
  chmod 644 "$dst"
  ln -sfn "$dst" "/etc/nginx/sites-enabled/$name"
  echo "installed: $name"
done

# The default site shipped by the nginx package competes with zwkl-web
# (which is already default_server).
if [ -e /etc/nginx/sites-enabled/default ]; then
  echo "removing /etc/nginx/sites-enabled/default (conflicts with zwkl-web)"
  rm -f /etc/nginx/sites-enabled/default
fi

nginx -t
systemctl reload nginx
echo "nginx reloaded"

echo "=== self-check ==="
for host in zhiwellcare.com game.zhiwellcare.com admin.zhiwellcare.com; do
  printf '%-26s / -> %s  /api/v1/catalog/courses -> %s\n' "$host" \
    "$(curl -s -o /dev/null -w '%{http_code}' -H "Host: $host" http://127.0.0.1/)" \
    "$(curl -s -o /dev/null -w '%{http_code}' -H "Host: $host" http://127.0.0.1/api/v1/catalog/courses)"
done
echo "expected: app/admin hosts proxy the API (200 with JSON); the marketing site is static (404 on /api/)"
