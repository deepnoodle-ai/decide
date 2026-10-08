#!/bin/sh
# Usage: ./checked.sh 'git reset --hard HEAD~3'
printf '%s\n' "$1" | decide run command-risk --fail-on flagged
case $? in
  0) sh -c "$1" ;;
  2) echo "decide flagged this command; not running it" >&2; exit 2 ;;
  *) echo "decide could not check this command; not running it" >&2; exit 1 ;;
esac
