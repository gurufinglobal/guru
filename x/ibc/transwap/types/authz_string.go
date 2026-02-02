package types

import (
	"fmt"
	"strings"
)

// gogoproto's legacy proto.Message interface requires String() string.
//
// We disable gogoproto stringer generation in the .proto (to keep output stable),
// so we provide minimal String() methods here to satisfy the interface.
func (p AllowedPath) String() string {
	return fmt.Sprintf("%s/%s", p.PortId, p.ChannelId)
}

func (a RecoverClientAuthorization) String() string {
	if len(a.AllowedPaths) == 0 {
		return fmt.Sprintf("RecoverClientAuthorization{msg_type_url=%s, allowed_paths=[]}", a.MsgTypeUrl)
	}

	paths := make([]string, 0, len(a.AllowedPaths))
	for _, p := range a.AllowedPaths {
		paths = append(paths, p.String())
	}
	return fmt.Sprintf("RecoverClientAuthorization{msg_type_url=%s, allowed_paths=[%s]}", a.MsgTypeUrl, strings.Join(paths, ","))
}
