"""
Shelby script — Mission Control user stats
Paginates through the Supabase Auth admin users API and returns
aggregated counts for the dashboard.
"""
import json
import sys
import urllib.request
import urllib.error
from datetime import datetime, timezone

def supa_req(base_url, service_key, path):
    req = urllib.request.Request(
        f"{base_url}{path}",
        headers={
            "Authorization": f"Bearer {service_key}",
            "apikey": service_key,
        },
    )
    with urllib.request.urlopen(req, timeout=15) as resp:
        return json.loads(resp.read().decode())

def main():
    payload = json.load(sys.stdin)
    env = payload.get("env", {})
    base_url = env.get("SUPABASE_URL", "").rstrip("/")
    service_key = env.get("SUPABASE_SERVICE_ROLE_KEY", "")

    if not base_url or not service_key:
        print(json.dumps({"ok": False, "data": {}, "error": "Missing env vars"}))
        return

    # Paginate through all users
    all_users = []
    page = 1
    while True:
        data = supa_req(base_url, service_key, f"/auth/v1/admin/users?page={page}&per_page=1000")
        users = data.get("users", [])
        all_users.extend(users)
        if len(users) < 1000:
            break
        page += 1

    now = datetime.now(timezone.utc)
    today_str = now.strftime("%Y-%m-%d")

    # 7-day window
    from datetime import timedelta
    week_ago = (now - timedelta(days=7)).isoformat()

    new_today = 0
    new_this_week = 0
    with_jukebox = 0

    for u in all_users:
        created = u.get("created_at", "")
        if created.startswith(today_str):
            new_today += 1
        if created >= week_ago:
            new_this_week += 1
        meta = u.get("user_metadata") or {}
        playlists = meta.get("playlists")
        if isinstance(playlists, list) and len(playlists) > 0:
            with_jukebox += 1

    total = len(all_users)
    without_jukebox = total - with_jukebox

    print(json.dumps({
        "ok": True,
        "data": {
            "total_users": total,
            "new_today": new_today,
            "new_this_week": new_this_week,
            "with_jukebox": with_jukebox,
            "without_jukebox": without_jukebox,
        },
    }))

if __name__ == "__main__":
    try:
        main()
    except Exception as e:
        print(json.dumps({"ok": False, "data": {}, "error": str(e)}))
