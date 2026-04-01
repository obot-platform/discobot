#!/bin/bash
#---
# name: OpenAI overlay sync
# type: file
# pattern: modelsdev/model-overlay.json
# notify_llm: false
#---
set -euo pipefail

exec go run ./modelsdev/cmd/openai-overlay-sync -concurrency=10
