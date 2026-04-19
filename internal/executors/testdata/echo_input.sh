#!/bin/bash
# echo the received stdin back as data.received
payload=$(cat)
printf '{"ok": true, "data": {"received": %s}}' "$payload"
