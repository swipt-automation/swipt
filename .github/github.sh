retry() {
  local output errors code
  output=$(mktemp)
  errors=$(mktemp)
  until "$@" > "$output" 2> "$errors"; do
    code=$?
    cat "$errors" >&2
    # gh api reports an error response with an empty JSON body as "unexpected end of JSON input"
    # (gh 2.104.0, pkg/cmd/api/api.go:495-500, 657-671).
    grep -qE 'HTTP 5[0-9]{2}|unexpected end of JSON input|returned error: 5[0-9]{2}|Internal Server Error|fatal error in commit_refs|HTTP 404: Not Found \(https://uploads\.github\.com/' "$errors" || return "$code"
    sleep 10
  done
  cat "$output"
  cat "$errors" >&2
}

delete() {
  local errors
  errors=$(gh api -X DELETE "$1" 2>&1 > /dev/null) && return
  [[ "$errors" == *"HTTP 404"* ]] && return
  echo "$errors" >&2
  return 1
}

# gh release view and download report a failed draft lookup as "release not found" when the published
# lookup finds nothing (gh 2.104.0, pkg/cmd/release/shared/fetch.go:183-224), so look releases up with gh api.
drafts() {
  gh api "repos/$GITHUB_REPOSITORY/releases?per_page=100" --jq ".[] | select(.draft and .tag_name == \"$1\") | .url"
}

create_draft() {
  local tag=$1 stale draft
  stale=$(drafts "$tag") || return
  for draft in $stale; do
    delete "$draft" || return
  done
  gh release create "$tag" --draft "${@:2}" > /dev/null || return
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    draft=$(drafts "$tag") || return
    if [ -n "$draft" ]; then
      echo "$draft"
      return
    fi
    sleep 3
  done
  echo "the draft $tag is not in the release list" >&2
  return 1
}
