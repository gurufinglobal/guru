package swagger

import (
	"fmt"
	"os"
	"strings"
)

// RPCMethod represents a parsed RPC method from proto file
type RPCMethod struct {
	Name        string
	HTTPPath    string
	HTTPMethod  string // GET or POST
	Summary     string
	OperationID string
	Parameters  []Parameter
	Module      string
	ServiceType string // "Query" or "Msg"
}

// Parameter represents a path parameter
type Parameter struct {
	Name     string
	Type     string
	Required bool
}

// ScanModulesInProtoDir scans proto/guru/ directory for module subdirectories
// Deprecated: Use ScanModulesInEmbeddedProtoDir for embedded filesystem support
func ScanModulesInProtoDir(protoDir string) ([]string, error) {
	var modules []string

	// Read the directory
	entries, err := os.ReadDir(protoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %v", protoDir, err)
	}

	// Look for subdirectories that contain v1/query.proto or v1/tx.proto
	for _, entry := range entries {
		if entry.IsDir() {
			moduleName := entry.Name()
			queryProtoPath := fmt.Sprintf("%s/%s/v1/query.proto", protoDir, moduleName)
			txProtoPath := fmt.Sprintf("%s/%s/v1/tx.proto", protoDir, moduleName)

			// Check if query.proto or tx.proto exists
			if _, err := os.Stat(queryProtoPath); err == nil {
				modules = append(modules, moduleName)
				// fmt.Printf("Found module: %s (with query.proto)\n", moduleName)
			} else if _, err := os.Stat(txProtoPath); err == nil {
				modules = append(modules, moduleName)
				// fmt.Printf("Found module: %s (with tx.proto)\n", moduleName)
			}
		}
	}

	return modules, nil
}

// ParseRPCMethods extracts RPC methods and their HTTP annotations from proto content
func ParseRPCMethods(content, module string) []RPCMethod {
	var methods []RPCMethod
	lines := strings.Split(content, "\n")

	// Determine service type from content
	serviceType := "Query" // default
	if strings.Contains(content, "service Msg") {
		serviceType = "Msg"
	}

	for i, line := range lines {
		// Look for RPC method definitions
		if strings.Contains(line, "rpc ") && strings.Contains(line, "(") {
			// Extract method name
			parts := strings.Fields(strings.TrimSpace(line))
			if len(parts) >= 2 {
				methodName := parts[1]
				methodName = strings.Split(methodName, "(")[0]

				// Look for HTTP annotation in the next few lines
				httpPath := ""
				httpMethod := ""

				for j := i + 1; j < len(lines) && j < i+10; j++ {
					if strings.Contains(lines[j], "option (google.api.http)") {
						// Look for GET method
						if strings.Contains(lines[j], ".get") {
							start := strings.Index(lines[j], `"`)
							end := strings.LastIndex(lines[j], `"`)
							if start != -1 && end != -1 && start < end {
								httpPath = lines[j][start+1 : end]
								httpMethod = "GET"
							}
							break
						}
						// Look for POST method in multi-line format
						if strings.Contains(lines[j], "= {") {
							// Multi-line HTTP annotation, look for post: in next lines
							for k := j + 1; k < len(lines) && k < j+5; k++ {
								if strings.Contains(lines[k], "post:") {
									start := strings.Index(lines[k], `"`)
									end := strings.LastIndex(lines[k], `"`)
									if start != -1 && end != -1 && start < end {
										httpPath = lines[k][start+1 : end]
										httpMethod = "POST"
									}
									break
								}
							}
							break
						}
					}
				}

				if httpPath != "" && httpMethod != "" {
					// Parse parameters from path
					params := parsePathParameters(httpPath)

					var summary string
					var tag string
					if serviceType == "Msg" {
						summary = fmt.Sprintf("%s %s transaction", strings.Title(module), methodName)
						tag = "Service"
					} else {
						summary = fmt.Sprintf("Query %s %s", strings.Title(module), methodName)
						tag = "Query"
					}

					methods = append(methods, RPCMethod{
						Name:        methodName,
						HTTPPath:    httpPath,
						HTTPMethod:  httpMethod,
						Summary:     summary,
						OperationID: fmt.Sprintf("%s%s", strings.Title(module), methodName),
						Parameters:  params,
						Module:      module,
						ServiceType: tag,
					})
				}
			}
		}
	}

	return methods
}

// parsePathParameters extracts parameters from HTTP path like {request_id}
func parsePathParameters(path string) []Parameter {
	var params []Parameter

	// Find all parameters in curly braces
	start := 0
	for {
		startIdx := strings.Index(path[start:], "{")
		if startIdx == -1 {
			break
		}
		startIdx += start

		endIdx := strings.Index(path[startIdx:], "}")
		if endIdx == -1 {
			break
		}
		endIdx += startIdx

		paramName := path[startIdx+1 : endIdx]
		params = append(params, Parameter{
			Name:     paramName,
			Type:     "string", // Default to string type
			Required: true,
		})

		start = endIdx + 1
	}

	return params
}
