"""Clean snippet: pure JSON helpers — no network, shell, or eval."""
import json


def to_json(data):
    return json.dumps(data, indent=2)


def from_json(text):
    return json.loads(text)
