package script

import "strings"

// blockedSuffixes lists file suffixes that indicate editor swap/backup/temp files.
var blockedSuffixes = []string{
	".swp",  // Vim swap file
	".swo",  // Vim swap file (2nd)
	"~",     // Vim/Emacs backup
	".bak",  // Backup file
	".tmp",  // Temporary file
	".orig", // Merge conflict original
	".rej",  // Patch reject file
}

// ShouldSkipScriptFile returns true if the given filename looks like an editor
// swap file, backup file, or other temporary file that should not be registered
// as a script.
//
// It checks for:
//   - Hidden files (filenames starting with ".")
//   - Known temporary/swap file suffixes (.swp, .swo, ~, .bak, .tmp, .orig, .rej)
//   - Emacs auto-save files (filenames wrapped in "#", e.g. #deploy.sh#)
func ShouldSkipScriptFile(filename string) bool {
	// Skip hidden files (e.g. .deploy.sh.swp, .gitkeep).
	if strings.HasPrefix(filename, ".") {
		return true
	}

	// Skip Emacs auto-save files (e.g. #deploy.sh#).
	if strings.HasPrefix(filename, "#") && strings.HasSuffix(filename, "#") {
		return true
	}

	// Skip files with blocked suffixes.
	for _, suffix := range blockedSuffixes {
		if strings.HasSuffix(filename, suffix) {
			return true
		}
	}

	return false
}
