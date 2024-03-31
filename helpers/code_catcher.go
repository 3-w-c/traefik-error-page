package helpers

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
)

// CodeCatcher is a response writer that detects as soon as possible
// whether the response is a code within the ranges of codes it watches for.
// If it is, it simply drops the data from the response.
// Otherwise, it forwards it directly to the original client (its responseWriter) without any buffering.
type CodeCatcher struct {
	headerMap          http.Header
	code               int
	httpCodeRanges     HTTPCodeRanges
	caughtFilteredCode bool
	caughtUnmatchingBody bool
	responseWriter     http.ResponseWriter
	headersSent        bool
	contentsOnly       bool
	contentsOnlyMatch  string
}

// NewCodeCatcher creates a new CodeCatcher.
func NewCodeCatcher(rw http.ResponseWriter, httpCodeRanges HTTPCodeRanges, contentsOnly bool, contentsOnlyMatch string) *CodeCatcher {
	return &CodeCatcher{
		headerMap:      make(http.Header),
		code:           http.StatusOK, // If backend does not call WriteHeader on us, we consider it's a 200.
		responseWriter: rw,
		httpCodeRanges: httpCodeRanges,
		contentsOnly:   contentsOnly,
		contentsOnlyMatch:   contentsOnlyMatch,
	}
}

// Header gets the captured headers.
func (cc *CodeCatcher) Header() http.Header {
	if cc.headersSent {
		return cc.responseWriter.Header()
	}

	if cc.headerMap == nil {
		cc.headerMap = make(http.Header)
	}

	return cc.headerMap
}

// GetCode gets the captured status code.
func (cc *CodeCatcher) GetCode() int {
	return cc.code
}

// IsFilteredCode returns whether the codeCatcher received a response code among the ones it is watching,
// and for which the response should be deferred to the error handler.
func (cc *CodeCatcher) IsFilteredCode() bool {
	return cc.caughtFilteredCode
}

// BodyMatches tells if the response body was the same as the specified
// contentsOnlyMatch
// FIXME: this is a bit of a misnomer, it's really checking if it passed the
// test. So if cc.contentsOnly is false, always return true.
func (cc *CodeCatcher) IsMatchingBody() bool {
	return !cc.contentsOnly || !cc.caughtUnmatchingBody
}

// Write writes the response or ignores it.
func (cc *CodeCatcher) Write(buf []byte) (int, error) {
	// If WriteHeader was already called from the caller, this is a NOOP.
	// Otherwise, cc.code is actually a 200 here.
	cc.WriteHeader(cc.code)

	if cc.caughtFilteredCode && !cc.contentsOnly {
		// We don't care about the contents of the response,
		// since we want to serve the ones from the error page,
		// so we just drop them.
		return len(buf), nil
	}

	// write the value because was ignored in the WriteHeader below
	if !cc.caughtUnmatchingBody && !cc.headersSent {
		// The copy is not appending the values,
		// to not repeat them in case any informational status code has been written.
		for k, v := range cc.Header() {
			cc.responseWriter.Header()[k] = v
		}
		cc.responseWriter.WriteHeader(cc.code)
		cc.headersSent = true
	}

	if cc.contentsOnly {
    // Convert the body back to a string for comparison
    bodyString := string(buf)

    if bodyString != cc.contentsOnlyMatch {
			cc.caughtUnmatchingBody = true;
		} else {
			return len(buf), nil
		}
	}

	return cc.responseWriter.Write(buf)
}

// WriteHeader is, in the specific case of 1xx status codes, a direct call to the wrapped ResponseWriter, without marking headers as sent,
// allowing so further calls.
func (cc *CodeCatcher) WriteHeader(code int) {
	if cc.headersSent || (cc.caughtFilteredCode && !cc.contentsOnly) {
		return
	}

	// Handling informational headers.
	if code >= 100 && code <= 199 {
		// Multiple informational status codes can be used,
		// so here the copy is not appending the values to not repeat them.
		for k, v := range cc.Header() {
			cc.responseWriter.Header()[k] = v
		}

		cc.responseWriter.WriteHeader(code)
		return
	}

	cc.code = code
	for _, block := range cc.httpCodeRanges {
		if cc.code >= block[0] && cc.code <= block[1] {
			cc.caughtFilteredCode = true
			// it will be up to the caller to send the headers,
			// so it is out of our hands now.
			return
		}
	}

	// The copy is not appending the values,
	// to not repeat them in case any informational status code has been written.
	for k, v := range cc.Header() {
		cc.responseWriter.Header()[k] = v
	}
	cc.responseWriter.WriteHeader(cc.code)
	cc.headersSent = true
}

// Hijack hijacks the connection.
func (cc *CodeCatcher) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := cc.responseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("%T is not a http.Hijacker", cc.responseWriter)
}

// Flush sends any buffered data to the client.
func (cc *CodeCatcher) Flush() {
	// If WriteHeader was already called from the caller, this is a NOOP.
	// Otherwise, cc.code is actually a 200 here.
	cc.WriteHeader(cc.code)

	// We don't care about the contents of the response,
	// since we want to serve the ones from the error page,
	// so we just don't flush.
	// (e.g., To prevent superfluous WriteHeader on request with a
	// `Transfert-Encoding: chunked` header).
	if cc.caughtFilteredCode {
		return
	}

	if flusher, ok := cc.responseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
