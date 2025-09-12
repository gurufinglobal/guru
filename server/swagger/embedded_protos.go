package swagger

import (
	"fmt"
)

// Proto file contents are generated as constants in embedded_protos_generated.go
// This approach avoids Go embed path limitations with relative paths

// ScanModulesInEmbeddedProtoDir scans embedded proto/guru/ directory for module subdirectories
func ScanModulesInEmbeddedProtoDir(protoDir string) ([]string, error) {
	// Since we embedded specific files as strings, we know the modules that exist
	// Return the hardcoded list of available modules
	modules := []string{"oracle", "feepolicy"}
	return modules, nil
}

// ReadEmbeddedProtoFile reads a proto file from the embedded strings
func ReadEmbeddedProtoFile(protoPath string) (string, error) {
	// Map proto paths to embedded strings
	switch protoPath {
	case "proto/guru/oracle/v1/query.proto":
		return oracleQueryProto, nil
	case "proto/guru/oracle/v1/tx.proto":
		return oracleTxProto, nil
	case "proto/guru/feepolicy/v1/query.proto":
		return feepolicyQueryProto, nil
	case "proto/guru/feepolicy/v1/tx.proto":
		return feepolicyTxProto, nil
	default:
		return "", fmt.Errorf("embedded proto file %s not found", protoPath)
	}
}

// CheckEmbeddedProtoFileExists checks if a proto file exists in the embedded strings
func CheckEmbeddedProtoFileExists(protoPath string) bool {
	// Check if the path maps to one of our embedded files
	switch protoPath {
	case "proto/guru/oracle/v1/query.proto",
		"proto/guru/oracle/v1/tx.proto",
		"proto/guru/feepolicy/v1/query.proto",
		"proto/guru/feepolicy/v1/tx.proto":
		return true
	default:
		return false
	}
}

// ListEmbeddedProtoFiles lists all proto files in the embedded strings for debugging
func ListEmbeddedProtoFiles() ([]string, error) {
	// Return the hardcoded list of embedded proto files
	files := []string{
		"proto/guru/oracle/v1/query.proto",
		"proto/guru/oracle/v1/tx.proto",
		"proto/guru/feepolicy/v1/query.proto",
		"proto/guru/feepolicy/v1/tx.proto",
	}
	return files, nil
}
