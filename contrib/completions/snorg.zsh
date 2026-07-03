# snorg shell completions for zsh
# Install: cp snorg.zsh /usr/local/share/zsh/site-functions/_snorg && exec zsh

#compdef snorg

_snorg() {
    local context state state_descr line
    typeset -A opt_args

    _arguments -C \
        '(- :)'{-c,--config}'[Config YAML file (repeatable)]:file:_files' \
        '(--no-archive-config)'{--no-archive-config}'[Ignore archive config.yaml]' \
        '1: :->cmds' \
        '*:: :->args'

    case "$state" in
        cmds)
            _values "command" \
                "ingest[Register a .note file into the archive]" \
                "list[List archived FILE_IDs]" \
                "retrieve[Print assembled note as JSON]" \
                "query[Query pages by filter]" \
                "analyze[Vision-LLM analysis of pages]" \
                "export[Render note through template]"
            ;;
        args)
            case "$line[1]" in
                ingest)
                    _arguments \
                        '(-j --jobs)'{-j,--jobs}'[Max concurrent notes (0 = NumCPU)]:number:'
                    ;;
                analyze)
                    _arguments \
                        '(--force)'{--force}'[Re-analyze even when unchanged]'
                    ;;
                query)
                    _values "filter" \
                        "all[All pages]" \
                        "note[Pages in a FILE_ID]" \
                        "unanalyzed[Pages without analysis]" \
                        "keyword[Pages matching keyword regexp]" \
                        "starred[Starred pages]"
                    ;;
            esac
            ;;
    esac
}

_snorg
