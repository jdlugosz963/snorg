package snorg

import (
	"os"
	"path/filepath"
	"strings"
)

// archiveConfigName is the per-archive default config file, loaded from the archive
// root and merged over the XDG user config, under any -c files.
const archiveConfigName = "config.yaml"

// userConfigPath returns the XDG user config file
// ($XDG_CONFIG_HOME/snorg/config.yaml, i.e. ~/.config/snorg/config.yaml), the
// lowest-precedence config layer and the natural home for a default archive: key.
// Empty when the user config dir can't be resolved.
func userConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "snorg", archiveConfigName)
}

// configPaths orders the config layers lowest-precedence first for LoadConfig's
// later-wins merge: the XDG user config, then the archive's config.yaml, then the
// -c files. A file layer is skipped when absent, a directory, or opted out.
func configPaths(userPath, archivePath string, cliPaths []string, noUser, noArchive bool) []string {
	var paths []string
	isFile := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
	if !noUser && userPath != "" && isFile(userPath) {
		paths = append(paths, userPath)
	}
	if !noArchive {
		if p := filepath.Join(archivePath, archiveConfigName); isFile(p) {
			paths = append(paths, p)
		}
	}
	return append(paths, cliPaths...)
}

// expandHome expands a leading ~ or ~/ in a path to the user's home directory, so a
// config archive: ~/notes/sn resolves like the shell would (YAML/Go do not).
// Returns p unchanged when it has no ~ prefix or the home dir can't be resolved.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
}
