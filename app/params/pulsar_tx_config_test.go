package params

import (
	"errors"
	"testing"
)

func TestShouldUsePulsarFallbackOnlyForGuruNestedMessageLookup(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "guru nested pulsar message",
			err:  errors.New(`decoding tx: failed to retrieve the message of type "guru.oracle.v1.OracleTask": tx parse error`),
			want: true,
		},
		{
			name: "non guru nested message",
			err:  errors.New(`decoding tx: failed to retrieve the message of type "cosmos.bank.v1beta1.MsgSend": tx parse error`),
			want: false,
		},
		{
			name: "ordinary decode error",
			err:  errors.New("txRaw must follow ADR-027"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUsePulsarFallback(tt.err); got != tt.want {
				t.Fatalf("shouldUsePulsarFallback() = %t, want %t", got, tt.want)
			}
		})
	}
}
