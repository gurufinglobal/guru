package pulsarcompat

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestGuruSourceDoesNotImportGoGoProtoDirectly(t *testing.T) {
	repoRoot := policyProjectRoot(t)
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

func TestNodeModulesDoNotImportPublicPulsarAPI(t *testing.T) {
	repoRoot := policyProjectRoot(t)
	publicAPI := "github.com/gurufinglobal/guru/v3/api/guru/"
	var offenders []string

	for _, sourceDir := range []string{"app", "cmd", "x"} {
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
		t.Fatalf("node modules must use internal gogo types, found public Pulsar imports in: %s", strings.Join(offenders, ", "))
	}
}

func TestStandaloneOracleModuleBoundary(t *testing.T) {
	repoRoot := policyProjectRoot(t)
	oracleRoot := filepath.Join(repoRoot, "oracle")
	moduleBytes, err := os.ReadFile(filepath.Join(oracleRoot, "go.mod"))
	if err != nil {
		t.Fatalf("read standalone oracle go.mod: %v", err)
	}
	sidecarModule := "github.com/gurufinglobal/guru/" + "oracle"
	const rootModule = "github.com/gurufinglobal/guru/v3"
	modulePath, rootRequirement, replacements := parseStandaloneModuleContract(string(moduleBytes), rootModule)
	if modulePath != sidecarModule {
		t.Fatalf("unexpected standalone oracle module path: %q", modulePath)
	}
	if rootRequirement != "v3.0.0" {
		t.Fatalf("standalone oracle must require %s v3.0.0, got %q", rootModule, rootRequirement)
	}
	if len(replacements) != 1 || replacements[0] != rootModule+" => .." {
		t.Fatalf("standalone oracle must have exactly `replace %s => ..`, got %v", rootModule, replacements)
	}

	for _, forbidden := range []string{"api", "proto"} {
		path := filepath.Join(oracleRoot, forbidden)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("standalone oracle must consume root-generated types without a copied %s tree: %v", forbidden, err)
		}
	}

	rootImport := rootModule + "/"
	internalGogoAPI := rootModule + "/x/oracle/types"
	foundInternalGogoAPI := false
	var disallowedRootImports []string
	err = filepath.WalkDir(oracleRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(contents), "\n") {
			if !strings.Contains(line, rootImport) {
				continue
			}
			if strings.Contains(line, `"`+internalGogoAPI+`"`) {
				foundInternalGogoAPI = true
				continue
			}
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			disallowedRootImports = append(disallowedRootImports, rel+": "+strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan standalone oracle imports: %v", err)
	}
	if len(disallowedRootImports) > 0 {
		t.Fatalf("standalone oracle may import only the root internal gogo Oracle types: %s", strings.Join(disallowedRootImports, ", "))
	}
	if !foundInternalGogoAPI {
		t.Fatalf("standalone oracle does not import the root internal gogo Oracle types")
	}
}

func parseStandaloneModuleContract(contents, rootModule string) (string, string, []string) {
	var (
		modulePath      string
		rootRequirement string
		replacements    []string
	)
	for _, rawLine := range strings.Split(contents, "\n") {
		line := strings.TrimSpace(strings.SplitN(rawLine, "//", 2)[0])
		fields := strings.Fields(line)
		switch {
		case len(fields) == 2 && fields[0] == "module":
			modulePath = fields[1]
		case len(fields) == 2 && fields[0] == rootModule:
			rootRequirement = fields[1]
		case len(fields) == 4 && fields[0] == "replace" && fields[2] == "=>":
			replacements = append(replacements, strings.Join(fields[1:], " "))
		case len(fields) > 0 && fields[0] == "replace":
			replacements = append(replacements, line)
		}
	}
	return modulePath, rootRequirement, replacements
}

func TestRootDoesNotImportStandaloneOracleOrUseGoWorkspace(t *testing.T) {
	repoRoot := policyProjectRoot(t)
	sidecarModule := "github.com/gurufinglobal/guru/" + "oracle"
	var offenders []string

	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			switch rel {
			case ".git", "build", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "go.work" || entry.Name() == "go.work.sum" {
			rel, err := filepath.Rel(repoRoot, path)
			if err != nil {
				return err
			}
			offenders = append(offenders, rel)
			return nil
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		if rel == "oracle" || strings.HasPrefix(rel, "oracle"+string(filepath.Separator)) {
			return nil
		}
		if !strings.HasSuffix(path, ".go") && path != filepath.Join(repoRoot, "go.mod") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(contents), sidecarModule) {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan root module boundary: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("root module must not import the standalone oracle or use a Go workspace: %s", strings.Join(offenders, ", "))
	}
}

func TestProtoGenScriptIsRootOnly(t *testing.T) {
	repoRoot := policyProjectRoot(t)
	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "proto-gen.sh"))
	if err != nil {
		t.Fatalf("read repository codegen script: %v", err)
	}
	for _, forbidden := range []string{"oracle/api", "oracle/proto", "oracle/scripts"} {
		if strings.Contains(string(script), forbidden) {
			t.Fatalf("root protobuf generator must not manage standalone sidecar files: found %q", forbidden)
		}
	}
}

