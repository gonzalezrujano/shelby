#!/usr/bin/env python3
"""Shelby script example: read ScriptRequest from stdin, emit ScriptResponse on stdout."""
import json
import sys


def main() -> None:
    req = json.load(sys.stdin)
    inp = req.get("input") or {}
    latency = float(inp.get("latency", 0))
    disk = float(inp.get("disk", 0))

    # toy health score: lower latency + lower disk usage = higher score
    score = max(0.0, 100.0 - (latency / 10.0) - disk)

    # stderr = logs (Shelby captures but doesn't parse)
    print(f"enrich: lat={latency} disk={disk} -> score={score}", file=sys.stderr)

    resp = {
        "ok": True,
        "data": {
            "score": round(score, 2),
            "note": "healthy" if score > 50 else "degraded",
            "inputs": {"latency": latency, "disk": disk},
        },
        "metrics": {"custom_ms": 1},
    }
    sys.stdout.write("<<<SHELBY_OUT\n")
    json.dump(resp, sys.stdout)
    sys.stdout.write("\nSHELBY_OUT>>>\n")


if __name__ == "__main__":
    main()
