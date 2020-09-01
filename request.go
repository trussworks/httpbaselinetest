package httpbaselinetest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
)

func formatRequest(r *http.Request) (string, []byte, error) {
	// Create return string
	var request []string
	// Add the request string
	url := fmt.Sprintf("%v %v %v", r.Method, r.URL, r.Proto)
	request = append(request, url)
	// Add the host
	request = append(request, fmt.Sprintf("Host: %v", r.Host))

	if len(r.TransferEncoding) > 0 {
		request = append(request, fmt.Sprintf("Transfer-Encoding: %s", strings.Join(r.TransferEncoding, ",")))
	}
	if r.Close {
		request = append(request, fmt.Sprintf("Connection: close"))
	}

	// Sort headers for deterministic output
	keys := make([]string, 0, len(r.Header))
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Loop through headers
	for _, k := range keys {
		for _, h := range r.Header.Values(k) {
			request = append(request, fmt.Sprintf("%v: %v", k, h))
		}
	}
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return "", nil, err
	}
	if len(body) > 0 {
		request = append(request, fmt.Sprintf("Content-Length: %d", len(body)))
		// make the body readable when the request is processed
		r.Body = ioutil.NopCloser(bytes.NewBuffer(body))
	}
	request = append(request, "")

	ctype := r.Header.Get("Content-Type")
	if ctype == "" {
		ctype = r.Header.Get("Content-type")
	}
	if len(body) > 0 {
		formattedBody, err := formatBody(ctype, body)
		if err != nil {
			return "", nil, err
		}
		request = append(request, formattedBody)
	}

	// Return the request as a string
	return strings.Join(request, "\n"), body, nil
}

func (r *httpBaselineTestRunner) buildRequest() *http.Request {

	var bodyReader io.Reader
	switch v := r.btest.Body.(type) {
	case io.Reader:
		bodyReader = v
	case string:
		bodyReader = strings.NewReader(v)
	case nil:
		// bodyReader is all set
	default:
		data, err := json.MarshalIndent(r.btest.Body, "", "  ")
		if err != nil {
			r.t.Fatalf("Error marshaling json body: %s", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(r.btest.Method, r.btest.Path, bodyReader)
	for key, val := range r.btest.Headers {
		req.Header.Add(key, val)
	}
	return req
}
