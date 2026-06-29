# SNORG — Project Principles

SNORG = **s**uper**n**ote-**org**anizer.

**Purpose:** turn Supernote tablet notes into a plain, machine-readable structure
and provide an interface to retrieve that data as human-readable content. Each note
is categorized by id; its content is analyzed by a vision LLM. All operational data
is plaintext so it lives well under version control.

## Engineering principles

- **No backward compatibility** — when something must change, drop the legacy and
  rewrite cleanly.
- **Go.**
- **Future-proof over cheap** — heavy abstraction is fine when genuinely useful
  long-term.
- **Plaintext-first** — all operational data is plaintext (VCS-friendly).
- **Docs in English, maximally concise**, preserving original meaning.
