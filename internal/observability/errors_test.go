package observability

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
)

func TestErrorReasonClassifiesWithoutExposingRawMessages(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "canceled"},
		{&url.Error{Op: "Get", URL: "https://secret:password@example.com/?token=private", Err: context.DeadlineExceeded}, "timeout"},
		{&net.DNSError{Err: "private-provider-message", Name: "private-secret.example"}, "dns"},
		{x509.UnknownAuthorityError{}, "tls_certificate"},
		{&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("secret internal address")}, "connection"},
		{&os.PathError{Op: "open", Path: "private path", Err: os.ErrPermission}, "permission"},
		{os.ErrNotExist, "not_found"},
		{syscall.ENOSPC, "no_space"},
		{io.ErrUnexpectedEOF, "truncated_io"},
		{errors.New("token=private unauthorized secret"), "internal_error"},
		{nil, "none"},
	} {
		if got := ErrorReason(tc.err); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}
