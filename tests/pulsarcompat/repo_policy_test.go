package pulsarcompat

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestGuruSourceDoesNotImportGoGoProtoDirectly(t *testing.T) {
	repoRoot := projectRootFromTestFile(t)
	disallowed := "github.com/cosmos/" + "gogoproto"
	allowed := map[string]struct{}{
		filepath.Join("app", "app.go"):                                   {},
		filepath.Join("app", "params", "bex_transwap_tx_config_test.go"): {},
		filepath.Join("app", "params", "encoding.go"):                    {},
		filepath.Join("app", "params", "bex_gogo_map_entries.go"):        {},
		filepath.Join("app", "tx_service_wrapper.go"):                    {},
		filepath.Join("tests", "pulsarcompat", "pulsar_compat_test.go"):  {},
		filepath.Join("x", "constitution", "genesis.go"):                 {},
		filepath.Join("x", "bex", "types", "proto_helpers.go"):           {},
		filepath.Join("x", "oracle", "genesis.go"):                       {},
	}

	var offenders []string
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "api", "build", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		if _, ok := allowed[rel]; ok {
			return nil
		}
		if isGeneratedGuruGogo(rel) {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(contents), disallowed) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan repo for disallowed gogo imports: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("direct gogoproto imports are not allowed in Guru source: %s", strings.Join(offenders, ", "))
	}
}

func TestMigratedNodeModulesDoNotImportPublicPulsarAPI(t *testing.T) {
	repoRoot := projectRootFromTestFile(t)
	publicAPI := "github.com/gurufinglobal/guru/v3/api/guru/"
	var offenders []string

	for _, sourceDir := range []string{"app", "cmd", "oracle", "x"} {
		root := filepath.Join(repoRoot, sourceDir)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(contents), publicAPI) {
				rel, err := filepath.Rel(repoRoot, path)
				if err != nil {
					return err
				}
				offenders = append(offenders, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s for public Pulsar imports: %v", sourceDir, err)
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("migrated node modules must use internal gogo types, found public Pulsar imports in: %s", strings.Join(offenders, ", "))
	}
}

func TestInternalAndPublicGatewayPatternsMatch(t *testing.T) {
	repoRoot := projectRootFromTestFile(t)
	modules := []struct {
		name         string
		internalPath string
	}{
		{name: "bex", internalPath: filepath.Join("x", "bex", "types")},
		{name: "constitution", internalPath: filepath.Join("x", "constitution", "types")},
		{name: "oracle", internalPath: filepath.Join("x", "oracle", "types")},
		{name: "transwap", internalPath: filepath.Join("x", "ibc", "transwap", "types")},
	}
	for _, module := range modules {
		internal := gatewayPatternLines(t, filepath.Join(repoRoot, module.internalPath, "query.pb.gw.go"))
		public := gatewayPatternLines(t, filepath.Join(repoRoot, "api", "guru", module.name, "v1", "query.pb.gw.go"))
		if strings.Join(internal, "\n") != strings.Join(public, "\n") {
			t.Fatalf("%s internal and public gateway patterns differ:\ninternal=%s\npublic=%s", module.name, internal, public)
		}
	}
}

func TestProtoGenScriptDiscoversFutureModulesAndPrunesStalePublicAPI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the repository codegen script requires a POSIX shell")
	}

	repoRoot := projectRootFromTestFile(t)
	tempRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(tempRoot, "bin"),
		filepath.Join(tempRoot, "proto"),
		filepath.Join(tempRoot, "scripts"),
		filepath.Join(tempRoot, "x", "constitution", "types"),
		filepath.Join(tempRoot, "x", "ibc", "transwap", "types"),
		filepath.Join(tempRoot, "x", "legacy", "types"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create fixture directory %s: %v", dir, err)
		}
	}

	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "proto-gen.sh"))
	if err != nil {
		t.Fatalf("read repository codegen script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "scripts", "proto-gen.sh"), script, 0o755); err != nil {
		t.Fatalf("write fixture codegen script: %v", err)
	}

	fakeBuf := `#!/bin/sh
set -eu
case "$*" in
  *buf.gen.gogo.yaml*)
    root=.proto-gen/gogo/github.com/gurufinglobal/guru/v3/x
    for module in constitution future ibc/transwap; do
      mkdir -p "$root/$module/types"
      printf '%s\n' '// generated fixture' > "$root/$module/types/types.pb.go"
    done
    ;;
  *buf.gen.pulsar.yaml*)
    root=.proto-gen/pulsar/api/guru
    for module in constitution future; do
      mkdir -p "$root/$module/v1"
      printf '%s\n' '// generated fixture' > "$root/$module/v1/types.pulsar.go"
    done
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(tempRoot, "bin", "buf"), []byte(fakeBuf), 0o755); err != nil {
		t.Fatalf("write fake buf executable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "x", "constitution", "types", "stale.pb.go"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale generated fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "x", "constitution", "types", "codec.go"), []byte("package types\n"), 0o644); err != nil {
		t.Fatalf("write handwritten fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "x", "ibc", "transwap", "types", "stale.pb.go"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write nested stale generated fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "x", "ibc", "transwap", "types", "codec.go"), []byte("package types\n"), 0o644); err != nil {
		t.Fatalf("write nested handwritten fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "x", "legacy", "types", "stale.pb.go"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write removed module generated fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "x", "legacy", "types", "codec.go"), []byte("package types\n"), 0o644); err != nil {
		t.Fatalf("write removed module handwritten fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tempRoot, "api", "guru", "legacy", "v1"), 0o755); err != nil {
		t.Fatalf("create stale public API fixture directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "api", "guru", "legacy", "v1", "stale.pulsar.go"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale public API fixture: %v", err)
	}

	cmd := exec.Command("sh", "scripts/proto-gen.sh")
	cmd.Dir = tempRoot
	cmd.Env = []string{"PATH=" + filepath.Join(tempRoot, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run codegen script fixture: %v\n%s", err, output)
	}

	for _, path := range []string{
		filepath.Join(tempRoot, "x", "constitution", "types", "types.pb.go"),
		filepath.Join(tempRoot, "x", "future", "types", "types.pb.go"),
		filepath.Join(tempRoot, "x", "ibc", "transwap", "types", "types.pb.go"),
		filepath.Join(tempRoot, "x", "constitution", "types", "codec.go"),
		filepath.Join(tempRoot, "x", "ibc", "transwap", "types", "codec.go"),
		filepath.Join(tempRoot, "x", "legacy", "types", "codec.go"),
		filepath.Join(tempRoot, "api", "guru", "constitution", "v1", "types.pulsar.go"),
		filepath.Join(tempRoot, "api", "guru", "future", "v1", "types.pulsar.go"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated or handwritten file %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(tempRoot, "x", "constitution", "types", "stale.pb.go")); !os.IsNotExist(err) {
		t.Fatalf("stale generated file was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempRoot, "x", "ibc", "transwap", "types", "stale.pb.go")); !os.IsNotExist(err) {
		t.Fatalf("nested stale generated file was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempRoot, "x", "legacy", "types", "stale.pb.go")); !os.IsNotExist(err) {
		t.Fatalf("removed module generated file was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempRoot, "api", "guru", "legacy", "v1", "stale.pulsar.go")); !os.IsNotExist(err) {
		t.Fatalf("stale public API file was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempRoot, ".proto-gen")); !os.IsNotExist(err) {
		t.Fatalf("staging directory was not removed: %v", err)
	}
}

func TestProtoGenScriptRejectsFilesBelowTypesPackage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the repository codegen script requires a POSIX shell")
	}

	repoRoot := projectRootFromTestFile(t)
	tempRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(tempRoot, "bin"),
		filepath.Join(tempRoot, "proto"),
		filepath.Join(tempRoot, "scripts"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create fixture directory %s: %v", dir, err)
		}
	}

	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "proto-gen.sh"))
	if err != nil {
		t.Fatalf("read repository codegen script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempRoot, "scripts", "proto-gen.sh"), script, 0o755); err != nil {
		t.Fatalf("write fixture codegen script: %v", err)
	}

	fakeBuf := `#!/bin/sh
set -eu
case "$*" in
  *buf.gen.gogo.yaml*)
    root=.proto-gen/gogo/github.com/gurufinglobal/guru/v3/x/future/types/internal
    mkdir -p "$root"
    printf '%s\n' '// invalid generated fixture' > "$root/invalid.pb.go"
    ;;
  *buf.gen.pulsar.yaml*)
    root=.proto-gen/pulsar/api/guru/future/v1
    mkdir -p "$root"
    printf '%s\n' '// generated fixture' > "$root/types.pulsar.go"
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(tempRoot, "bin", "buf"), []byte(fakeBuf), 0o755); err != nil {
		t.Fatalf("write fake buf executable: %v", err)
	}

	cmd := exec.Command("sh", "scripts/proto-gen.sh")
	cmd.Dir = tempRoot
	cmd.Env = []string{"PATH=" + filepath.Join(tempRoot, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")}
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("codegen script accepted a generated package below a types directory:\n%s", output)
	}
	if !strings.Contains(string(output), "x/future/types/internal/invalid.pb.go") {
		t.Fatalf("codegen rejection did not identify the invalid file:\n%s", output)
	}
}

