#!/bin/sh
# Prints the CHANGELOG.md section for a tag, such as v0.2.0, for the
# release notes. A prerelease with no section of its own uses Unreleased.
set -eu

tag=${1:?usage: scripts/release-notes.sh <tag>}
version=${tag#v}
[ -r CHANGELOG.md ] || { echo "Run this from the repository root." >&2; exit 1; }

section() {
	awk -v head="## [$1]" '
		index($0, head) == 1 { found = 1; next }
		found && (/^## \[/ || /^\[[^]]+\]: /) { exit }
		found
	' CHANGELOG.md | sed -e '/./,$!d'
}

notes=$(section "$version")
case $version in *-*) [ -n "$notes" ] || notes=$(section Unreleased) ;; esac
if [ -z "$notes" ]; then
	echo "CHANGELOG.md has no section for $version. See docs/releasing.md." >&2
	exit 1
fi
printf '%s\n' "$notes"
