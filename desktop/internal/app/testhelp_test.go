package app

import (
	"encoding/json"
	"net/http"
	"testing"
)

func decode(t *testing.T, response *http.Response, target any) error {
	t.Helper()
	return json.NewDecoder(response.Body).Decode(target)
}
