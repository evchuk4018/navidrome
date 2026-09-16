#!/usr/bin/env bash

set -euo pipefail

compose_file="${NAVIDROME_COMPOSE_FILE:-/srv/storage/wowzerbowser/files/navidrome/docker-compose.yml}"
env_file="${NAVIDROME_ENV_FILE:-/etc/navidrome/navidrome.env}"
project_name="${NAVIDROME_COMPOSE_PROJECT:-navidrome}"

if [[ ! -f "$compose_file" ]]; then
  printf 'Navidrome Compose file not found: %s\n' "$compose_file" >&2
  exit 1
fi

compose_file="$(realpath -- "$compose_file")"
compose_dir="$(dirname -- "$compose_file")"
cd "$compose_dir"

compose_env_args=()
if [[ -f "$env_file" ]]; then
  env_file="$(realpath -- "$env_file")"
  compose_env_args=(--env-file "$env_file")
fi

printf 'Pulling the Navidrome image using %s\n' "$compose_file"
docker compose "${compose_env_args[@]}" \
  --project-name "$project_name" \
  --file "$compose_file" \
  pull navidrome

printf 'Recreating Navidrome if the image changed\n'
docker compose "${compose_env_args[@]}" \
  --project-name "$project_name" \
  --file "$compose_file" \
  up -d --wait navidrome
