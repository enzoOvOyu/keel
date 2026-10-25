#!/usr/bin/env python3
"""Run the shared UI fixture on an isolated Vite port, without API or DB writes."""
import os, signal, socket, subprocess, tempfile, time, urllib.request
from pathlib import Path
root = Path(__file__).resolve().parents[1]
with socket.socket() as probe:
    probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    probe.bind(("127.0.0.1", 5178))
work = Path(tempfile.mkdtemp(prefix="keel-shadcn-"))
print("Shared UI evidence directory: " + str(work), flush=True)
with (work / "vite.log").open("w") as log:
    vite = subprocess.Popen(["npm", "run", "dev", "--workspace=@keel/web", "--", "--port", "5178"], cwd=root, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        for _ in range(200):
            if vite.poll() is not None:
                raise RuntimeError("Isolated Vite exited; see evidence directory")
            try:
                urllib.request.urlopen("http://127.0.0.1:5178/test-fixtures/shadcn.html", timeout=1)
                break
            except Exception:
                time.sleep(.1)
        else:
            raise RuntimeError("Isolated Vite did not become ready")
        subprocess.run(["node", "scripts/check-shadcn-browser.mjs"], cwd=root, env={**os.environ, "KEEL_SHADCN_EVIDENCE_DIR": str(work)}, check=True, timeout=120)
    finally:
        os.killpg(vite.pid, signal.SIGTERM)
        try:
            vite.wait(timeout=10)
        except subprocess.TimeoutExpired:
            os.killpg(vite.pid, signal.SIGKILL)
            vite.wait()
