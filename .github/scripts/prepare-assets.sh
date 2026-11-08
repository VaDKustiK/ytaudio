#!/usr/bin/env bash
set -euo pipefail
# Developer/media-test downloads only. Never called by the release workflow.
python3 scripts/prepare_assets.py --os "$GOOS" --arch "$GOARCH" --output "$ASSET_DIR"
