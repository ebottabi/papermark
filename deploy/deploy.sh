#!/usr/bin/env bash
# Runs on the host, invoked by CI. Usage: deploy.sh <dir with new server and worker binaries>
set -euo pipefail

src=${1:?usage: deploy.sh <upload-dir>}
bin=/opt/papermark/bin
health=http://127.0.0.1:8787/health
bins=(server worker)

# The worker unit is optional until it is installed (see README, CI/CD).
worker_unit=false
systemctl cat papermark-worker.service >/dev/null 2>&1 && worker_unit=true

restart() {
  systemctl restart papermark.service
  if $worker_unit; then systemctl restart papermark-worker.service; fi
}

wait_healthy() {
  for _ in $(seq 1 30); do
    if curl -fsS --max-time 2 "$health" >/dev/null 2>&1 \
      && systemctl is-active --quiet papermark.service \
      && { ! $worker_unit || systemctl is-active --quiet papermark-worker.service; }; then
      return 0
    fi
    sleep 1
  done
  return 1
}

rollback() {
  echo "health check failed; rolling back" >&2
  for b in "${bins[@]}"; do
    [ -f "$bin/$b.prev" ] && mv -f "$bin/$b.prev" "$bin/$b"
  done
  restart || true
  wait_healthy || echo "still unhealthy after rollback" >&2
}

for b in "${bins[@]}"; do
  [ -s "$src/$b" ] || { echo "missing $src/$b" >&2; exit 1; }
  install -m 755 "$src/$b" "$bin/$b.new"
  [ -f "$bin/$b" ] && cp -p "$bin/$b" "$bin/$b.prev"
  mv -f "$bin/$b.new" "$bin/$b"
done

restart
if wait_healthy; then
  echo "deploy ok"
  rm -rf "$src"
else
  rollback
  exit 1
fi
