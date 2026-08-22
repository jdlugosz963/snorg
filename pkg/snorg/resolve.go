package snorg

import (
	"os"
	"path/filepath"
)

// userConfigName is the basename of the XDG user config file.
const userConfigName = "config.yaml"

// userConfigPath returns the XDG user config file
// ($XDG_CONFIG_HOME/snorg/config.yaml, i.e. ~/.config/snorg/config.yaml), the only
// auto-loaded config layer and the natural home for a default archive: key. Empty
// when the user config dir can't be resolved.
func userConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "snorg", userConfigName)
}

// configPaths orders the config layers lowest-precedence first for LoadConfig's
// later-wins merge: the XDG user config, then the -c files. The user config is
// skipped when absent, a directory, or opted out (noUser).
func configPaths(userPath string, cliPaths []string, noUser bool) []string {
	var paths []string
	if !noUser && userPath != "" {
		if st, err := os.Stat(userPath); err == nil && !st.IsDir() {
			paths = append(paths, userPath)
		}
	}
	return append(paths, cliPaths...)
}
