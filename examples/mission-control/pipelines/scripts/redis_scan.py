"""
Shelby script — Mission Control Redis full SCAN
Reads all Upstash Redis keys via REST API, categorizes by prefix,
and returns aggregated stats for the dashboard.
"""
import json
import sys
import urllib.request
import urllib.error

def redis_req(base_url, token, path):
    req = urllib.request.Request(
        f"{base_url}/{path}",
        headers={"Authorization": f"Bearer {token}"},
    )
    with urllib.request.urlopen(req, timeout=10) as resp:
        return json.loads(resp.read().decode())

def main():
    payload = json.load(sys.stdin)
    env = payload.get("env", {})
    base_url = env.get("KV_REST_API_URL", "").rstrip("/")
    token = env.get("KV_REST_API_TOKEN", "")

    if not base_url or not token:
        print(json.dumps({"ok": False, "data": {}, "error": "Missing env vars"}))
        return

    # Full SCAN — iterate until cursor returns to 0
    all_keys = []
    cursor = "0"
    while True:
        result = redis_req(base_url, token, f"scan/{cursor}/count/200")
        cursor, batch = result["result"]
        all_keys.extend(batch)
        if str(cursor) == "0":
            break

    # Categorize keys by prefix pattern
    playlist_keys = []
    counts = {
        "settings": 0,
        "bans": 0,
        "votes": 0,
        "autoplay": 0,
        "cache": 0,
    }

    for key in all_keys:
        if key.startswith("settings:"):
            counts["settings"] += 1
        elif key.startswith("ban:"):
            counts["bans"] += 1
        elif key.startswith("votes:"):
            counts["votes"] += 1
        elif key.startswith("autoplay:"):
            counts["autoplay"] += 1
        elif key.startswith("top_hits") or key.startswith("rec_cache"):
            counts["cache"] += 1
        else:
            # Bare key = jukebox queue (Redis List)
            playlist_keys.append(key)

    # Get queue length for each active jukebox
    total_songs = 0
    for key in playlist_keys:
        try:
            result = redis_req(base_url, token, f"llen/{key}")
            total_songs += int(result.get("result", 0))
        except Exception:
            pass

    print(json.dumps({
        "ok": True,
        "data": {
            "active_playlists": len(playlist_keys),
            "total_songs": total_songs,
            "total_keys": len(all_keys),
            "key_settings": counts["settings"],
            "key_bans": counts["bans"],
            "key_votes": counts["votes"],
            "key_autoplay": counts["autoplay"],
            "key_cache": counts["cache"],
        },
    }))

if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(json.dumps({"ok": False, "data": {}, "error": str(e)}))
