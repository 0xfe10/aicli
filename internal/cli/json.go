package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Response is the stable stdout JSON document for service CLIs.
type Response struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
	Meta  any        `json:"meta,omitempty"`
}

// ErrorBody is the stable failure payload.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes exactly one JSON document to w.
func WriteJSON(w io.Writer, response Response) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(response)
}

// WriteError writes a failed JSON document.
func WriteError(w io.Writer, code string, message string) error {
	return WriteJSON(w, Response{
		OK: false,
		Error: &ErrorBody{
			Code:    code,
			Message: message,
		},
	})
}

// WriteOK writes a successful JSON document.
func WriteOK(w io.Writer, data any, meta any) error {
	return WriteJSON(w, Response{
		OK:   true,
		Data: data,
		Meta: meta,
	})
}

// UnknownCommand writes UNKNOWN_COMMAND for unrecognized argv.
func UnknownCommand(w io.Writer, args []string) error {
	return WriteError(w, "UNKNOWN_COMMAND", fmt.Sprintf("Unknown command: %s", strings.Join(args, " ")))
}
