package swagger

import (
	"strings"
	"testing"
)

// TestGenerateCustomPathsFromProto tests the complete swagger generation from embedded proto files
func TestGenerateCustomPathsFromProto(t *testing.T) {
	// Generate swagger paths from embedded proto files
	paths, err := GenerateCustomPathsFromProto()
	if err != nil {
		t.Fatalf("Failed to generate custom paths from proto: %v", err)
	}

	// Verify the output contains expected content
	if !strings.Contains(paths, "# Guru Module endpoints (generated from embedded proto files)") {
		t.Error("Expected header comment not found in generated paths")
	}

	// Check for oracle module endpoints
	if !strings.Contains(paths, "/guru/oracle/") {
		t.Error("Expected oracle module endpoints not found")
	}

	// Check for feepolicy module endpoints
	if !strings.Contains(paths, "/guru/feepolicy/") {
		t.Error("Expected feepolicy module endpoints not found")
	}

	// Verify swagger syntax elements
	expectedElements := []string{
		"get:",
		"summary:",
		"operationId:",
		"tags:",
		"responses:",
		"'200':",
		"description:",
		"schema:",
	}

	for _, element := range expectedElements {
		if !strings.Contains(paths, element) {
			t.Errorf("Expected swagger element '%s' not found in generated paths", element)
		}
	}

	t.Logf("Generated paths length: %d characters", len(paths))

	// Log first few lines for debugging
	lines := strings.Split(paths, "\n")
	if len(lines) > 10 {
		t.Logf("First 10 lines of generated paths:")
		for i := 0; i < 10; i++ {
			t.Logf("  %s", lines[i])
		}
	}
}

// TestGetStaticSwaggerContent tests the static swagger content retrieval
func TestGetStaticSwaggerContent(t *testing.T) {
	content, err := GetStaticSwaggerContent()
	if err != nil {
		t.Fatalf("Failed to get static swagger content: %v", err)
	}

	// Verify basic swagger structure
	expectedElements := []string{
		"swagger:",
		"info:",
		"paths:",
		"definitions:",
	}

	for _, element := range expectedElements {
		if !strings.Contains(content, element) {
			t.Errorf("Expected swagger element '%s' not found in static content", element)
		}
	}

	t.Logf("Static swagger content length: %d characters", len(content))
}

// TestMergeSwaggerContent tests merging static swagger with custom paths
func TestMergeSwaggerContent(t *testing.T) {
	// Get static swagger content
	staticSwagger, err := GetStaticSwaggerContent()
	if err != nil {
		t.Fatalf("Failed to get static swagger content: %v", err)
	}

	// Generate custom paths
	customPaths, err := GenerateCustomPathsFromProto()
	if err != nil {
		t.Fatalf("Failed to generate custom paths: %v", err)
	}

	// Merge the content
	mergedSwagger := MergeSwaggerContent(staticSwagger, customPaths)

	// Verify the merged content contains both static and custom content
	if !strings.Contains(mergedSwagger, "# Guru Module endpoints (generated from embedded proto files)") {
		t.Error("Custom paths not found in merged swagger")
	}

	// Verify static content is still present
	if !strings.Contains(mergedSwagger, "swagger:") {
		t.Error("Static swagger content not found in merged result")
	}

	// Verify the merged content is longer than either individual part
	if len(mergedSwagger) <= len(staticSwagger) {
		t.Error("Merged swagger should be longer than static swagger alone")
	}

	if len(mergedSwagger) <= len(customPaths) {
		t.Error("Merged swagger should be longer than custom paths alone")
	}

	t.Logf("Merged swagger length: %d characters", len(mergedSwagger))
	t.Logf("Static swagger length: %d characters", len(staticSwagger))
	t.Logf("Custom paths length: %d characters", len(customPaths))
}

// TestGenerateSwaggerPath tests individual swagger path generation
func TestGenerateSwaggerPath(t *testing.T) {
	// Create a test RPC method
	testMethod := RPCMethod{
		Name:        "QueryParams",
		HTTPPath:    "/guru/oracle/v1/params",
		HTTPMethod:  "GET",
		Summary:     "Query Oracle QueryParams",
		OperationID: "OracleQueryParams",
		Parameters:  []Parameter{},
		Module:      "oracle",
		ServiceType: "Query",
	}

	// Generate swagger path
	swaggerPath := GenerateSwaggerPath(testMethod)

	// Verify the generated path contains expected elements
	expectedElements := []string{
		"/guru/oracle/v1/params:",
		"get:",
		"summary: Query Oracle QueryParams",
		"operationId: OracleQueryParams",
		"tags:",
		"- \"Query\"",
		"responses:",
		"'200':",
		"description: A successful response",
	}

	for _, element := range expectedElements {
		if !strings.Contains(swaggerPath, element) {
			t.Errorf("Expected element '%s' not found in generated swagger path", element)
		}
	}

	t.Logf("Generated swagger path:\n%s", swaggerPath)
}

// TestGenerateSwaggerPathWithParameters tests swagger path generation with parameters
func TestGenerateSwaggerPathWithParameters(t *testing.T) {
	// Create a test RPC method with parameters
	testMethod := RPCMethod{
		Name:        "QueryRequest",
		HTTPPath:    "/guru/oracle/v1/request/{request_id}",
		HTTPMethod:  "GET",
		Summary:     "Query Oracle QueryRequest",
		OperationID: "OracleQueryRequest",
		Parameters: []Parameter{
			{
				Name:     "request_id",
				Type:     "string",
				Required: true,
			},
		},
		Module:      "oracle",
		ServiceType: "Query",
	}

	// Generate swagger path
	swaggerPath := GenerateSwaggerPath(testMethod)

	// Verify parameter is included
	expectedElements := []string{
		"parameters:",
		"- name: request_id",
		"in: path",
		"required: true",
		"type: string",
	}

	for _, element := range expectedElements {
		if !strings.Contains(swaggerPath, element) {
			t.Errorf("Expected parameter element '%s' not found in generated swagger path", element)
		}
	}

	t.Logf("Generated swagger path with parameters:\n%s", swaggerPath)
}
