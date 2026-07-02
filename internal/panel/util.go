package panel

import (
	"encoding/base64"
)

// util.go: tiny helpers that don't belong to any single handler file.

// base64Decode tries both standard and URL-safe alphabets (with and
// without padding). Caller only cares that they get PEM bytes back —
// which alphabet arrived over the wire is not their problem.
func base64Decode(s string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, base64.CorruptInputError(0)
}
