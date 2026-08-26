#!/usr/bin/env bash
# Vendor the Luanti mod into the DoggioWars game, so a streamer gets the
# integration with the game build and installs no mod of their own.
#
# The bridge repo stays the source of truth; this only copies one way.
# Usage: tools/sync-to-doggiowars.sh [path-to-DoggioWars]
set -euo pipefail
SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/mod/chatbridge"
DEST="${1:-/Volumes/YOTTA/Dev/DoggioFight}/mods/chatbridge"

[ -d "$SRC" ] || { echo "chybí zdroj: $SRC" >&2; exit 1; }
[ -d "$(dirname "$DEST")" ] || { echo "chybí cíl: $(dirname "$DEST")" >&2; exit 1; }

rm -rf "$DEST"
cp -r "$SRC" "$DEST"

# The copy carries a header so nobody edits the vendored file by mistake.
python3 - "$DEST/init.lua" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
head = "-- chatbridge — Luanti side.\n"
note = """-- chatbridge — Luanti side.
--
-- VENDOROVANÁ KOPIE. Zdroj pravdy je repo chatbridge (mod/chatbridge); sem
-- se kopíruje přes jeho tools/sync-to-doggiowars.sh, aby hra dorazila
-- streamerovi i s integrací a on neinstaloval žádný mod. Needituj tady.
"""
if "VENDOROVANÁ" not in s:
    s = s.replace(head, note, 1)
    open(p, 'w').write(s)
PY
echo "synced -> $DEST"
