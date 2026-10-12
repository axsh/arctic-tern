#!/usr/bin/env bash
# check_docs_index.sh — Verify README/docs index layout after documentation split.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./scripts/utils/check_docs_index.sh

Verifies that the top-level README is an index and that docs/ files exist
with kebab-case names (including reference-manual-web-apis.md).

Exit codes:
  0  all checks passed
  1  one or more checks failed
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

failures=0
fail() {
  echo "FAIL: $*" >&2
  failures=$((failures + 1))
}
pass() {
  echo "OK: $*"
}

required_files=(
  docs/README.md
  docs/getting-started.md
  docs/examples.md
  docs/artifacts.md
  docs/use-cases.md
  docs/architecture.md
  docs/roadmap.md
  docs/testing.md
  docs/project-structure.md
  docs/reference-manual-web-apis.md
  docs/client-sse-tool-input.md
  docs/sse-chunk-protocol.md
)

for f in "${required_files[@]}"; do
  if [[ -f "$f" ]]; then
    pass "exists $f"
  else
    fail "missing $f"
  fi
done

if [[ -e docs/ReferenceManual-WebAPIs.md ]]; then
  fail "old path still exists: docs/ReferenceManual-WebAPIs.md"
else
  pass "old ReferenceManual path removed"
fi

forbidden_headings=(
  "## Installation"
  "## Quick Start"
  "## Artifact API Examples"
  "## Testing"
  "## Roadmap"
)

# "## Example" as a standalone section (not "## Example Use Cases")
if grep -qE '^## Example$' README.md; then
  fail "README still has ## Example section"
else
  pass "README has no ## Example section"
fi

for h in "${forbidden_headings[@]}"; do
  if grep -qxF "$h" README.md; then
    fail "README still has heading: $h"
  else
    pass "README lacks heading: $h"
  fi
done

for needle in "docs/README.md" "docs/getting-started.md" "docs/reference-manual-web-apis.md"; do
  if grep -qF "$needle" README.md; then
    pass "README links to $needle"
  else
    fail "README missing link to $needle"
  fi
done

readme_lc=$(tr '[:upper:]' '[:lower:]' < README.md)
r8_hits=0
for word in embed lightweight "on-demand" ondemand; do
  if grep -qF "$word" <<<"$readme_lc"; then
    r8_hits=$((r8_hits + 1))
  fi
done
# Accept "embed" / "embedded" / "embeddable" via embed substring already.
if [[ "$r8_hits" -ge 2 ]]; then
  pass "README mentions R8 keywords (hits=$r8_hits)"
else
  fail "README should mention embed/lightweight/on-demand style keywords (hits=$r8_hits)"
fi

if command -v rg >/dev/null 2>&1; then
  if rg -n 'ReferenceManual-WebAPIs' README.md docs examples >/tmp/docs_index_oldpath.txt 2>/dev/null; then
    fail "old ReferenceManual-WebAPIs path still referenced:"
    cat /tmp/docs_index_oldpath.txt >&2 || true
  else
    pass "no ReferenceManual-WebAPIs refs in README/docs/examples"
  fi
else
  matches=$(grep -RIn 'ReferenceManual-WebAPIs' README.md docs examples 2>/dev/null || true)
  if [[ -n "$matches" ]]; then
    fail "old ReferenceManual-WebAPIs path still referenced:"
    echo "$matches" >&2
  else
    pass "no ReferenceManual-WebAPIs refs in README/docs/examples"
  fi
fi

# Relative markdown link existence (ignore http(s) and mailto)
check_links_in() {
  local file="$1"
  local dir
  dir=$(dirname "$file")
  # markdown links: ](path) or ](path#anchor)
  while IFS= read -r target; do
    [[ -z "$target" ]] && continue
    case "$target" in
      http://*|https://*|mailto:*|\#*) continue ;;
    esac
    local path="${target%%\#*}"
    [[ -z "$path" ]] && continue
    local resolved
    if [[ "$path" == /* ]]; then
      resolved="$ROOT$path"
    else
      resolved="$dir/$path"
    fi
    # normalize ..
    if [[ ! -e "$resolved" ]]; then
      fail "broken link in $file -> $target"
    fi
  done < <(grep -oE '\[[^]]*\]\([^)]+\)' "$file" | sed -E 's/.*\(([^)]+)\).*/\1/' || true)
}

check_links_in README.md
shopt -s nullglob
for f in docs/*.md; do
  check_links_in "$f"
done
shopt -u nullglob

if [[ "$failures" -gt 0 ]]; then
  echo "check_docs_index.sh: $failures failure(s)" >&2
  exit 1
fi

echo "check_docs_index.sh: all checks passed"
exit 0
