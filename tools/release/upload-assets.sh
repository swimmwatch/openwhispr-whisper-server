#!/usr/bin/env bash
set -euo pipefail

checksums="$RUNNER_TEMP/SHA256SUMS"
: > "$checksums"
for file in "$STATE_FILE" "$CHART_DIR"/*.tgz; do
  (
    cd "$(dirname "$file")"
    sha256sum "$(basename "$file")"
  ) >> "$checksums"
done

if gh release view "$RELEASE_TAG" --json isPrerelease,assets > "$RUNNER_TEMP/release.json"; then
  expected=false
  if [ "$RELEASE_STAGE" = candidate ]; then
    expected=true
  fi
  actual=$(jq -r '.isPrerelease' "$RUNNER_TEMP/release.json")
  if [ "$actual" != "$expected" ]; then
    printf '%s\n' 'Release mode differs from the qualified artifacts' >&2
    exit 1
  fi
else
  flags=(--verify-tag --title "$RELEASE_TAG")
  if [ "$RELEASE_STAGE" = candidate ]; then
    flags+=(--prerelease --latest=false --notes 'GPU qualification in Docker and Kubernetes is pending.')
  else
    flags+=(--latest --generate-notes)
  fi
  gh release create "$RELEASE_TAG" "${flags[@]}"
  gh release view "$RELEASE_TAG" --json isPrerelease,assets > "$RUNNER_TEMP/release.json"
fi

for file in "$STATE_FILE" "$CHART_DIR"/*.tgz "$checksums"; do
  name=$(basename "$file")
  if jq -e --arg name "$name" '.assets | any(.name == $name)' "$RUNNER_TEMP/release.json" > /dev/null; then
    existing=$(mktemp -d "$RUNNER_TEMP/release-asset.XXXXXX")
    gh release download "$RELEASE_TAG" --pattern "$name" --dir "$existing"
    cmp "$file" "$existing/$name"
    rm -rf "$existing"
  else
    gh release upload "$RELEASE_TAG" "$file"
  fi
done
