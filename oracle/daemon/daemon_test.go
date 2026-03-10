package daemon

import (
	"testing"

	"github.com/gurufinglobal/guru/v2/oracle/config"
)

func TestNew_EmptyHomeDirFastFails(t *testing.T) {
	t.Parallel()

	_, err := New(&config.Config{}, "")
	if err == nil {
		t.Fatalf("expected error")
	}
}
