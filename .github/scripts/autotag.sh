#!/usr/bin/env bash
# autotag.sh <prefix> <base-version> <base-ref> <pathspec>...
#
# Tags HEAD <prefix><next version> when a commit since the module's last tag
# touched <pathspec> and its Conventional Commit type calls for a release:
# `feat` bumps the minor, and `fix`, `perf`, `refactor` or `revert` the patch.
# Other types (docs, chore, ci, test, ...) release nothing. A breaking change
# (`type!:` or a `BREAKING CHANGE:` footer) fails instead: a Go major release
# needs a /vN module path, which is a deliberate, manual change.
#
# With no tag yet, the module continues from <base-version>, the upstream
# release the fork was taken from, counting commits after <base-ref> (that
# release's commit; empty when the module has no upstream history here).
#
# Prints `tag=<tag>` (or nothing) for $GITHUB_OUTPUT; DRY_RUN=1 skips the push.
set -euo pipefail

prefix=$1
base=$2
base_ref=$3
shift 3
pathspec=("$@")

last=$(git tag --list "${prefix}[0-9]*" --sort=-v:refname | head -n1)
if [[ -n $last ]]; then
	version=${last#"$prefix"}
	range=("$last..HEAD")
elif [[ -n $base_ref ]]; then
	version=$base
	range=("$base_ref..HEAD")
else
	version=$base
	range=(HEAD)
fi

bump=none
rank() { case $1 in minor) echo 2 ;; patch) echo 1 ;; *) echo 0 ;; esac; }
raise() { if (($(rank "$1") > $(rank "$bump"))); then bump=$1; fi; }

while IFS= read -r -d $'\x1e' message; do
	subject=$(printf '%s\n' "$message" | sed -n '/./{p;q;}')
	type=$(printf '%s' "$subject" | sed -nE 's/^([a-z]+)(\([^)]*\))?(!)?: .*/\1\3/p')
	if [[ $type == *! ]] || printf '%s\n' "$message" | grep -qE '^BREAKING[ -]CHANGE: '; then
		echo "breaking change for ${prefix}: \"$subject\" needs a manual major release with a /vN module path" >&2
		exit 1
	fi
	case $type in
	feat) raise minor ;;
	fix | perf | refactor | revert) raise patch ;;
	esac
done < <(git log --format='%B%x1e' "${range[@]}" -- "${pathspec[@]}")

if [[ $bump == none ]]; then
	echo "no releasable commits for ${prefix} since ${last:-the fork base v$base}" >&2
	exit 0
fi

IFS=. read -r major minor patch <<<"$version"
case $bump in
minor) next="$major.$((minor + 1)).0" ;;
patch) next="$major.$minor.$((patch + 1))" ;;
esac
tag="${prefix}${next}"

echo "tagging $tag ($bump bump from ${last:-v$base})" >&2
if [[ ${DRY_RUN:-} != 1 ]]; then
	git tag -a "$tag" -m "$tag"
	git push origin "refs/tags/$tag"
fi
echo "tag=$tag"
