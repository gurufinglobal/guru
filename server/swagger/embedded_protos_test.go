package swagger

import (
	"strings"
	"testing"
)

// TestScanModulesInEmbeddedProtoDir tests the embedded proto directory scanning
func TestScanModulesInEmbeddedProtoDir(t *testing.T) {
	// Test scanning the embedded proto/guru directory
	modules, err := ScanModulesInEmbeddedProtoDir("proto/guru")
	if err != nil {
		t.Fatalf("Failed to scan embedded proto directory: %v", err)
	}

	// We expect to find oracle and feepolicy modules
	expectedModules := map[string]bool{
		"oracle":    false,
		"feepolicy": false,
	}

	for _, module := range modules {
		if _, expected := expectedModules[module]; expected {
			expectedModules[module] = true
		}
	}

	// Check that we found all expected modules
	for module, found := range expectedModules {
		if !found {
			t.Errorf("Expected module %s not found in embedded proto directory", module)
		}
	}

	t.Logf("Found modules: %v", modules)
}

// TestReadEmbeddedProtoFile tests reading embedded proto files
func TestReadEmbeddedProtoFile(t *testing.T) {
	// Test reading oracle query.proto
	content, err := ReadEmbeddedProtoFile("proto/guru/oracle/v1/query.proto")
	if err != nil {
		t.Fatalf("Failed to read embedded oracle query.proto: %v", err)
	}

	// Verify the content contains expected proto syntax
	if !strings.Contains(content, "syntax = \"proto3\"") {
		t.Error("Expected proto3 syntax not found in oracle query.proto")
	}

	if !strings.Contains(content, "service Query") {
		t.Error("Expected Query service not found in oracle query.proto")
	}

	t.Logf("Successfully read oracle query.proto, length: %d bytes", len(content))

	// Test reading feepolicy query.proto
	content, err = ReadEmbeddedProtoFile("proto/guru/feepolicy/v1/query.proto")
	if err != nil {
		t.Fatalf("Failed to read embedded feepolicy query.proto: %v", err)
	}

	if !strings.Contains(content, "syntax = \"proto3\"") {
		t.Error("Expected proto3 syntax not found in feepolicy query.proto")
	}

	t.Logf("Successfully read feepolicy query.proto, length: %d bytes", len(content))
}

// TestCheckEmbeddedProtoFileExists tests file existence checking
func TestCheckEmbeddedProtoFileExists(t *testing.T) {
	// Test existing files
	existingFiles := []string{
		"proto/guru/oracle/v1/query.proto",
		"proto/guru/oracle/v1/tx.proto",
		"proto/guru/feepolicy/v1/query.proto",
		"proto/guru/feepolicy/v1/tx.proto",
	}

	for _, file := range existingFiles {
		if !CheckEmbeddedProtoFileExists(file) {
			t.Errorf("Expected file %s to exist in embedded filesystem", file)
		}
	}

	// Test non-existing file
	if CheckEmbeddedProtoFileExists("proto/guru/nonexistent/v1/query.proto") {
		t.Error("Expected non-existent file to return false")
	}
}

// TestListEmbeddedProtoFiles tests listing all embedded proto files
func TestListEmbeddedProtoFiles(t *testing.T) {
	files, err := ListEmbeddedProtoFiles()
	if err != nil {
		t.Fatalf("Failed to list embedded proto files: %v", err)
	}

	// We should have at least some proto files
	if len(files) == 0 {
		t.Error("Expected to find some embedded proto files")
	}

	// Check that all files have .proto extension
	for _, file := range files {
		if !strings.HasSuffix(file, ".proto") {
			t.Errorf("File %s does not have .proto extension", file)
		}
	}

	t.Logf("Found %d embedded proto files: %v", len(files), files)
}

// TestReadEmbeddedProtoFileError tests error handling for non-existent files
func TestReadEmbeddedProtoFileError(t *testing.T) {
	_, err := ReadEmbeddedProtoFile("proto/guru/nonexistent/v1/query.proto")
	if err == nil {
		t.Error("Expected error when reading non-existent file")
	}

	if !strings.Contains(err.Error(), "embedded proto file") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}
