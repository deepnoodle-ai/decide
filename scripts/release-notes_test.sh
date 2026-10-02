#!/bin/sh
# Tests scripts/release-notes.sh on a fixture changelog.
set -eu

script=$(cd "$(dirname "$0")" && pwd)/release-notes.sh
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
cd "$dir"
failed=0

check() { # name, expected exit code, expected output, tag
	got=$("$script" "$4" 2>/dev/null) && code=0 || code=$?
	if [ "$code" != "$2" ] || [ "$got" != "$3" ]; then
		printf 'FAIL %s: exit %s, output:\n%s\n' "$1" "$code" "$got"
		failed=1
	fi
}

cat > CHANGELOG.md <<'EOF'
# Changelog

## [Unreleased]

- Next.

## [0.2.0] - 2026-11-02

Summary.

### Added

- Two.

## [0.1.0] - 2026-10-02

- One.

[0.1.0]: https://example.com
EOF

check "section ends at the next one" 0 "Summary.

### Added

- Two." v0.2.0
check "last section ends at the links" 0 "- One." v0.1.0
check "missing version fails" 1 "" v0.3.0
check "prerelease uses Unreleased" 0 "- Next." v0.3.0-rc.1

sed -i.bak '/- Next./d' CHANGELOG.md
check "empty Unreleased fails" 1 "" v0.3.0-rc.1

rm CHANGELOG.md
check "missing changelog fails" 1 "" v0.1.0

[ "$failed" = 0 ] && echo "ok"
exit "$failed"
