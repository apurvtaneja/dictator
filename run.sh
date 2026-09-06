#!/bin/sh
# Run ChatGPT Dictate from anywhere.
cd "$(dirname "$0")"
exec /usr/bin/python3 -m chatgpt_dictate "$@"
