package oracle

import (
	"fmt"
)

func ConfigTemplate(homeDir string) string {
	cfg := DefaultConfig(homeDir)

	return fmt.Sprintf(`# Guru oracle sidecar daemon configuration.
socket = %q
request_timeout = %q
source_timeout = %q
node_grpc = %q
node_query_timeout = %q

# Operators add local numeric sources. The daemon only serves samples for chain
# tasks whose symbol and value_type match configured sources. Non-numeric value
# types are reserved for future use and are rejected in oracle v1.
#
#[[sources]]
#name = "example-btc-usd"
#symbol = "BTC/USD"
#value_type = "NUMERIC"
#url = "https://example.invalid/prices/btc-usd"
#response_path = "data.price"
#timeout = "300ms"
#interval = "1s"
#[sources.headers]
#Authorization = "Bearer token"
`, cfg.Socket, cfg.RequestTimeout, cfg.SourceTimeout, cfg.NodeGRPC, cfg.NodeQueryTimeout)
}
