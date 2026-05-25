import httpx


def fetch_news_context(base_url: str, symbol: str) -> dict | None:
    symbol = (symbol or "").strip().upper()
    if not symbol:
        return None
    url = f"{base_url.rstrip('/')}/api/v1/news/sentiment"
    try:
        with httpx.Client(timeout=10.0) as client:
            resp = client.get(url, params={"symbol": symbol})
            if resp.status_code == 404:
                return None
            resp.raise_for_status()
            return resp.json()
    except httpx.HTTPError:
        return None
