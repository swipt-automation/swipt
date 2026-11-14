#!/usr/bin/env bash

set -euo pipefail

XCODE=$(
  for app in /Applications/Xcode*.app; do
    [ -L "$app" ] && continue
    # licenseType is "GM" or "Beta" (Xcode 27.2 27B52, SharedFrameworks/DVTSystemPrerequisites.framework).
    [[ "$(plutil -extract licenseType raw "$app/Contents/Resources/LicenseInfo.plist")" == *Beta* ]] && continue
    printf '%s\t%s\n' "$(plutil -extract CFBundleShortVersionString raw "$app/Contents/version.plist")" "$app"
  done | sort -V | tail -1 | cut -f 2
)
if [ -z "$XCODE" ]; then
  echo "no stable Xcode found" >&2
  exit 1
fi
echo "$XCODE"
echo "DEVELOPER_DIR=$XCODE/Contents/Developer" >> "$GITHUB_ENV"
