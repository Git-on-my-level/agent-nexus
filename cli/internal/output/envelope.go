package output

import (
	"encoding/json"
	"fmt"
	"io"
)

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type Envelope struct {
	OK      bool          `json:"ok"`
	Command string        `json:"command"`
	Data    any           `json:"data,omitempty"`
	Error   *ErrorPayload `json:"error,omitempty"`
}

func WriteEnvelopeJSON(w io.Writer, envelope Envelope) error {
	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if _, err := w.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}
	return nil
}
