#!/bin/sh
# examples/web/export.sh — build a static, servable HTML site from selected pages.
#
# Reads PAGEIDs on stdin (pipe them from `snorg query …`) and, for the SAME set,
# runs `snorg export` twice with two configs living next to this script:
#   index.yaml -> <dest>/index.html          (one listing of all selected notes)
#   note.yaml  -> <dest>/<FILE_ID>.html       (one page per selected note)
# It also copies the SVG of each selected page to <dest>/<FILE_ID>/<PAGEID>.svg so
# the <img> references in the note pages resolve. No JSON is copied.
#
# Usage:
#   snorg -a <archive> query <filter> | examples/web/export.sh <archive> <dest>
#
# Env knobs:
#   SNORG   snorg binary to use (default: snorg on PATH)
#   FORCE   set to 1 (or pass -y) to wipe a non-empty <dest> without prompting
#
# Requires: snorg and pandoc (the `html` filter) on PATH.
set -eu

FORCE="${FORCE:-0}"
if [ "${1:-}" = "-y" ]; then
	FORCE=1
	shift
fi

if [ "$#" -ne 2 ]; then
	echo "usage: [SNORG=…] $0 [-y] <archive> <dest>   (PAGEIDs on stdin)" >&2
	exit 2
fi

ARCHIVE=$1
DEST=$2
SNORG="${SNORG:-snorg}"
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)

if [ ! -d "$ARCHIVE" ]; then
	echo "archive is not a directory: $ARCHIVE" >&2
	exit 1
fi

# Slurp the PAGEIDs off stdin now — stdin is the pipe, so any later prompt must
# read the terminal (/dev/tty) instead.
PAGEIDS=$(mktemp)
trap 'rm -f "$PAGEIDS"' EXIT
cat > "$PAGEIDS"
if [ ! -s "$PAGEIDS" ]; then
	echo "no PAGEIDs on stdin (pipe them from: snorg -a $ARCHIVE query …)" >&2
	exit 1
fi

# The destination must be clean so old pages/assets don't linger.
if [ -e "$DEST" ] && [ -n "$(ls -A "$DEST" 2>/dev/null)" ]; then
	if [ "$FORCE" != "1" ]; then
		printf 'destination %s is not empty; wipe it? [y/N] ' "$DEST" >&2
		read ans < /dev/tty || ans=""
		case "$ans" in
			y | Y | yes | YES) ;;
			*)
				echo "aborted." >&2
				exit 1
				;;
		esac
	fi
	rm -rf -- "${DEST:?}"/* "${DEST:?}"/.[!.]* 2>/dev/null || true
fi
mkdir -p "$DEST"

# The listing page: all selected notes in one pass.
"$SNORG" -a "$ARCHIVE" --no-archive-config -c "$SCRIPT_DIR/index.yaml" export \
	< "$PAGEIDS" > "$DEST/index.html"

# One HTML page per selected note, plus its selected pages' SVGs. `query note`
# reads the piped PAGEIDs and restricts the note filter to that set, so `sub` is
# exactly this note's selected pages (empty when none were selected).
"$SNORG" -a "$ARCHIVE" list | while IFS= read -r fid; do
	[ -n "$fid" ] || continue
	sub=$("$SNORG" -a "$ARCHIVE" query note "$fid" < "$PAGEIDS")
	[ -n "$sub" ] || continue
	printf '%s\n' "$sub" | "$SNORG" -a "$ARCHIVE" --no-archive-config \
		-c "$SCRIPT_DIR/note.yaml" export > "$DEST/$fid.html"
	mkdir -p "$DEST/$fid"
	printf '%s\n' "$sub" | while IFS= read -r pid; do
		[ -n "$pid" ] || continue
		cp "$ARCHIVE/$fid/$pid.svg" "$DEST/$fid/"
	done
	echo "wrote $DEST/$fid.html" >&2
done

echo "site written to $DEST (open $DEST/index.html)" >&2
