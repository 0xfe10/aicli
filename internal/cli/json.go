package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

type Response struct {
	OK    bool       `json:"ok"`
	Data  any        `json:"data,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
	Meta  any        `json:"meta,omitempty"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteJSON(w io.Writer, response Response) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(response)
}

func WriteError(w io.Writer, code string, message string) error {
	return WriteJSON(w, Response{
		OK: false,
		Error: &ErrorBody{
			Code:    code,
			Message: message,
		},
	})
}

func UnknownCommand(w io.Writer, args []string) error {
	return WriteError(w, "UNKNOWN_COMMAND", fmt.Sprintf("Unknown command: %s", joinArgs(args)))
}

func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	result := args[0]
	for _, arg := range args[1:] {
		result += " " + arg
	}
	return result
}