func TestPublicPulsarArtifactsExactlyMatchGuruProtoSources(t *testing.T) {
	repoRoot := projectRootFromTestFile(t)
	expected := make(map[string]struct{})
	protoRoot := filepath.Join(repoRoot, "proto", "guru")

	err := filepath.WalkDir(protoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}

		rel, err := filepath.Rel(filepath.Join(repoRoot, "proto"), path)
		if err != nil {
			return err
		}
		base := strings.TrimSuffix(rel, ".proto")
		expected[filepath.Join("api", base+".pulsar.go")] = struct{}{}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(contents), "\nservice ") {
			expected[filepath.Join("api", base+"_grpc.pb.go")] = struct{}{}
		}
		if strings.Contains(string(contents), "google.api.http") {
			expected[filepath.Join("api", base+".pb.gw.go")] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("derive expected public API artifacts: %v", err)
	}

	var unexpected []string
	err = filepath.WalkDir(filepath.Join(repoRoot, "api", "guru"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		if _, ok := expected[rel]; !ok {
			unexpected = append(unexpected, rel)
			return nil
		}
		delete(expected, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("scan generated public API artifacts: %v", err)
	}

	missing := make([]string, 0, len(expected))
	for path := range expected {
		missing = append(missing, path)
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	if len(missing) > 0 || len(unexpected) > 0 {
		t.Fatalf("public Pulsar artifacts do not match proto sources: missing=%v unexpected=%v", missing, unexpected)
	}
}

func gatewayPatternLines(t *testing.T, path string) []string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated gateway %s: %v", path, err)
	}
	var patterns []string
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.Contains(line, "pattern_Query_") {
			patterns = append(patterns, strings.TrimSpace(line))
		}
	}
	return patterns
}

func isGeneratedGuruGogo(rel string) bool {
	if !strings.HasSuffix(rel, ".pb.go") && !strings.HasSuffix(rel, ".pb.gw.go") {
		return false
	}

	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) >= 4 && parts[0] == "x" && parts[len(parts)-2] == "types"
}
