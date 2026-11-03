package billing

import "testing"

func TestBillable(t *testing.T) {
	cases := []struct {
		status int
		failed bool
		want   bool
	}{
		{200, false, true},
		{201, false, true},
		{304, false, true},
		{400, false, true},
		{404, false, true},
		{429, false, true}, // provider's own 429 is a provider response
		{499, false, true},
		{500, false, false},
		{502, false, false},
		{504, false, false},
		{0, true, false}, // timeout / connection error
	}
	for _, c := range cases {
		if got := Billable(c.status, c.failed); got != c.want {
			t.Errorf("Billable(%d, %v) = %v, want %v", c.status, c.failed, got, c.want)
		}
	}
}

func TestRetryable(t *testing.T) {
	cases := []struct {
		method string
		idem   bool
		status int
		failed bool
		want   bool
	}{
		{"GET", false, 503, false, true},
		{"GET", false, 0, true, true},
		{"GET", false, 500, false, false},
		{"GET", false, 404, false, false},
		{"PUT", false, 502, false, true},
		{"POST", false, 503, false, false}, // never auto-retry a POST
		{"POST", false, 0, true, false},
		{"POST", true, 503, false, true}, // unless it carries an idempotency key
		{"PATCH", false, 504, false, false},
	}
	for _, c := range cases {
		if got := Retryable(c.method, c.idem, c.status, c.failed); got != c.want {
			t.Errorf("Retryable(%s, %v, %d, %v) = %v, want %v", c.method, c.idem, c.status, c.failed, got, c.want)
		}
	}
}
