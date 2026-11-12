#!/bin/sh
# Tags a new version and pushes it: the release workflow builds and publishes it.
#   ./release.sh            next patch version (v1.2.3 -> v1.2.4)
#   ./release.sh minor      v1.2.3 -> v1.3.0
#   ./release.sh major      v1.2.3 -> v2.0.0
#   ./release.sh v1.5.0-rc1 a given version (with a "-": a pre-release)
set -eu

fail() { printf 'release: %s\n' "$*" >&2; exit 1; }

cd "$(dirname "$0")"
bump="${1:-patch}"

[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || fail "not on main"
[ -z "$(git status --porcelain)" ] || fail "uncommitted changes"
git fetch --quiet --tags origin main
[ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] || fail "main is not in sync with origin/main (pull / push first)"

last="$(git tag --list 'v[0-9]*' --sort=-v:refname | grep -v -- - | head -n1)"
last="${last:-v0.0.0}"
IFS=. read -r major minor patch <<EOF
${last#v}
EOF
case "$bump" in
  patch) next="v$major.$minor.$((patch + 1))" ;;
  minor) next="v$major.$((minor + 1)).0" ;;
  major) next="v$((major + 1)).0.0" ;;
  v[0-9]*) next="$bump" ;;
  *) fail "usage: ./release.sh [patch|minor|major|vX.Y.Z]" ;;
esac
git rev-parse -q --verify "refs/tags/$next" >/dev/null && fail "$next already exists"

echo "running the tests..."
make test >/dev/null || fail "tests fail"

printf 'last release: %s\nnew release:  %s\n\n' "$last" "$next"
git log --oneline "$last..HEAD" 2>/dev/null || git log --oneline
printf '\nTag and push %s? [y/N] ' "$next"
read -r answer || answer=""
case "$answer" in y | Y | yes) ;; *) fail "cancelled" ;; esac

git tag -a "$next" -m "$next"
git push origin "$next"
echo "pushed $next: https://github.com/$(git remote get-url origin | sed -E 's#(git@github.com:|https://github.com/)##; s#\.git$##')/actions"