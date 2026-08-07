"""AI-generated snippet: command injection via os.system with user input."""
import os


def run_command(request):
    os.system(request.args.get("cmd"))
