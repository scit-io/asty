package utils

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/micro"
)

// JSON replies with a JSON-wrapped {"data": ...} body, the supplied
// HTTP status (carried in the Status header so the gateway forwards
// it), and any Set-Cookie strings passed via the variadic cookies
// argument. Set-Cookie headers can repeat; other headers can't, which
// is why cookies get their own slot.
//
// The json.Marshal error is intentionally ignored: the inputs are
// always plain maps/structs of JSON-serialisable values, and the
// alternative (returning an error a handler couldn't meaningfully
// handle) would just push the same panic deeper.
func JSON(req micro.Request, status int, data any, cookies ...string) {
	body, _ := json.Marshal(map[string]any{"data": data})
	headers := micro.Headers{
		"Status":       []string{fmt.Sprintf("%d", status)},
		"Content-Type": []string{"application/json"},
	}
	if len(cookies) > 0 {
		headers["Set-Cookie"] = cookies
	}
	req.Respond(body, micro.WithHeaders(headers))
}

// Error replies with a JSON {"error": text} body and the supplied
// status. Same json.Marshal rationale as JSON.
func Error(req micro.Request, status int, text string) {
	body, _ := json.Marshal(map[string]string{"error": text})
	headers := micro.Headers{
		"Status":       []string{fmt.Sprintf("%d", status)},
		"Content-Type": []string{"application/json"},
	}
	req.Respond(body, micro.WithHeaders(headers))
}
