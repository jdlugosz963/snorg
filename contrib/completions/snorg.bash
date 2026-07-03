# snorg shell completions for bash
# Install: source snorg.bash

_snorg_completions() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local prev="${COMP_WORDS[COMP_CWORD-1]}"
    local subcmds="ingest list retrieve query analyze export"

    # No command yet → suggest subcommands + global flags
    if [[ $COMP_CWORD -eq 1 ]]; then
        COMPREPLY=($(compgen -W "$subcmds" -- "$cur"))
        return
    fi

    local cmd="${COMP_WORDS[1]}"

    case "$cmd" in
        ingest)
            case "$prev" in
                -j|--jobs)
                    COMPREPLY=()
                    ;;
                *)
                    COMPREPLY=($(compgen -W "-j --jobs" -- "$cur"))
                    ;;
            esac
            ;;
        analyze)
            case "$prev" in
                --force)
                    COMPREPLY=()
                    ;;
                *)
                    COMPREPLY=($(compgen -W "--force" -- "$cur"))
                    ;;
            esac
            ;;
        query)
            if [[ $COMP_CWORD -eq 2 ]]; then
                COMPREPLY=($(compgen -W "all note unanalyzed keyword starred" -- "$cur"))
            fi
            ;;
        *)
            COMPREPLY=()
            ;;
    esac
}

complete -F _snorg_completions snorg
