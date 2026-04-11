#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -eq 0 ]]; then
  echo "Usage: $0 <tool> [tool...]" >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"
export PATH="${repo_root}/.bin:${PATH}"

missing=0

install_hint() {
  local tool_name="$1"

  case "${tool_name}" in
    golangci-lint)
      echo "go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8"
      ;;
    gitleaks)
      echo "https://github.com/gitleaks/gitleaks#installing"
      ;;
    govulncheck)
      echo "go install golang.org/x/vuln/cmd/govulncheck@latest"
      ;;
    gofmt|go)
      echo "Install Go from https://go.dev/dl/"
      ;;
    *)
      echo "Install '${tool_name}' and ensure it is available in PATH."
      ;;
  esac

  return 0
}

for tool in "$@"; do
  if command -v "$tool" >/dev/null 2>&1; then
    continue
  fi
  echo "Missing required tool: $tool" >&2
  echo "Install hint: $(install_hint "$tool")" >&2
  missing=1
done

exit "$missing"
