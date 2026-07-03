# snorg shell completions for fish
# Install: cp snorg.fish ~/.config/fish/completions/ && exec fish

complete -c snorg -f

# Global flags
complete -c snorg -s c -l config         -d "Config YAML file (repeatable)" -r
complete -c snorg -l no-archive-config   -d "Ignore archive config.yaml"

# Commands
complete -c snorg -n "not __fish_seen_subcommand_from ingest list retrieve query analyze export" -xa "ingest"   -d "Register a .note file into the archive"
complete -c snorg -n "not __fish_seen_subcommand_from ingest list retrieve query analyze export" -xa "list"     -d "List archived FILE_IDs"
complete -c snorg -n "not __fish_seen_subcommand_from ingest list retrieve query analyze export" -xa "retrieve" -d "Print assembled note as JSON"
complete -c snorg -n "not __fish_seen_subcommand_from ingest list retrieve query analyze export" -xa "query"    -d "Query pages by filter"
complete -c snorg -n "not __fish_seen_subcommand_from ingest list retrieve query analyze export" -xa "analyze"  -d "Vision-LLM analysis of pages"
complete -c snorg -n "not __fish_seen_subcommand_from ingest list retrieve query analyze export" -xa "export"   -d "Render note through template"

# ingest flags
complete -c snorg -n "__fish_seen_subcommand_from ingest" -s j -l jobs -d "Max concurrent notes (0 = NumCPU)" -r

# analyze flags
complete -c snorg -n "__fish_seen_subcommand_from analyze" -l force -d "Re-analyze even when unchanged"

# query sub-filters (positional args)
complete -c snorg -n "__fish_seen_subcommand_from query; and not __fish_seen_subcommand_from all note unanalyzed keyword starred" -xa "all"       -d "All pages"
complete -c snorg -n "__fish_seen_subcommand_from query; and not __fish_seen_subcommand_from all note unanalyzed keyword starred" -xa "note"      -d "Pages in a FILE_ID"
complete -c snorg -n "__fish_seen_subcommand_from query; and not __fish_seen_subcommand_from all note unanalyzed keyword starred" -xa "unanalyzed" -d "Pages without analysis"
complete -c snorg -n "__fish_seen_subcommand_from query; and not __fish_seen_subcommand_from all note unanalyzed keyword starred" -xa "keyword"   -d "Pages matching keyword regexp"
complete -c snorg -n "__fish_seen_subcommand_from query; and not __fish_seen_subcommand_from all note unanalyzed keyword starred" -xa "starred"   -d "Starred pages"