func TestRootAndOracleReleaseTagsAreIsolated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the version expressions and fixture use POSIX shell tools")
	}

	repoRoot := policyProjectRoot(t)
	makefile, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	assignments := make(map[string]string, 2)
	for _, line := range strings.Split(string(makefile), "\n") {
		for _, name := range []string{"VERSION", "ORACLE_VERSION"} {
			if strings.HasPrefix(line, name+" :=") {
				assignments[name] = line
			}
		}
	}
	if len(assignments) != 2 {
		t.Fatalf("Makefile must define isolated VERSION and ORACLE_VERSION expressions: %v", assignments)
	}

	fixture := t.TempDir()
	runFixtureCommand(t, fixture, "git", "init", "-q")
	runFixtureCommand(t, fixture, "git", "config", "user.email", "oracle-version-test@example.invalid")
	runFixtureCommand(t, fixture, "git", "config", "user.name", "Oracle Version Test")
	writeVersionFixtureCommit(t, fixture, "root")
	runFixtureCommand(t, fixture, "git", "tag", "v1.2.3")
	writeVersionFixtureCommit(t, fixture, "oracle")
	runFixtureCommand(t, fixture, "git", "tag", "oracle/v4.5.6")
	writeVersionFixtureCommit(t, fixture, "head")

	fixtureMakefile := assignments["VERSION"] + "\n" +
		assignments["ORACLE_VERSION"] + "\n" +
		"print-versions:\n" +
		"\t@printf '%s\\n%s\\n' '$(VERSION)' '$(ORACLE_VERSION)'\n"
	if err := os.WriteFile(filepath.Join(fixture, "Makefile"), []byte(fixtureMakefile), 0o600); err != nil {
		t.Fatalf("write version fixture Makefile: %v", err)
	}
	output := runFixtureCommand(t, fixture, "make", "--no-print-directory", "print-versions")
	versions := strings.Fields(output)
	if len(versions) != 2 {
		t.Fatalf("unexpected version output %q", output)
	}
	if !strings.HasPrefix(versions[0], "v1.2.3-2-g") {
		t.Fatalf("root version was contaminated by sidecar tag: %q", versions[0])
	}
	if !strings.HasPrefix(versions[1], "v4.5.6-1-g") {
		t.Fatalf("sidecar version was contaminated by root tag or retained its namespace: %q", versions[1])
	}

	archive := t.TempDir()
	if err := os.WriteFile(filepath.Join(archive, "Makefile"), []byte(fixtureMakefile), 0o600); err != nil {
		t.Fatalf("write source archive Makefile: %v", err)
	}
	archiveVersions := strings.Fields(runFixtureCommand(
		t,
		archive,
		"make",
		"--no-print-directory",
		"print-versions",
	))
	if len(archiveVersions) != 2 || archiveVersions[0] != "dev" || archiveVersions[1] != "dev" {
		t.Fatalf("source archive versions = %v, want [dev dev]", archiveVersions)
	}
}

func TestInternalAndPublicGatewayPatternsMatch(t *testing.T) {
	repoRoot := policyProjectRoot(t)
	modules := []struct {
		name         string
		internalPath string
		gatewayFiles []string
	}{
		{name: "bex", internalPath: filepath.Join("x", "bex", "types"), gatewayFiles: []string{"query.pb.gw.go"}},
		{name: "constitution", internalPath: filepath.Join("x", "constitution", "types"), gatewayFiles: []string{"query.pb.gw.go"}},
		{name: "feepolicy", internalPath: filepath.Join("x", "feepolicy", "types"), gatewayFiles: []string{"query.pb.gw.go", "tx.pb.gw.go"}},
		{name: "oracle", internalPath: filepath.Join("x", "oracle", "types"), gatewayFiles: []string{"query.pb.gw.go"}},
		{name: "transwap", internalPath: filepath.Join("x", "ibc", "transwap", "types"), gatewayFiles: []string{"query.pb.gw.go"}},
	}
	for _, module := range modules {
		for _, gatewayFile := range module.gatewayFiles {
			internal := gatewayPatternLines(t, filepath.Join(repoRoot, module.internalPath, gatewayFile))
			public := gatewayPatternLines(t, filepath.Join(repoRoot, "api", "guru", module.name, "v1", gatewayFile))
			if strings.Join(internal, "\n") != strings.Join(public, "\n") {
				t.Fatalf("%s %s internal and public gateway patterns differ:\ninternal=%s\npublic=%s", module.name, gatewayFile, internal, public)
			}
		}
	}
}

func TestProtoGenScriptDiscoversFutureModulesPrunesStalePublicAPIAndIsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the repository codegen script requires a POSIX shell")
	}

	repoRoot := policyProjectRoot(t)
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
	firstGeneration := snapshotFixtureTrees(t, tempRoot, "x", "api")

	cmd = exec.Command("sh", "scripts/proto-gen.sh")
	cmd.Dir = tempRoot
	cmd.Env = []string{"PATH=" + filepath.Join(tempRoot, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rerun codegen script fixture: %v\n%s", err, output)
	}
	secondGeneration := snapshotFixtureTrees(t, tempRoot, "x", "api")
	if !reflect.DeepEqual(firstGeneration, secondGeneration) {
		t.Fatalf("root protobuf generation is not idempotent:\nfirst=%v\nsecond=%v", firstGeneration, secondGeneration)
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

	repoRoot := policyProjectRoot(t)
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
	repoRoot := policyProjectRoot(t)
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
		if strings.Contains(line, "pattern_Query_") || strings.Contains(line, "pattern_Msg_") {
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

func snapshotFixtureTrees(t *testing.T, root string, trees ...string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	for _, tree := range trees {
		treeRoot := filepath.Join(root, tree)
		err := filepath.WalkDir(treeRoot, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			snapshot[rel] = string(contents)
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot fixture tree %s: %v", tree, err)
		}
	}
	return snapshot
}

func writeVersionFixtureCommit(t *testing.T, root, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "state"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write version fixture state: %v", err)
	}
	runFixtureCommand(t, root, "git", "add", "state")
	runFixtureCommand(t, root, "git", "commit", "-q", "-m", contents)
}

func runFixtureCommand(t *testing.T, root, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %v: %v\n%s", name, args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func policyProjectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve repository root from policy test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
