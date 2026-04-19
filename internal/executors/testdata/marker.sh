#!/bin/bash
cat >/dev/null
echo "log line A"
echo "log line B"
echo "<<<SHELBY_OUT"
echo '{"ok": true, "data": {"via": "marker"}}'
echo "SHELBY_OUT>>>"
echo "trailing log"
