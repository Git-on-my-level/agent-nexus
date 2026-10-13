package errnorm

import "testing"

func TestSQLiteWriterBusyIsRetryableUnavailable(t *testing.T) {
	err := FromHTTPFailure(503, []byte(`{"error":{"code":"busy","message":"workspace writer is busy","recoverable":true,"hint":"Wait, refresh the board token and retry."}}`))
	if ExitCode(err) != 6 || err.Recoverable == nil || !*err.Recoverable {
		t.Fatalf("contention should be retryable unavailable: exit=%d error=%+v", ExitCode(err), err)
	}
	if ExitCode(FromHTTPFailure(429, []byte(`{"error":{"code":"busy","message":"PM queue is full"}}`))) != 1 {
		t.Fatal("changed existing PM admission exit code")
	}
}
