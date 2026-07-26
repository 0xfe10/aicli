package pingcode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// DecodeStrictJSON decodes exactly one JSON document into dest.
// Unknown fields, empty input, trailing garbage, and a second document are rejected.
func DecodeStrictJSON(raw []byte, dest any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return NewError(CodeInvalidArgument, "stdin JSON 为空")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return NewError(CodeInvalidArgument, formatJSONDecodeError(err))
	}
	// Reject a second JSON value or trailing non-whitespace.
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return NewError(CodeInvalidArgument, "stdin 只能包含一个 JSON 文档")
		}
		// Decoder may report syntax error for trailing garbage.
		return NewError(CodeInvalidArgument, "stdin 包含尾随内容，只能有一个 JSON 文档")
	}
	return nil
}

func formatJSONDecodeError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unknown field"):
		return "未知 JSON 字段: " + msg
	case strings.Contains(msg, "unexpected end of JSON"):
		return "stdin JSON 不完整"
	default:
		return fmt.Sprintf("stdin JSON 无效: %s", msg)
	}
}
