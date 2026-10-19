#!/usr/bin/env bash
# Reset the SQLite-backed administrator password through ARTEX itself.
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"
mode=local
config_path=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -m|--mode) mode="$2"; shift 2 ;;
    -c|--config) config_path="$2"; shift 2 ;;
    -h|--help) echo "Usage: ./reset-password.sh [-m local|docker] [-c config.json]"; exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done
read -r -s -p '输入新密码（至少 8 个字符）：' password; echo >&2
read -r -s -p '再次输入：' confirmation; echo >&2
[ "$password" = "$confirmation" ] || { echo '两次输入不一致' >&2; exit 1; }
if [ "$mode" = docker ]; then
  printf '%s\n' "$password" | docker compose exec -T artex /app/artex -reset-password
elif [ "$mode" = local ]; then
  [ -z "$config_path" ] || export ARTEX_CONFIG="$config_path"
  printf '%s\n' "$password" | ./artex -reset-password
else
  echo 'mode must be local or docker' >&2; exit 1
fi
unset password confirmation
