#!/usr/bin/env bash
set -euo pipefail

# release.sh — builds, packages, and publishes a versioned after release.
#
# Usage: ./scripts/release.sh <version>
# Example: ./scripts/release.sh v1.4.0

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

info()    { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
success() { printf '\033[1;32m✓\033[0m %s\n' "$*"; }
err()     { printf '\033[1;31mError:\033[0m %s\n' "$*" >&2; exit 1; }
confirm() {
  printf '\n\033[1;33m%s\033[0m [y/N] ' "$*"
  read -r reply
  [[ "$reply" =~ ^[Yy]$ ]] || { echo "Aborted."; exit 1; }
}

# ---------------------------------------------------------------------------
# Argument + format validation
# ---------------------------------------------------------------------------

[[ $# -eq 1 ]] || err "Usage: $0 <version>  (e.g. $0 v1.4.0)"

VERSION="$1"
[[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] \
  || err "Version must be in the format vX.Y.Z (got: $VERSION)"

DIST_DIR="dist/${VERSION}"

# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------

info "Running pre-flight checks"

# Must be run from repo root
[[ -f "go.mod" ]] || err "Must be run from the repository root"

# Required tools
for tool in go gh shasum git tar; do
  command -v "$tool" >/dev/null 2>&1 || err "'$tool' not found in PATH"
done

# Must be on main branch
CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
[[ "$CURRENT_BRANCH" == "main" ]] \
  || err "Must be on main branch (currently on: $CURRENT_BRANCH)"

# Working tree must be clean
[[ -z "$(git status --porcelain)" ]] \
  || err "Working tree is not clean — commit or stash changes before releasing"

# Tag must not already exist
git tag | grep -qx "$VERSION" \
  && err "Tag $VERSION already exists"

# dist dir must not already exist
[[ ! -d "$DIST_DIR" ]] \
  || err "$DIST_DIR already exists — remove it to re-run"

success "Pre-flight checks passed"

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

info "Building binaries for $VERSION"

mkdir -p "$DIST_DIR"

TARGETS=(
  "darwin  amd64"
  "darwin  arm64"
  "linux   amd64"
  "linux   arm64"
)

for target in "${TARGETS[@]}"; do
  read -r GOOS GOARCH <<< "$target"
  BIN="${DIST_DIR}/after_${GOOS}_${GOARCH}"
  GOOS="$GOOS" GOARCH="$GOARCH" go build \
    -ldflags "-X main.version=${VERSION}" \
    -o "$BIN" .
  success "Built $BIN"
done

# ---------------------------------------------------------------------------
# Smoke test
# ---------------------------------------------------------------------------

info "Smoke testing local binary"

LOCAL_OS="$(uname -s)"
LOCAL_ARCH="$(uname -m)"
[[ "$LOCAL_OS" == "Darwin" ]] || err "Smoke test only supported on macOS (got: $LOCAL_OS)"
case "$LOCAL_ARCH" in
  x86_64)  HOST_BIN="${DIST_DIR}/after_darwin_amd64" ;;
  arm64)   HOST_BIN="${DIST_DIR}/after_darwin_arm64" ;;
  *)       err "Unrecognised host architecture: $LOCAL_ARCH" ;;
esac

VERSION_OUTPUT="$("$HOST_BIN" --version)"
[[ "$VERSION_OUTPUT" == "after ${VERSION}" ]] \
  || err "Smoke test failed: expected 'after ${VERSION}', got '${VERSION_OUTPUT}'"

success "Smoke test passed: $VERSION_OUTPUT"

# ---------------------------------------------------------------------------
# Package
# ---------------------------------------------------------------------------

info "Packaging archives"

cd "$DIST_DIR"

for target in "${TARGETS[@]}"; do
  read -r GOOS GOARCH <<< "$target"
  ARCHIVE="after_${VERSION}_${GOOS}_${GOARCH}.tar.gz"
  tar -czf "$ARCHIVE" "after_${GOOS}_${GOARCH}"
  success "Packaged $ARCHIVE"
done

# ---------------------------------------------------------------------------
# Checksums
# ---------------------------------------------------------------------------

info "Generating checksums"

shasum -a 256 after_"${VERSION}"_*.tar.gz > checksums.txt
cat checksums.txt

cd - >/dev/null

success "checksums.txt written to $DIST_DIR"

# ---------------------------------------------------------------------------
# Confirmation gate before remote operations
# ---------------------------------------------------------------------------

echo ""
echo "  Version : $VERSION"
echo "  Dist dir: $DIST_DIR"
echo "  Commits will be tagged and pushed to origin/main."
echo "  A GitHub release will be created with the above artifacts."
echo ""

# Determine release notes file
NOTES_FILE="${DIST_DIR}/release-note-${VERSION}.md"
if [[ -f "$NOTES_FILE" ]]; then
  info "Release notes: $NOTES_FILE"
else
  NOTES_FILE=""
  printf '\033[1;33mWarning:\033[0m No release notes file found at %s — release will have no body.\n' \
    "${DIST_DIR}/release-note-${VERSION}.md"
fi

confirm "Proceed with tagging, pushing, and publishing the GitHub release?"

# ---------------------------------------------------------------------------
# Tag + push
# ---------------------------------------------------------------------------

info "Tagging $VERSION"
git tag "$VERSION"

info "Pushing commits and tag to origin"
git push origin main
git push origin "$VERSION"

success "Tag $VERSION pushed"

# ---------------------------------------------------------------------------
# GitHub release
# ---------------------------------------------------------------------------

info "Creating GitHub release $VERSION"

ARTIFACTS=(
  "${DIST_DIR}/after_${VERSION}_darwin_amd64.tar.gz"
  "${DIST_DIR}/after_${VERSION}_darwin_arm64.tar.gz"
  "${DIST_DIR}/after_${VERSION}_linux_amd64.tar.gz"
  "${DIST_DIR}/after_${VERSION}_linux_arm64.tar.gz"
  "${DIST_DIR}/checksums.txt"
)

GH_ARGS=(release create "$VERSION" --title "$VERSION")
if [[ -n "$NOTES_FILE" ]]; then
  GH_ARGS+=(--notes-file "$NOTES_FILE")
else
  GH_ARGS+=(--notes "")
fi
GH_ARGS+=("${ARTIFACTS[@]}")

gh "${GH_ARGS[@]}"

success "GitHub release $VERSION published"

# ---------------------------------------------------------------------------
# Homebrew reminder
# ---------------------------------------------------------------------------

DARWIN_AMD64_SHA="$(awk '/darwin_amd64/ {print $1}' "${DIST_DIR}/checksums.txt")"
DARWIN_ARM64_SHA="$(awk '/darwin_arm64/ {print $1}' "${DIST_DIR}/checksums.txt")"
LINUX_AMD64_SHA="$(awk '/linux_amd64/  {print $1}' "${DIST_DIR}/checksums.txt")"
LINUX_ARM64_SHA="$(awk '/linux_arm64/  {print $1}' "${DIST_DIR}/checksums.txt")"

printf '\n\033[1;33m==> Homebrew formula update required (homebrew-tools/Formula/after.rb)\033[0m\n'
printf '  version "%s"\n\n' "${VERSION#v}"
printf '  on_macos / on_intel  url  .../after_%s_darwin_amd64.tar.gz\n' "$VERSION"
printf '                       sha256 "%s"\n\n' "$DARWIN_AMD64_SHA"
printf '  on_macos / on_arm    url  .../after_%s_darwin_arm64.tar.gz\n' "$VERSION"
printf '                       sha256 "%s"\n\n' "$DARWIN_ARM64_SHA"
printf '  on_linux / on_intel  url  .../after_%s_linux_amd64.tar.gz\n' "$VERSION"
printf '                       sha256 "%s"\n\n' "$LINUX_AMD64_SHA"
printf '  on_linux / on_arm    url  .../after_%s_linux_arm64.tar.gz\n' "$VERSION"
printf '                       sha256 "%s"\n\n' "$LINUX_ARM64_SHA"
printf '  test: assert_match "after %s"\n' "$VERSION"
printf '\nCommit message: after: update formula to %s\n' "$VERSION"
