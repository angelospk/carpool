#!/usr/bin/env python3
"""Serve the exported web build, without letting a browser cache it.

python -m http.server sends Last-Modified and nothing else, so a browser is
free to keep serving the bundle it already has. That turns every fix into an
argument about whether it was deployed: the page loads, behaves like the old
build, and the access log stays empty because no request is made at all.

For a test build that is never the trade you want. Correctness beats a second
of load time, so everything goes out as no-store.

  python3 scripts/serve-web.py <dir> [port]
"""
import functools
import http.server
import os
import sys


class NoCache(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cache-Control", "no-store, must-revalidate")
        self.send_header("Pragma", "no-cache")
        self.send_header("Expires", "0")
        super().end_headers()

    def send_head(self):
        # SimpleHTTPRequestHandler answers 304 from If-Modified-Since before any
        # response header above is consulted, which would hand back the copy the
        # browser already has: exactly what this is here to stop.
        for conditional in ("If-Modified-Since", "If-None-Match"):
            del self.headers[conditional]

        # The export is one page and the router runs in the browser, so only /
        # exists on disk. Without this, reloading on a trip, or opening a link
        # to one, answers "File not found" from a file server the person
        # reading it has no idea is involved.
        path = self.translate_path(self.path)
        if not os.path.exists(path) and "." not in os.path.basename(self.path.split("?")[0]):
            self.path = "/"
        return super().send_head()


def main() -> int:
    directory = sys.argv[1] if len(sys.argv) > 1 else "."
    port = int(sys.argv[2]) if len(sys.argv) > 2 else 8099
    handler = functools.partial(NoCache, directory=directory)
    with http.server.ThreadingHTTPServer(("0.0.0.0", port), handler) as httpd:
        print(f"serving {directory} on 0.0.0.0:{port}, no-store", flush=True)
        httpd.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
