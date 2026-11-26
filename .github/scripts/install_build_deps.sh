#!/bin/sh
# Check the lockfile against web/package.json's allowScripts, then install with
# all lifecycle scripts disabled. npm verifies tarballs against the lockfile.
set -eu
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd -P)
cd "$ROOT"
case ${1-} in ''|-f) ;; *) echo "Usage: $0 [-f]" >&2; exit 1 ;; esac
node .github/scripts/check_web_install_scripts.mjs
npm ci --prefix web --ignore-scripts
