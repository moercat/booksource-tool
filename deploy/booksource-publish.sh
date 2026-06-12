#!/usr/bin/env bash
set -euo pipefail

SSH_KEY=/home/user/.ssh/deploy_key
REMOTE_DIR=user@your-server-ip:/opt/filedown/var

publish_if_changed() {
  local source=$1
  local target_name=$2
  local state=$3

  if [[ ! -s "$source" ]]; then
    echo "source missing or empty: $source"
    return 0
  fi

  local current
  current=$(sha256sum "$source" | awk '{print $1}')
  local last=""
  if [[ -f "$state" ]]; then
    last=$(cat "$state")
  fi

  if [[ "$current" == "$last" ]]; then
    echo "unchanged: $target_name $current"
    return 0
  fi

  scp -i "$SSH_KEY" -o BatchMode=yes -o StrictHostKeyChecking=accept-new -q "$source" "$REMOTE_DIR/$target_name"
  printf '%s\n' "$current" > "$state"
  echo "published: $target_name $current"
}

publish_if_changed \
  /opt/booksource-tool/output/usable_dedup_sources.json \
  legado-usable-book-sources.json \
  /opt/booksource-tool/output/.usable_dedup_sources.published.sha256
