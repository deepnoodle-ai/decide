#!/bin/sh
# Create a git repository for the decide demos: shop at its last commit,
# with change.diff applied but not committed.
#
#   sh demo/setup.sh /tmp/shop
#   cd /tmp/shop && git diff | decide run code-risk --each function
set -eu

dir=${1:?usage: sh demo/setup.sh DIR}
demo=$(cd "$(dirname "$0")" && pwd)
if [ -e "$dir" ]; then
	echo "$dir already exists. Choose a new folder." >&2
	exit 1
fi

mkdir -p "$dir"
cp -R "$demo/repo/." "$dir"
cd "$dir"
git init -q -b main
git apply -R "$demo/change.diff"
git add -A
git -c user.name=demo -c user.email=demo@example.com commit -q -m "Start shop"
git apply "$demo/change.diff"
git add -N .

echo "Created $dir with uncommitted changes. Next:"
echo "  cd $dir"
echo "  git diff | decide run prompt-injection"
