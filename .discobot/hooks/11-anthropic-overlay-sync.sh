#!/bin/bash
#---
# name: Anthropic overlay sync
# type: file
# pattern: modelsdev/model-overlay.json
# notify_llm: false
#---
set -euo pipefail

exec go run ./modelsdev/cmd/anthropic-overlay-sync
