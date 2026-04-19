#!/bin/bash
# consume stdin, echo hardcoded ok response
cat >/dev/null
printf '{"ok": true, "data": {"hello": "world", "n": 42}}'
