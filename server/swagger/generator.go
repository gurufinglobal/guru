package swagger

import (
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/client/docs"
)

// GetStaticSwaggerContent retrieves the embedded static swagger content
func GetStaticSwaggerContent() (string, error) {
	swaggerBytes, err := docs.SwaggerUI.ReadFile("swagger-ui/swagger.yaml")
	if err != nil {
		return "", fmt.Errorf("failed to read static swagger: %v", err)
	}
	return string(swaggerBytes), nil
}

// MergeSwaggerContent merges static swagger with custom paths
func MergeSwaggerContent(staticSwagger, customPaths string) string {
	// Find the paths section in the static swagger
	pathsIndex := strings.Index(staticSwagger, "paths:")
	if pathsIndex == -1 {
		// If no paths section found, just append custom paths
		return staticSwagger + "\npaths:" + customPaths
	}

	// Find the end of the paths section by looking for the next top-level section
	pathsEnd := len(staticSwagger)
	searchStart := pathsIndex + 6 // Start after "paths:"

	// Look for next top-level section (starts with letter at beginning of line)
	lines := strings.Split(staticSwagger[searchStart:], "\n")
	for _, line := range lines {
		// Check if line starts a new top-level section (no leading spaces and contains ":")
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && strings.Contains(line, ":") {
			pathsEnd = searchStart + strings.Index(staticSwagger[searchStart:], line)
			break
		}
	}

	// Insert custom paths before the end of paths section
	before := staticSwagger[:pathsEnd]
	after := staticSwagger[pathsEnd:]

	return before + customPaths + "\n" + after
}

// GenerateCustomPathsFromProto reads proto files from embedded proto/guru/ and generates Swagger paths
func GenerateCustomPathsFromProto() (string, error) {
	var paths strings.Builder
	paths.WriteString("  # Guru Module endpoints (generated from embedded proto files)\n")

	// Process embedded proto/guru/ directory
	protoDir := "proto/guru"

	// Scan for modules in embedded proto/guru/ directory
	modules, err := ScanModulesInEmbeddedProtoDir(protoDir)
	if err != nil {
		return "", fmt.Errorf("failed to scan modules in embedded %s: %v", protoDir, err)
	}

	for _, module := range modules {
		// Process both query.proto and tx.proto if they exist
		protoFiles := []string{"query.proto", "tx.proto"}

		for _, protoFile := range protoFiles {
			protoPath := fmt.Sprintf("%s/%s/v1/%s", protoDir, module, protoFile)

			// Check if the proto file exists in embedded filesystem
			if !CheckEmbeddedProtoFileExists(protoPath) {
				continue // Skip if file doesn't exist
			}

			// Read the proto file from embedded filesystem
			content, err := ReadEmbeddedProtoFile(protoPath)
			if err != nil {
				fmt.Printf("Warning: Could not read embedded %s: %v\n", protoPath, err)
				continue
			}

			// Parse RPC methods and their HTTP annotations
			rpcMethods := ParseRPCMethods(content, module)

			// Generate Swagger paths for each RPC method
			for _, rpcMethod := range rpcMethods {
				paths.WriteString(GenerateSwaggerPath(rpcMethod))
			}
		}
	}

	return paths.String(), nil
}

// GenerateSwaggerPath generates a Swagger path definition for an RPC method
func GenerateSwaggerPath(method RPCMethod) string {
	var path strings.Builder

	// Determine the appropriate tag
	var tag string
	if method.ServiceType == "Service" {
		tag = "Service"
	} else if method.ServiceType == "Query" {
		tag = "Query"
	} else {
		tag = "Guru Module" // fallback
	}

	path.WriteString(fmt.Sprintf("  %s:\n", method.HTTPPath))
	path.WriteString(fmt.Sprintf("    %s:\n", strings.ToLower(method.HTTPMethod)))
	path.WriteString(fmt.Sprintf("      summary: %s\n", method.Summary))
	path.WriteString(fmt.Sprintf("      operationId: %s\n", method.OperationID))
	path.WriteString("      tags:\n")
	path.WriteString(fmt.Sprintf("        - \"%s\"\n", tag))

	// Add parameters
	if len(method.Parameters) > 0 || method.HTTPMethod == "POST" {
		path.WriteString("      parameters:\n")

		// Add path parameters if any
		for _, param := range method.Parameters {
			path.WriteString(fmt.Sprintf("        - name: %s\n", param.Name))
			path.WriteString("          in: path\n")
			path.WriteString(fmt.Sprintf("          required: %t\n", param.Required))
			path.WriteString(fmt.Sprintf("          type: %s\n", param.Type))
		}

		// Add request body for POST methods
		if method.HTTPMethod == "POST" {
			path.WriteString("        - name: body\n")
			path.WriteString("          in: body\n")
			path.WriteString("          required: true\n")
			path.WriteString("          schema:\n")
			path.WriteString("            type: object\n")
		}
	}

	path.WriteString("      responses:\n")
	path.WriteString("        '200':\n")
	path.WriteString("          description: A successful response\n")
	path.WriteString("          schema:\n")
	path.WriteString("            type: object\n")

	return path.String()
}
