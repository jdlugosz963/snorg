# Supernote `.note` format & tooling

Working notes for SNORG. Sample: `note.note` (6 pages), device `N5`, renders 1920×2560.

## Tool: `supernote-tool` (supernotelib 0.7.1)

- `analyze <file>` → JSON: `__signature__`, `__header__`, `__footer__`, `__pages__`.
- `convert -n N -t {png,svg,pdf,txt} <in> <out>` — **`-n` is 0-indexed**, `-a` = all pages.
- Page renders at **1920×2560 px**; all `*RECT` values are in this pixel space.

## Identifiers

- **File id**: `__header__.FILE_ID` (e.g. `F20260629154102100593mO9IZI46DNYe`). Stable; links reference it via `LINKFILEID`.
- **Page id**: `__pages__[i].PAGEID` (e.g. `P2026...`), one per page.
- **Version**: `__signature__` (e.g. `SN_FILE_VER_20260016`).
- **Page numbering**: footer index keys are **1-based**; `convert -n` is **0-based** → footer page `N` == `convert -n (N-1)`.

## Footer index keys

`TITLE_/KEYWORD_/LINKO_` keys carry a numeric suffix = position metadata (4-digit groups):
- TITLE / LINKO: `page, y, x, h, w`.  KEYWORD: `page, y`.
- The **page lives in this key**; the structured records below don't carry a reliable page
  (`KEYWORDPAGE` was `0` for a page-4 keyword). Always derive page from the footer key.

## Structured records (content source)

- **Titles** `get_titles()`: `TITLERECT="x,y,w,h"`, `TITLEBITMAP` (own RLE bitmap of the region), `TITLELEVEL`, `TITLESEQNO`.
- **Keywords** `get_keywords()`: `KEYWORD` is **already decoded text** (e.g. `"fizyka"`). Keywords are **invisible page-level metadata** — no handwritten counterpart, so `KEYWORDRECT` is meaningless (parser even copies the title rect). Use text + page only.
- **Links** `get_links()`: `LINKRECT`, `PAGEID` (target page id — **preferred**, stable across reorder),
  `OBJPAGE` (target page number, 1-based — derived/volatile, **not stored**), `LINKFILE` (**base64** of an
  absolute path `/storage/emulated/0/Note/...`), `LINKFILEID` (target file id), `LINKINOUT`, `LINKTYPE`.
  - **Internal vs external**: `LINKFILEID == FILE_ID` → internal (jump within same note); else external (other `.note`).

## Element extraction

- **Title region** → crop page PNG at `(x, y, x+w, y+h)` from `TITLERECT`.
  **VERIFIED**: rect `356,98,336,208` on page 1 bounds the title "Essay" exactly. (Alt: decode `TITLEBITMAP`.)
- **Star / favorite page** → `__pages__[i].FIVESTAR`: present only on starred pages (page 3 in sample),
  absent/null otherwise. Value = star stroke coords + trailing flag. Criterion: `FIVESTAR` not null.
- **Keyword** → invisible metadata: take text from `KEYWORD`, page from footer key; no region to crop.
- **Link region** → crop page PNG at `(x,y,x+w,y+h)` from `LINKRECT`. **VERIFIED**: p5 → "link" box, p6 → "link to another note" box. Target: `PAGEID` + `LINKFILEID` (internal) or decode `LINKFILE` base64 (external).
- **Link name** → base64-decode `LINKFILE` to the device path, take the basename without extension (e.g. `…/linked-note.note` → `linked-note`); used as the target note's human name.

## Sample ground truth

p1 title "Essay" · p3 starred (`FIVESTAR`) · p4 keyword "fizyka" · p5 internal link → p1 · p6 external link → `library/external-note.note`.
