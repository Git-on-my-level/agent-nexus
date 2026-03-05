package streaming

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Event represents one SSE event frame.
type Event struct {
	ID    string
	Type  string
	Data  string
	Retry string
}

// ReadEvent reads the next SSE event from the reader.
// Comments and empty keepalive frames are skipped.
func ReadEvent(reader *bufio.Reader) (Event, error) {
	var event Event
	dataLines := make([]string, 0, 4)
	hasAnyField := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if hasAnyField {
					event.Data = strings.Join(dataLines, "\n")
					return event, nil
				}
				return Event{}, io.EOF
			}
			return Event{}, fmt.Errorf("read sse line: %w", err)
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if hasAnyField {
				event.Data = strings.Join(dataLines, "\n")
				return event, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}

		key, value, ok := strings.Cut(line, ":")
		if !ok {
			key = line
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		hasAnyField = true
		switch key {
		case "id":
			event.ID = value
		case "event":
			event.Type = value
		case "data":
			dataLines = append(dataLines, value)
		case "retry":
			event.Retry = value
		}
	}
}
