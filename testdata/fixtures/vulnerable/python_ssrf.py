"""AI-generated snippet: SSRF — user-controlled URL fetched server-side."""
import requests


def fetch_url(request):
    return requests.get(request.args.get("url"))
