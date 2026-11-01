package observability

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
)

// ErrorReason never returns raw errors: HTTP and database messages can contain
// credentials, SQL parameters, provider bodies, cookies or complete URLs.
func ErrorReason(err error) string {
	var timeout net.Error
	var dns *net.DNSError
	var cert *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalidCert x509.CertificateInvalidError
	var network *net.OpError
	var database interface{ Code() int }
	var syntax *json.SyntaxError
	var value *json.UnmarshalTypeError
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return "timeout"
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &cert), errors.As(err, &unknownCA), errors.As(err, &hostname), errors.As(err, &invalidCert):
		return "tls_certificate"
	case errors.As(err, &network):
		return "connection"
	case errors.Is(err, syscall.ENOTDIR):
		return "not_directory"
	case errors.Is(err, os.ErrExist):
		return "already_exists"
	case errors.Is(err, os.ErrPermission):
		return "permission"
	case errors.Is(err, os.ErrNotExist), errors.Is(err, sql.ErrNoRows):
		return "not_found"
	case errors.Is(err, syscall.ENOSPC):
		return "no_space"
	case errors.As(err, &database):
		return fmt.Sprintf("sqlite_%d", database.Code())
	case errors.As(err, &syntax), errors.As(err, &value):
		return "invalid_json"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "truncated_io"
	default:
		return "internal_error"
	}
}
