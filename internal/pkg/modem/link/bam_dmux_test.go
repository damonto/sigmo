package link

import (
	"errors"
	"testing"

	"github.com/damonto/wwan-go/qcom"
)

func TestQualcomm410RawIPLeaseCloseDetachesClientAfterFailure(t *testing.T) {
	client := &qcom.Client{}
	clientErr := errors.New("close client")
	clientAttempts := 0
	closeClient := func(*qcom.Client) error {
		clientAttempts++
		return clientErr
	}

	lease := &Qualcomm410RawIPLease{client: client}
	if err := lease.closeWith(closeClient); !errors.Is(err, clientErr) {
		t.Fatalf("first Close() error = %v, want %v", err, clientErr)
	}
	if lease.client != nil {
		t.Fatal("client was retained after terminal Close")
	}
	if clientAttempts != 1 {
		t.Fatalf("client close attempts = %d, want 1", clientAttempts)
	}
	if err := lease.closeWith(closeClient); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
	if clientAttempts != 1 {
		t.Fatal("second Close retried detached resources")
	}
}
