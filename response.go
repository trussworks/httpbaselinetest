package httpbaselinetest

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
)

func formatResponse(r *http.Response) (string, []byte, error) {
	// Create return string
	var response []string
	status := fmt.Sprintf("%s %s", r.Proto, r.Status)
	response = append(response, status)

	// Sort headers for deterministic output
	keys := make([]string, 0, len(r.Header))
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Loop through headers
	for _, k := range keys {
		for _, h := range r.Header.Values(k) {
			response = append(response, fmt.Sprintf("%v: %v", k, h))
		}
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return "", nil, err
	}
	response = append(response, "")

	ctype := r.Header.Get("Content-Type")
	if ctype == "" {
		ctype = r.Header.Get("Content-type")
	}
	if len(body) > 0 {
		formattedBody, err := formatBody(ctype, body)
		if err != nil {
			return "", nil, err
		}
		response = append(response, formattedBody)
	}

	// Return the request as a string
	return strings.Join(response, "\n"), body, nil
}
