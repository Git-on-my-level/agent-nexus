package stdio

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/Git-on-my-level/agent-nexus/mcp/protocol"
)

type Options struct {
	Logger *log.Logger
}

func Serve(ctx context.Context, server *protocol.Server, stdin io.Reader, stdout io.Writer, opts Options) error {
	if server == nil {
		return fmt.Errorf("server is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	writer := bufio.NewWriter(stdout)
	defer writer.Flush()

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		response, err := server.Handle(ctx, []byte(line))
		if err != nil {
			logger.Printf("mcp request failed: %v", err)
			continue
		}
		if len(response) == 0 {
			continue
		}
		if _, err := writer.Write(response); err != nil {
			return fmt.Errorf("write response: %w", err)
		}
		if err := writer.WriteByte('\n'); err != nil {
			return fmt.Errorf("write response newline: %w", err)
		}
		if err := writer.Flush(); err != nil {
			return fmt.Errorf("flush response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return nil
}
