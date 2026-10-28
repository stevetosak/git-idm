#!/bin/sh
set -eu

head="${1:-HEAD}"
release_tag='^v[0-9]+\.[0-9]+\.[0-9]+$'

current="$(git tag --points-at "$head" | grep -E "$release_tag" | sort -V | tail -n 1 || true)"
if [ -n "$current" ]; then
	printf 'tag=%s\nbump=existing\n' "$current"
	exit 0
fi

last="$(git tag --merged "$head" | grep -E "$release_tag" | sort -V | tail -n 1 || true)"
if [ -n "$last" ]; then
	subjects="$(git log --no-merges --format=%s "$last..$head")"
else
	last="v0.0.0"
	subjects="$(git log --no-merges --format=%s "$head")"
fi

type_pattern='(^|\| )'
if printf '%s\n' "$subjects" | grep -Eq "${type_pattern}feature(\([^)]*\))?!?:"; then
	bump=minor
elif printf '%s\n' "$subjects" | grep -Eq "${type_pattern}fix(\([^)]*\))?!?:"; then
	bump=patch
else
	printf 'tag=\nbump=none\n'
	exit 0
fi

IFS=. read -r major minor patch <<VERSION
${last#v}
VERSION
case "$bump" in
	minor) minor=$((minor + 1)) patch=0 ;;
	patch) patch=$((patch + 1)) ;;
esac
printf 'tag=v%s.%s.%s\nbump=%s\n' "$major" "$minor" "$patch" "$bump"
