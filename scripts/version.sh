# Shared version resolution for build.sh, install.sh and update.sh (source from repository root).
if [ -z "${ARTEX_BUILD_VERSION:-}" ]; then
  if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    ARTEX_BUILD_VERSION="$(git describe --tags --always --dirty)"
  else
    ARTEX_BUILD_VERSION=dev
  fi
fi
ARTEX_BUILD_VERSION="${ARTEX_BUILD_VERSION#v}"
export ARTEX_BUILD_VERSION
