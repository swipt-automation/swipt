#!/usr/bin/env bash

set -euo pipefail

VERSION=$1
TAG=$2
# The CLI's optional dependencies name the driver packages, one for each target.
mapfile -t TARGETS < <(jq -r '.optionalDependencies | keys[] | ltrimstr("@swipt/driver-")' cli/package.json)
npm install -g npm@latest
optional=()
for target in "${TARGETS[@]}"; do
  os=${target%-*}
  cpu=${target#*-}
  package=build/driver/$target
  executable=$(cd "$package" && echo swipt-driver*)
  # Artifacts lose the executable bit.
  chmod 755 "$package/$executable"
  jq -n --arg target "$target" --arg os "$os" --arg cpu "$cpu" --arg executable "$executable" --arg version "$VERSION" '{name: "@swipt/driver-\($target)", version: $version, license: "GPL-3.0-or-later",
    repository: {type: "git", url: "https://github.com/swipt-automation/swipt.git", directory: "cmd/swipt-driver"},
    os: [$os], cpu: [$cpu], bin: {"swipt-driver": $executable}, files: [$executable], publishConfig: {access: "public"}}
    + if $os == "linux" then {libc: ["glibc"]} else {} end' \
    > "$package/package.json"
  optional+=("optionalDependencies.@swipt/driver-$target=$VERSION")
done
# The Developer Disk Image has its own version, and each version is published once.
DISK_IMAGE=Swipt/Resources/DeveloperDiskImage
DISK_IMAGE_VERSION=$(node -p "require('./$DISK_IMAGE/package.json').version")
if npm view "@swipt/developer-disk-image@$DISK_IMAGE_VERSION" version > /dev/null 2>&1; then
  echo "@swipt/developer-disk-image@$DISK_IMAGE_VERSION is already published"
else
  npm publish "./$DISK_IMAGE"
fi
npm pkg set "version=$VERSION" --prefix api
npm pkg set "version=$VERSION" "peerDependencies.@swipt/api=$VERSION" "${optional[@]}" --prefix cli
for package in api "${TARGETS[@]/#/build/driver/}" cli; do
  cp LICENSE "$package/"
  PACKAGE=$(node -p "require('./$package/package.json').name")
  if npm view "$PACKAGE@$VERSION" version > /dev/null 2>&1; then
    echo "$PACKAGE@$VERSION is already published"
    npm dist-tag add "$PACKAGE@$VERSION" "$TAG"
  else
    npm publish "./$package" --tag "$TAG"
  fi
  # Until a package has a stable release, latest follows its prereleases.
  if [ "$TAG" != latest ]; then
    case "$(npm view "$PACKAGE@latest" version 2> /dev/null || true)" in
      *-*) npm dist-tag add "$PACKAGE@$VERSION" latest ;;
    esac
  fi
done
