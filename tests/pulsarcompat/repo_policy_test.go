package pulsarcompat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuruSourceDoesNotImportGoGoProtoDirectly(t *testing.T) {
	repoRoot := projectRootFromTestFile(t)
	disallowed := "github.com/cosmos/" + "gogoproto"
	allowed := map[string]struct{}{
		filepath.Join("app", "tx_service_wrapper.go"): {},
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
