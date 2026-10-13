#!/usr/bin/env python3
"""Collect one server's model inventory for scan-import (FR-14.4).

Runs ON the target machine (PoC execution-package form, ADR-14 mock-first:
the production channel arrives with WP-1.5 dual-channel runner). Reads only
local hardware inventory sources, emits the standard manifest JSON accepted
by POST /api/v1/projects/{id}/models/import, and exits non-zero listing what
is missing when a source is unavailable.

Usage: collect-model-inventory.py [--host-name NAME] > inventory.json
"""
import argparse
import json
import os
import re
import subprocess
import sys


def sh(*cmd):
    try:
        return subprocess.run(cmd, capture_output=True, text=True, timeout=30).stdout.strip()
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return ""


def read(path):
    try:
        with open(path) as f:
            return f.read().strip()
    except OSError:
        return ""


def collect_slots():
    """NIC slots from sysfs: (pci address, slot label guess, current model)."""
    slots = []
    base = "/sys/class/net"
    for name in sorted(os.listdir(base)) if os.path.isdir(base) else []:
        link = os.path.realpath(os.path.join(base, name))
        m = re.search(r"/([0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-9a-f])$", link)
        if not m:
            continue
        pci = m.group(1)
        model = read(os.path.join(os.path.dirname(link), "device", "modalias")) or ""
        slots.append({"pci": pci, "net": name, "model": model})
    return slots


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--host-name", default=read("/etc/hostname") or os.uname().nodename)
    args = parser.parse_args()

    missing = []
    slots = collect_slots()
    if not slots:
        missing.append("no NIC devices found under /sys/class/net")

    dmidecode = sh("dmidecode", "-t", "system") if os.geteuid() == 0 else ""
    if not dmidecode:
        missing.append("dmidecode unavailable (run as root for vendor/product fields)")

    vendor = product = ""
    for line in dmidecode.splitlines():
        if line.startswith("Manufacturer:"):
            vendor = line.split(":", 1)[1].strip()
        elif line.startswith("Product Name:"):
            product = line.split(":", 1)[1].strip()

    gpus = []
    query = sh("nvidia-smi", "--query-gpu=name", "--format=csv,noheader")
    for line in query.splitlines():
        if line.strip():
            gpus.append(line.strip())
    if not gpus:
        missing.append("nvidia-smi unavailable or no GPUs")

    manifest = {
        "source_host": args.host_name,
        "model_name": product or None,
        "hw_spec": {
            "vendor": vendor or None,
            "model": product or None,
            "kind": "server",
            "gpu_count": len(gpus),
            "gpu_model": gpus[0] if gpus else None,
            "kernel": os.uname().release,
        },
        "slots": [
            {
                "slot_no": i + 1,
                "pcie_slot": s["pci"],
                "expect_nic_model": s["model"] or "unknown",
                "panel_port": s["net"],
                "default_role": "compute",
            }
            for i, s in enumerate(slots)
        ],
        "_warnings": missing,
    }
    json.dump(manifest, sys.stdout, ensure_ascii=False, indent=2)
    print()
    if missing:
        print(f"NOTE: {len(missing)} source(s) incomplete: {'; '.join(missing)}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
