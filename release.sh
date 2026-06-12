#!/usr/bin/env bash
#
# Copyright (c) 2025 Karagatan LLC.
# SPDX-License-Identifier: BUSL-1.1
#
# Tags a coordinated release of the store interface and every provider/middleware
# module in this multi-module repository.
#
# Every module is tagged with the SAME base version (an interface change ripples
# into all of them). A module that carries an extra change can be bumped on its
# own with a per-module override.
#
# Go module tags must be valid 3-component semver: vMAJOR.MINOR.PATCH (optionally
# -prerelease). FOUR-number versions like v1.2.3.4 are NOT valid and are rejected
# by the go tool/proxy -- express "shared version plus an extra change in module X"
# as a higher PATCH for that one module, e.g.:
#
#     ./release.sh v1.3.0 providers/badger=v1.3.1
#
# Tags follow the multi-module convention: the root module is "vX.Y.Z" and every
# submodule is "<subpath>/vX.Y.Z" (e.g. providers/badger/v1.3.0).
#
# Usage:
#     ./release.sh [--dry-run] [--no-push] <version> [module=version ...]
#
#     --dry-run   print the plan (require bumps + tags) and exit, change nothing
#     --no-push   create the commit and tags locally but do not push
#
set -euo pipefail

STORE_MODULE="go.arpabet.com/store"
REMOTE="origin"

die() { echo "error: $*" >&2; exit 1; }

semver_ok() {
	# vMAJOR.MINOR.PATCH with optional -prerelease ; rejects 4+ components
	[[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]
}

validate_version() {
	local v="$1"
	if [[ "$v" =~ ^v[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ ]]; then
		die "'$v' has four numbers; Go requires 3-component semver (vX.Y.Z). Use a higher patch for the module that changed, e.g. providers/badger=vX.Y.(Z+1)."
	fi
	semver_ok "$v" || die "'$v' is not valid semver (expected vMAJOR.MINOR.PATCH)."
}

# ---- parse args ----------------------------------------------------------------

DRY_RUN=0
PUSH=1
BASE=""
declare -a OVERRIDE_ARGS=()

while [[ $# -gt 0 ]]; do
	case "$1" in
		--dry-run) DRY_RUN=1 ;;
		--no-push) PUSH=0 ;;
		-h|--help) sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
		-*) die "unknown flag: $1" ;;
		*) if [[ -z "$BASE" ]]; then BASE="$1"; else OVERRIDE_ARGS+=("$1"); fi ;;
	esac
	shift
done

[[ -n "$BASE" ]] || die "missing version. Usage: ./release.sh [--dry-run] [--no-push] <version> [module=version ...]"
validate_version "$BASE"

# validate overrides up front (bash 3.2 compatible: no associative arrays)
if [[ ${#OVERRIDE_ARGS[@]} -gt 0 ]]; then
	for a in "${OVERRIDE_ARGS[@]}"; do
		[[ "$a" == *=* ]] || die "override '$a' must look like module=version"
		validate_version "${a#*=}"
	done
fi

version_for() { # module path -> version (override or base)
	local m="$1" a key val
	if [[ ${#OVERRIDE_ARGS[@]} -gt 0 ]]; then
		for a in "${OVERRIDE_ARGS[@]}"; do
			key="${a%%=*}"; val="${a#*=}"
			if [[ "$key" == "$m" ]]; then echo "$val"; return; fi
		done
	fi
	echo "$BASE"
}

tag_for() { # module path + version -> git tag
	local m="$1" v="$2"
	if [[ "$m" == "." ]]; then echo "$v"; else echo "$m/$v"; fi
}

# ---- locate repo + modules -----------------------------------------------------

cd "$(dirname "$0")"
[[ -d .git ]] || die "must run from the repository root (no .git here)."
[[ -f go.work ]] || echo "warning: no go.work found at repo root."

# root first, then submodules sorted for stable output
declare -a MODULES=(".")
while IFS= read -r f; do
	d="${f#./}"; d="${d%/go.mod}"
	MODULES+=("$d")
done < <(find . -mindepth 2 -name go.mod | sort)

STORE_VERSION="$(version_for .)"

# ---- preflight -----------------------------------------------------------------

branch="$(git rev-parse --abbrev-ref HEAD)"
[[ "$branch" == "main" ]] || echo "warning: on branch '$branch', not 'main'."

if [[ $DRY_RUN -eq 0 && -n "$(git status --porcelain)" ]]; then
	die "working tree is not clean; commit or stash first."
fi

# refuse to clobber existing tags
for m in "${MODULES[@]}"; do
	tag="$(tag_for "$m" "$(version_for "$m")")"
	if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
		die "tag '$tag' already exists."
	fi
done

# ---- plan ----------------------------------------------------------------------

echo "Release plan (store interface version: $STORE_VERSION)"
echo
printf "  %-22s %-14s %s\n" "MODULE" "TAG" "store require"
for m in "${MODULES[@]}"; do
	v="$(version_for "$m")"
	tag="$(tag_for "$m" "$v")"
	if [[ "$m" == "." ]]; then
		printf "  %-22s %-14s %s\n" "$m" "$tag" "(interface)"
	else
		printf "  %-22s %-14s %s\n" "$m" "$tag" "-> $STORE_VERSION"
	fi
done
echo

if [[ $DRY_RUN -eq 1 ]]; then
	echo "(dry run: no changes made)"
	exit 0
fi

# ---- bump intra-repo store dependency -----------------------------------------

changed=0
for m in "${MODULES[@]}"; do
	[[ "$m" == "." ]] && continue
	if grep -qE "${STORE_MODULE} v" "$m/go.mod"; then
		( cd "$m" && go mod edit -require="${STORE_MODULE}@${STORE_VERSION}" )
		changed=1
	fi
done

if [[ $changed -eq 1 && -n "$(git status --porcelain)" ]]; then
	git add -A
	git commit -m "release ${BASE}: pin ${STORE_MODULE}@${STORE_VERSION}"
	echo "committed go.mod dependency bump"
fi

# ---- create tags ---------------------------------------------------------------

declare -a TAGS=()
for m in "${MODULES[@]}"; do
	tag="$(tag_for "$m" "$(version_for "$m")")"
	git tag -a "$tag" -m "$tag"
	TAGS+=("$tag")
	echo "tagged $tag"
done

# ---- push ----------------------------------------------------------------------

if [[ $PUSH -eq 1 ]]; then
	git push "$REMOTE" "$branch"
	git push "$REMOTE" "${TAGS[@]}"
	echo "pushed branch '$branch' and ${#TAGS[@]} tags to $REMOTE"
else
	echo "skipped push (--no-push). Push manually with:"
	echo "  git push $REMOTE $branch && git push $REMOTE ${TAGS[*]}"
fi

echo "done: released ${BASE}"
