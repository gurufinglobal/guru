package swagger

import (
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
)

// RegisterCustomSwaggerAPI registers custom swagger API handler with dynamic proto-based content
func RegisterCustomSwaggerAPI(rtr *mux.Router, grpcGateway *gwruntime.ServeMux) {
	// Register custom swagger.yaml handler that serves dynamic content
	// This MUST be registered BEFORE the default static file handler
	rtr.HandleFunc("/swagger/swagger.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")

		// Get the static swagger content
		staticSwagger, err := GetStaticSwaggerContent()
		if err != nil {
			http.Error(w, "Failed to load swagger content", http.StatusInternalServerError)
			return
		}

		// Update the title and description for Guru Chain
		staticSwagger = strings.Replace(staticSwagger, "title: Cosmos SDK - gRPC Gateway docs", "title: Guru Chain API Documentation", 1)
		staticSwagger = strings.Replace(staticSwagger, "description: A REST interface for state queries.", "description: Complete API documentation for Guru Chain including Oracle and FeePolicy modules.", 1)

		// Add global tags section with custom ordering
		tagsSection := `tags:
  - name: "Query"
    description: "Standard Cosmos SDK query endpoints"
  - name: "Service"
    description: "Cosmos SDK service endpoints"
  - name: "default"
    description: ""
`
		// Insert tags section after version
		staticSwagger = strings.Replace(staticSwagger, "version: 1.0.0\npaths:", "version: 1.0.0\n"+tagsSection+"paths:", 1)

		// Generate custom paths dynamically from proto files
		customPaths, err := GenerateCustomPathsFromProto()
		if err != nil {
			http.Error(w, "Failed to generate custom paths", http.StatusInternalServerError)
			return
		}

		// Merge static swagger with custom paths
		mergedSwagger := MergeSwaggerContent(staticSwagger, customPaths)
		w.Write([]byte(mergedSwagger))
	})
}
