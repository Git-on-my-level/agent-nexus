package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/schema"
)

func TestReadAdmissionPrioritizesAnswersAndCancelsWaiters(t *testing.T) {
	a := newReadAdmission()
	a.limit = 1
	read, err := a.read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, second := a.answer(), a.answer()
	done := make(chan func(), 1)
	go func() {
		release, err := a.read(context.Background())
		if err == nil {
			done <- release
		}
	}()
	read()
	first()
	select {
	case release := <-done:
		release()
		t.Error("reader passed a pending answer")
	case <-time.After(20 * time.Millisecond):
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.read(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled waiter: %v", err)
	}
	second()
	select {
	case release := <-done:
		release()
	case <-time.After(time.Second):
		t.Fatal("reader was not released")
	}
	release, err := a.read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}

type slowProjectionBody struct {
	started, release chan struct{}
	once             sync.Once
	body             io.Reader
}

func (b *slowProjectionBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.body.Read(p)
}

func TestSlowProjectionUploadDoesNotReserveReadLane(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedHumanPrincipalForLockoutTest(t, context.Background(), env.workspace.DB(), "upload-owner", "upload-actor", "upload-owner", "upload-token")
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(contract.Version, WithPrimitiveStore(env.primitiveStore), WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithSchemaContract(contract))
	for _, path := range []string{"/inbox/slow-body/respond", "/reports/preview"} {
		t.Run(path, func(t *testing.T) {
			body := &slowProjectionBody{started: make(chan struct{}), release: make(chan struct{}), body: strings.NewReader(`{}`)}
			request := httptest.NewRequest(http.MethodPost, path, body)
			request.Header.Set("Authorization", "Bearer "+owner.AccessToken)
			uploadDone := make(chan struct{})
			go func() { defer close(uploadDone); h.ServeHTTP(httptest.NewRecorder(), request) }()
			select {
			case <-body.started:
			case <-time.After(3 * time.Second):
				close(body.release)
				<-uploadDone
				t.Fatal("upload did not reach body read")
			}
			getDone := make(chan int, 1)
			go func() {
				request := httptest.NewRequest(http.MethodGet, "/events?limit=1", nil)
				request.Header.Set("Authorization", "Bearer "+owner.AccessToken)
				response := httptest.NewRecorder()
				h.ServeHTTP(response, request)
				getDone <- response.Code
			}()
			select {
			case status := <-getDone:
				if status != 200 {
					t.Errorf("GET status=%d", status)
				}
				close(body.release)
			case <-time.After(2 * time.Second):
				t.Error("slow upload blocked the read lane")
				close(body.release)
				<-getDone
			}
			<-uploadDone
		})
	}
}
