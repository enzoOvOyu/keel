#!/bin/sh
# Package self-check with synthetic inputs; proves neither platform nor PXE readiness.
set -eu
cd "$(dirname "$0")/.."
python3 -m unittest discover -s poc/h1 -p 'test_*.py'
printf '%s\n' 'PACKAGE_SELF_CHECK_ONLY: synthetic validator checks passed; platform, PXE and hardware readiness remain unverified.'
