package users_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskfileFreshToolInstallReturnsExecutablePath(t *testing.T) {
	task, err := os.ReadFile("taskfile")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if mkdirErr := os.Mkdir(bin, 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	files := map[string]string{
		"taskfile": string(task),
		"bin/go": `#!/bin/bash
set -eu
case "$1" in
  env) printf '%s\n' "$GOBIN" ;;
  install)
    printf '#!/bin/bash\nprintf "fixture tool ran\\n"\n' > "$GOBIN/fixture-tool"
    chmod +x "$GOBIN/fixture-tool"
    ;;
  *) exit 1 ;;
esac
`,
	}
	for name, contents := range files {
		if writeErr := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o755); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	command := exec.CommandContext(t.Context(), "bash", "taskfile", "_go:tool:ensure", "fixture-tool", "example.test/tool@v1.0.0")
	command.Dir = dir
	command.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "GOBIN="+bin)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("install failed: %v\n%s", err, stderr.String())
	}
	path := strings.TrimSpace(string(output))
	if path != filepath.Join(bin, "fixture-tool") {
		t.Fatalf("stdout must contain only executable path, got %q", output)
	}
	if !strings.Contains(stderr.String(), "Installing fixture-tool") {
		t.Fatalf("missing install diagnostic: %s", stderr.String())
	}
	if output, err := exec.CommandContext(t.Context(), path).Output(); err != nil || strings.TrimSpace(string(output)) != "fixture tool ran" {
		t.Fatalf("installed path was not executable: %q, %v", output, err)
	}
}
