"""AI-generated snippet: path traversal — user filename in open()."""
import os


def read_user_file(filename):
    with open("/data/" + filename) as f:
        return f.read()
