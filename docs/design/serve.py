"""Serve the final reference UI on loopback, with current Git build metadata."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit
import argparse
import json
import subprocess

from build import BUILD_MARKER, DEFAULT_BUILD, ROOT, check

REPO = ROOT.parent.parent


def git(*args):
    result = subprocess.run(["git", "-C", str(REPO), *args], capture_output=True, text=True, encoding="utf-8")
    return result.stdout.strip() if result.returncode == 0 else ""


def metadata():
    commit = git("rev-parse", "HEAD")
    tag = git("describe", "--tags", "--exact-match", "HEAD")
    return {**DEFAULT_BUILD, "commit": commit or "master", "onTag": bool(tag),
            "tag": tag or "v0.0.1", "version": tag.lstrip("vV") if tag else "0.0.1"}


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        route = urlsplit(self.path).path.rstrip("/") or "/"
        if route == "/favicon.ico":
            self.send_response(204)
            self.end_headers()
            return
        files = {
            "/": ("preview/index.html", "text/html; charset=utf-8"),
            "/admin": ("preview/admin.html", "text/html; charset=utf-8"),
            "/preview/index.html": ("preview/index.html", "text/html; charset=utf-8"),
            "/preview/admin.html": ("preview/admin.html", "text/html; charset=utf-8"),
            "/vendor/lucide.js": ("vendor/lucide.js", "text/javascript; charset=utf-8"),
        }
        if route not in files:
            self.send_error(404)
            return
        name, content_type = files[route]
        data = (ROOT / name).read_bytes()
        if name.endswith(".html"):
            text = data.decode("utf-8")
            text = text.replace(BUILD_MARKER + json.dumps(DEFAULT_BUILD), json.dumps(metadata()))
            data = text.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        self.wfile.write(data)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=52771)
    args = parser.parse_args()
    check()
    print(f"Home: http://127.0.0.1:{args.port}/\nAdmin: http://127.0.0.1:{args.port}/admin", flush=True)
    try:
        ThreadingHTTPServer(("127.0.0.1", args.port), Handler).serve_forever()
    except KeyboardInterrupt:
        pass
