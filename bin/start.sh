#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

cd "$ROOT_DIR"

port_is_in_use() {
  lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
}

wait_for_port() {
  port="$1"
  attempts=0

  while port_is_in_use "$port"; do
    attempts=$((attempts + 1))
    if [ "$attempts" -ge 10 ]; then
      return 1
    fi
    sleep 1
  done
}

confirm_stop() {
  printf "Stop %s? [y/N] " "$1"
  if [ ! -t 0 ]; then
    printf '\nCannot ask for confirmation because this shell is not interactive.\n' >&2
    return 1
  fi

  read -r answer
  case "$answer" in
    y|Y|yes|YES|Yes) return 0 ;;
    *) return 1 ;;
  esac
}

free_port() {
  port="$1"
  service="$2"

  if ! port_is_in_use "$port"; then
    return 0
  fi

  printf '\nPort %s, needed by %s, is already in use.\n' "$port" "$service"

  if ! container_ids="$(docker ps --filter "publish=$port" --format '{{.ID}}')"; then
    printf 'Could not inspect Docker containers. Is Docker running and accessible?\n' >&2
    return 1
  fi

  expected_container_id="$(docker compose ps -q "$service" 2>/dev/null | cut -c1-12 || true)"
  conflicting_container_ids=""
  for container_id in $container_ids; do
    if [ "$container_id" != "$expected_container_id" ]; then
      conflicting_container_ids="${conflicting_container_ids}${conflicting_container_ids:+ }${container_id}"
    fi
  done

  if [ -n "$container_ids" ] && [ -z "$conflicting_container_ids" ]; then
    printf 'The port belongs to this Compose project; leaving it running.\n'
    return 0
  fi

  if [ -n "$conflicting_container_ids" ]; then
    printf 'Docker container(s) publishing this port:\n'
    for container_id in $conflicting_container_ids; do
      docker ps --filter "id=$container_id" --format '  {{.ID}}  {{.Names}}  {{.Image}}  {{.Ports}}'
    done

    if ! confirm_stop "the Docker container(s) above"; then
      printf 'Startup cancelled; nothing was stopped.\n' >&2
      return 1
    fi

    # Word splitting is intentional: docker stop accepts multiple container IDs.
    docker stop $conflicting_container_ids
  else
    process_ids="$(lsof -t -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u)"
    if [ -z "$process_ids" ]; then
      printf 'The port became available while it was being inspected.\n'
      return 0
    fi

    printf 'Listening process(es):\n'
    for process_id in $process_ids; do
      ps -p "$process_id" -o pid=,user=,command= 2>/dev/null || true
    done

    if ! confirm_stop "the process(es) above"; then
      printf 'Startup cancelled; nothing was stopped.\n' >&2
      return 1
    fi

    # Word splitting is intentional: kill accepts multiple PIDs.
    kill $process_ids
  fi

  if ! wait_for_port "$port"; then
    printf 'Port %s is still in use after waiting 10 seconds. Startup cancelled.\n' "$port" >&2
    return 1
  fi

  printf 'Port %s is now available.\n' "$port"
}

if ! command -v lsof >/dev/null 2>&1; then
  printf 'lsof is required to check whether Docker ports are available.\n' >&2
  exit 1
fi

free_port 8080 backend
free_port 5173 frontend
free_port 5432 postgres

docker compose up --build -d
