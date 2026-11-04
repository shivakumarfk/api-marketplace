package domain

import (
	"net/textproto"
	"regexp"
	"strings"
)

// Endpoint documents one operation of an API for customers: how to call it
// and what comes back. It is the public contract shown in the catalog and the
// source of the example responses served by "Try it". The gateway does not
// enforce it (it proxies any path under the API).
type Endpoint struct {
	ID                  string
	APIID               string
	Method              string
	Path                string // template relative to the API, e.g. /forecast or /branches/{ifsc}
	Summary             string
	Description         string
	Params              []Param
	Headers             []DocHeader // headers the customer sends (besides X-API-Key)
	RequestExample      string      // body example, for methods that take one
	ResponseStatus      int
	ResponseContentType string
	ResponseExample     string
}

type Param struct {
	Name        string `json:"name"`
	In          string `json:"in"` // path | query
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
}

type DocHeader struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
}

var (
	methods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	// Path characters allowed in a template: unreserved URL characters, '/', and {param}.
	pathTemplateRe = regexp.MustCompile(`^(/([A-Za-z0-9\-._~]+|\{[A-Za-z_][A-Za-z0-9_]{0,63}\}))*/?$`)
	pathParamRe    = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]{0,63})\}`)
	paramNameRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-\[\]]{0,63}$`)
	contentTypeRe  = regexp.MustCompile(`^[a-z0-9.+\-]+/[a-z0-9.+\-]+(;\s*[a-z\-]+=[A-Za-z0-9\-]+)?$`)
)

// Headers the platform manages; documenting them per endpoint would mislead.
var platformHeaders = map[string]bool{"X-Api-Key": true, "Authorization": true, "Cookie": true}

const maxExample = 64 << 10

// Idempotent reports whether the gateway may retry the call (no side effects
// on repeat). Matches the gateway's retry rule.
func (e Endpoint) Idempotent() bool {
	return e.Method == "GET" || e.Method == "PUT" || e.Method == "DELETE"
}

// Normalize validates the endpoint and puts it in canonical form: upper-case
// method, canonical header names, path parameters marked required, defaults
// for the response status and content type.
func (e *Endpoint) Normalize() error {
	e.Method = strings.ToUpper(strings.TrimSpace(e.Method))
	e.Path = strings.TrimSpace(e.Path)
	e.Summary, e.Description = strings.TrimSpace(e.Summary), strings.TrimSpace(e.Description)
	e.ResponseContentType = strings.ToLower(strings.TrimSpace(e.ResponseContentType))
	if e.ResponseContentType == "" {
		e.ResponseContentType = "application/json"
	}
	if e.ResponseStatus == 0 {
		e.ResponseStatus = 200
	}
	switch {
	case !methods[e.Method]:
		return Invalid("INVALID_METHOD", "method must be GET, POST, PUT, PATCH or DELETE")
	case e.Path == "" || !strings.HasPrefix(e.Path, "/") || len(e.Path) > 512 || !pathTemplateRe.MatchString(e.Path):
		return Invalid("INVALID_PATH", "path must start with / and use letters, digits, -._~ and {param} segments; no query string")
	case len(e.Summary) > 200 || len(e.Description) > 5000:
		return Invalid("TOO_LONG", "summary (200) or description (5000) is too long")
	case e.ResponseStatus < 100 || e.ResponseStatus > 599:
		return Invalid("INVALID_STATUS", "response_status must be between 100 and 599")
	case !contentTypeRe.MatchString(e.ResponseContentType):
		return Invalid("INVALID_CONTENT_TYPE", "response_content_type must look like application/json")
	case len(e.RequestExample) > maxExample || len(e.ResponseExample) > maxExample:
		return Invalid("EXAMPLE_TOO_LARGE", "examples are limited to 64 KB")
	case e.RequestExample != "" && (e.Method == "GET" || e.Method == "DELETE"):
		return Invalid("BODY_NOT_ALLOWED", e.Method+" requests have no body; remove the request example")
	}
	if err := e.normalizeParams(); err != nil {
		return err
	}
	return e.normalizeHeaders()
}

func (e *Endpoint) normalizeParams() error {
	if len(e.Params) > 50 {
		return Invalid("TOO_MANY_PARAMS", "at most 50 parameters")
	}
	inPath := map[string]bool{}
	for _, m := range pathParamRe.FindAllStringSubmatch(e.Path, -1) {
		if inPath[m[1]] {
			return Invalid("INVALID_PATH", "path parameter {"+m[1]+"} appears twice")
		}
		inPath[m[1]] = true
	}
	seen := map[string]bool{}
	documented := map[string]bool{}
	out := make([]Param, 0, len(e.Params))
	for _, p := range e.Params {
		p.Name, p.In = strings.TrimSpace(p.Name), strings.TrimSpace(p.In)
		p.Description, p.Example = strings.TrimSpace(p.Description), strings.TrimSpace(p.Example)
		switch {
		case !paramNameRe.MatchString(p.Name):
			return Invalid("INVALID_PARAM", "parameter name "+quote(p.Name)+" is not valid")
		case p.In != "path" && p.In != "query":
			return Invalid("INVALID_PARAM", "parameter "+p.Name+": in must be path or query")
		case seen[p.In+":"+p.Name]:
			return Invalid("INVALID_PARAM", "parameter "+p.Name+" is listed twice")
		case p.In == "path" && !inPath[p.Name]:
			return Invalid("INVALID_PARAM", "path parameter "+p.Name+" is not in the path; add {"+p.Name+"} or make it a query parameter")
		case len(p.Description) > 1000 || len(p.Example) > 1000:
			return Invalid("TOO_LONG", "parameter "+p.Name+": description or example is too long")
		}
		if p.In == "path" {
			p.Required = true // a path segment cannot be omitted
			documented[p.Name] = true
		}
		seen[p.In+":"+p.Name] = true
		out = append(out, p)
	}
	for name := range inPath {
		if !documented[name] {
			return Invalid("INVALID_PARAM", "path parameter {"+name+"} needs a parameter entry")
		}
	}
	e.Params = out
	return nil
}

func (e *Endpoint) normalizeHeaders() error {
	if len(e.Headers) > 30 {
		return Invalid("TOO_MANY_HEADERS", "at most 30 headers")
	}
	seen := map[string]bool{}
	out := make([]DocHeader, 0, len(e.Headers))
	for _, h := range e.Headers {
		name := strings.TrimSpace(h.Name)
		if !headerNameRe.MatchString(name) {
			return Invalid("INVALID_HEADER", "header name "+quote(name)+" is not valid")
		}
		h.Name = textproto.CanonicalMIMEHeaderKey(name)
		h.Description, h.Example = strings.TrimSpace(h.Description), strings.TrimSpace(h.Example)
		switch {
		case platformHeaders[h.Name]:
			return Invalid("INVALID_HEADER", h.Name+" is handled by the platform; don't document it per endpoint")
		case forbiddenAuthHeaders[h.Name] && h.Name != "Content-Type" && h.Name != "Idempotency-Key":
			return Invalid("INVALID_HEADER", h.Name+" is a transport header")
		case seen[h.Name]:
			return Invalid("INVALID_HEADER", h.Name+" is listed twice")
		case len(h.Description) > 1000 || len(h.Example) > 1000:
			return Invalid("TOO_LONG", "header "+h.Name+": description or example is too long")
		}
		seen[h.Name] = true
		out = append(out, h)
	}
	e.Headers = out
	return nil
}
