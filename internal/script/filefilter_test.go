package script

import "testing"

func TestShouldSkipScriptFile(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
		reason   string
	}{
		// Valid script files — should NOT be skipped.
		{"deploy.sh", false, "normal shell script"},
		{"build.bash", false, "bash script"},
		{"setup.py", false, "python script"},
		{"migrate.rb", false, "ruby script"},
		{"run", false, "extensionless script"},
		{"my-script.sh", false, "script with hyphen"},
		{"my_script.sh", false, "script with underscore"},

		// Hidden files — should be skipped.
		{".deploy.sh.swp", true, "Vim swap file (hidden)"},
		{".gitkeep", true, "hidden file"},
		{".hidden", true, "generic hidden file"},

		// Vim swap files — should be skipped.
		{"deploy.sh.swp", true, ".swp suffix"},
		{"file.swp", true, ".swp suffix without double ext"},
		{"deploy.sh.swo", true, ".swo suffix"},
		{"file.swo", true, ".swo suffix without double ext"},

		// Backup files — should be skipped.
		{"deploy.sh~", true, "tilde backup (Vim/Emacs)"},
		{"file~", true, "tilde backup without ext"},
		{"deploy.sh.bak", true, ".bak backup"},
		{"file.bak", true, ".bak backup without double ext"},

		// Temporary files — should be skipped.
		{"deploy.sh.tmp", true, ".tmp temporary file"},
		{"file.tmp", true, ".tmp temporary file without double ext"},

		// Merge/patch artifacts — should be skipped.
		{"deploy.sh.orig", true, ".orig merge conflict file"},
		{"deploy.sh.rej", true, ".rej patch reject file"},

		// Emacs auto-save files — should be skipped.
		{"#deploy.sh#", true, "Emacs auto-save file"},
		{"#file#", true, "Emacs auto-save file without ext"},

		// Edge cases.
		{"#onlyhash", false, "starts with # but doesn't end with #"},
		{"onlyhash#", false, "ends with # but doesn't start with #"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := ShouldSkipScriptFile(tt.filename)
			if got != tt.want {
				t.Errorf("ShouldSkipScriptFile(%q) = %v, want %v (%s)", tt.filename, got, tt.want, tt.reason)
			}
		})
	}
}
