package coordinator

import (
	"io"
	"net/http"
	"net/http/httptest"

	"planetary-mesh/internal/protocol"
)

func newVersionedRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	protocol.SetVersionHeader(req.Header)
	return req
}
