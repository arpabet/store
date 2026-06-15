#!/usr/bin/env bash
#
# Coordinated release for a go.arpabet.com multi-module repository.
#
# Repo-agnostic: the module prefix is auto-detected from go.mod, so this exact
# script works unchanged in every repo (servion, sprint, store, record, ...).
# Keep it byte-identical across repos.
#
# One shared version moves every module; an interface change ripples into all of
# them. A module carrying an extra change takes a higher patch via a per-module
# override (keyed by subdir, "." for the root module):
#
#     ./release.sh v1.3.0 grpc=v1.3.1
#
# Modules are discovered automatically (every dir with a go.mod, excluding
# examples). The root module is tagged "vX.Y.Z"; each submodule "<subdir>/vX.Y.Z".
# Before tagging, internal `require <prefix>/X` lines are pinned to the release
# version and local-dev `replace <prefix>/X => ../X` bootstrap directives are
# stripped (consumers ignore replaces; this keeps published go.mods clean).
# go.work covers local dev post-release.
#
# Re-runs are safe: existing tags are skipped and an empty release commit is
# tolerated, so a newly added submodule can be tagged at an already-released
# shared version.
#
# Usage: ./release.sh [--dry-run] [--no-push] <version> [module=version ...]
#
#     --dry-run   print the plan + go.mod diff and exit, change nothing
#     --no-push   create the commit and tags locally but do not push
#
# Compatible with the bash 3.2 that ships on macOS (no associative arrays/mapfile).
#
set -euo pipefail

REMOTE="origin"
DRY_RUN=0; NO_PUSH=0
VERSION=""; OVERRIDES=""

die() { echo "error: $*" >&2; exit 1; }
semver_ok() { case "$1" in v[0-9]*.[0-9]*.[0-9]*) return 0;; *) return 1;; esac; }
four_part() { case "$1" in v[0-9]*.[0-9]*.[0-9]*.[0-9]*) return 0;; *) return 1;; esac; }

for a in "$@"; do
	case "$a" in
		--dry-run) DRY_RUN=1 ;;
		--no-push) NO_PUSH=1 ;;
		-h|--help) awk 'NR>1 && /^#/{sub(/^# ?/,"");print;next} NR>1{exit}' "$0"; exit 0 ;;
		*=v*)      OVERRIDES="$OVERRIDES $a" ;;
		v*)        VERSION="$a" ;;
		*)         die "unrecognized arg: $a" ;;
	esac
done
[ -n "$VERSION" ] || die "usage: ./release.sh [--dry-run] [--no-push] <version> [module=version ...]"
semver_ok "$VERSION" || die "'$VERSION' is not vMAJOR.MINOR.PATCH"
! four_part "$VERSION" || die "'$VERSION' has four numbers; Go requires vX.Y.Z. Use a higher patch override for the module that changed."
for tok in $OVERRIDES; do
	v="${tok#*=}"
	semver_ok "$v" || die "override '$tok' version is not vMAJOR.MINOR.PATCH"
	! four_part "$v" || die "override '$tok' has four numbers; use vX.Y.Z."
done

# release version for a module key (subdir, or "." for the root module)
ver_for() {
	local tok
	for tok in $OVERRIDES; do
		case "$tok" in "$1="*) echo "${tok#*=}"; return;; esac
	done
	echo "$VERSION"
}

cd "$(dirname "$0")"
[ -d .git ] || die "must run from the repository root (no .git here)."
[ -f go.work ] || echo "warning: no go.work at repo root."
[ -z "$(git status --porcelain)" ] || die "working tree is dirty; commit or stash first."

branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "main" ] || echo "warning: on branch '$branch', not 'main'."

# discover module keys: "." for the root module, "<subdir>" for each submodule;
# examples are never their own published modules.
MODULES="$(find . -name go.mod -not -path './.*' -not -path '*/examples/*' \
	| sed 's#/go.mod$##; s#^\./##' | sort)"
[ -n "$MODULES" ] || die "no modules found"

# auto-detect the module prefix from the first module's go.mod
first="$(printf '%s\n' "$MODULES" | head -1)"
firstmod="$(sed -n 's/^module[[:space:]]\{1,\}//p' "$first/go.mod" | head -1)"
[ -n "$firstmod" ] || die "could not read module path from $first/go.mod"
if [ "$first" = "." ]; then PREFIX="$firstmod"; else PREFIX="${firstmod%/$first}"; fi
echo "module prefix: $PREFIX"

# go import path / git tag for a module key
mod_path() { case "$1" in .) echo "$PREFIX";; *) echo "$PREFIX/$1";; esac; }
tag_for()  { case "$1" in .) echo "$2";;     *) echo "$1/$2";;        esac; }

echo "Release plan (shared $VERSION):"
for m in $MODULES; do
	t="$(tag_for "$m" "$(ver_for "$m")")"
	if git rev-parse -q --verify "refs/tags/$t" >/dev/null 2>&1; then
		printf "  %-22s -> %s (exists, will skip)\n" "$m" "$t"
	else
		printf "  %-22s -> %s\n" "$m" "$t"
	fi
done
echo

# rewrite each go.mod: strip bootstrap replaces, pin internal requires
for m in $MODULES; do
	gm="$m/go.mod"
	perl -i -ne "print unless m{^replace \Q$PREFIX\E(/|\s)}" "$gm"
	for dep in $MODULES; do
		dpath="$(mod_path "$dep")"
		dv="$(ver_for "$dep")"
		perl -i -pe "s{(\Q$dpath\E)\s+v\S+}{\$1 $dv}g" "$gm"
	done
done

if [ "$DRY_RUN" -eq 1 ]; then
	echo "--- dry run: go.mod changes below, nothing committed ---"
	git --no-pager diff -- '*go.mod' || true
	git checkout -- . 2>/dev/null || true
	exit 0
fi

git add -A
if git diff --cached --quiet; then
	# go.mods were already in their released form (no replaces to strip, requires
	# already pinned), so there is nothing to commit — tag the current HEAD.
	echo "no go.mod changes to commit; tagging current HEAD"
else
	git commit -m "release $VERSION"
fi

# Create only the tags that don't exist yet, so re-runs add missing module tags
# (e.g. a new submodule at an already-released shared version) instead of aborting.
TAGS=""
for m in $MODULES; do
	t="$(tag_for "$m" "$(ver_for "$m")")"
	if git rev-parse -q --verify "refs/tags/$t" >/dev/null 2>&1; then
		echo "tag $t already exists; skipping"
		continue
	fi
	git tag -a "$t" -m "$t"
	TAGS="$TAGS $t"
	echo "tagged $t"
done

if [ -z "$TAGS" ]; then
	echo "no new tags to create; nothing to release"
	exit 0
fi

if [ "$NO_PUSH" -eq 1 ]; then
	echo "--no-push: created tag(s) locally; not pushed:$TAGS"
	echo "  git push $REMOTE $branch && git push $REMOTE $TAGS"
	exit 0
fi
git push "$REMOTE" "$branch"
git push "$REMOTE" $TAGS
echo "released $VERSION"
