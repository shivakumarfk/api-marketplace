package domain

import (
	"strings"
	"testing"
)

func TestEndpointNormalize(t *testing.T) {
	e := Endpoint{Method: "get", Path: "/branches/{ifsc}", Summary: " Branch ",
		Params: []Param{
			{Name: "ifsc", In: "path", Example: "HDFC0CAGSBK"},
			{Name: "fields", In: "query", Required: false},
		},
		Headers: []DocHeader{{Name: "accept-language", Example: "en-IN"}},
	}
	if err := e.Normalize(); err != nil {
		t.Fatal(err)
	}
	if e.Method != "GET" || e.Summary != "Branch" || e.ResponseStatus != 200 || e.ResponseContentType != "application/json" {
		t.Fatalf("%+v", e)
	}
	if !e.Params[0].Required || e.Params[1].Required {
		t.Fatal("path params are always required; query params keep their flag")
	}
	if e.Headers[0].Name != "Accept-Language" || !e.Idempotent() {
		t.Fatalf("%+v", e)
	}

	root := Endpoint{Method: "POST", Path: "/", RequestExample: `{"a":1}`, ResponseStatus: 201,
		Headers: []DocHeader{{Name: "content-type", Required: true}, {Name: "Idempotency-Key"}}}
	if err := root.Normalize(); err != nil || root.Idempotent() {
		t.Fatalf("root POST: %v", err)
	}
}

func TestEndpointNormalizeRejects(t *testing.T) {
	ok := func() Endpoint { return Endpoint{Method: "GET", Path: "/today"} }
	cases := map[string]struct {
		mut  func(*Endpoint)
		code string
	}{
		"method":            {func(e *Endpoint) { e.Method = "TRACE" }, "INVALID_METHOD"},
		"no slash":          {func(e *Endpoint) { e.Path = "today" }, "INVALID_PATH"},
		"query in path":     {func(e *Endpoint) { e.Path = "/today?city=x" }, "INVALID_PATH"},
		"bad chars":         {func(e *Endpoint) { e.Path = "/to day" }, "INVALID_PATH"},
		"half brace":        {func(e *Endpoint) { e.Path = "/a/{id" }, "INVALID_PATH"},
		"dup path param":    {func(e *Endpoint) { e.Path = "/{id}/{id}"; e.Params = []Param{{Name: "id", In: "path"}} }, "INVALID_PATH"},
		"undocumented":      {func(e *Endpoint) { e.Path = "/a/{id}" }, "INVALID_PARAM"},
		"path param absent": {func(e *Endpoint) { e.Params = []Param{{Name: "id", In: "path"}} }, "INVALID_PARAM"},
		"param in":          {func(e *Endpoint) { e.Params = []Param{{Name: "x", In: "header"}} }, "INVALID_PARAM"},
		"param name":        {func(e *Endpoint) { e.Params = []Param{{Name: "1x", In: "query"}} }, "INVALID_PARAM"},
		"dup param":         {func(e *Endpoint) { e.Params = []Param{{Name: "q", In: "query"}, {Name: "q", In: "query"}} }, "INVALID_PARAM"},
		"status":            {func(e *Endpoint) { e.ResponseStatus = 700 }, "INVALID_STATUS"},
		"content type":      {func(e *Endpoint) { e.ResponseContentType = "json" }, "INVALID_CONTENT_TYPE"},
		"GET body":          {func(e *Endpoint) { e.RequestExample = "{}" }, "BODY_NOT_ALLOWED"},
		"huge example":      {func(e *Endpoint) { e.ResponseExample = strings.Repeat("x", 64<<10+1) }, "EXAMPLE_TOO_LARGE"},
		"api key header":    {func(e *Endpoint) { e.Headers = []DocHeader{{Name: "x-api-key"}} }, "INVALID_HEADER"},
		"host header":       {func(e *Endpoint) { e.Headers = []DocHeader{{Name: "Host"}} }, "INVALID_HEADER"},
		"dup header":        {func(e *Endpoint) { e.Headers = []DocHeader{{Name: "X-A"}, {Name: "x-a"}} }, "INVALID_HEADER"},
	}
	for label, c := range cases {
		e := ok()
		c.mut(&e)
		wantCode(t, label, e.Normalize(), c.code)
	}
}
