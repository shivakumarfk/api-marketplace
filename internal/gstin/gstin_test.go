package gstin

import "testing"

func TestValidateFormat(t *testing.T) {
	valid := []string{
		"29ABCDE1234F1Z5",   // Karnataka
		"27aaacb1234c1zx",   // lower case is normalised
		" 07ABCDE1234F2ZA ", // surrounding spaces trimmed
	}
	for _, s := range valid {
		if err := ValidateFormat(s); err != nil {
			t.Errorf("%q: unexpected error %v", s, err)
		}
	}

	invalid := map[string]error{
		"":                 ErrFormat,
		"29ABCDE1234F1Z":   ErrFormat, // 14 chars
		"29ABCDE1234F1Z55": ErrFormat, // 16 chars
		"29ABCDE1234F1X5":  ErrFormat, // 14th char must be Z
		"29ABCDE1234F0Z5":  ErrFormat, // entity number cannot be 0
		"2XABCDE1234F1Z5":  ErrFormat, // state code not numeric
		"29ABCD12345F1Z5":  ErrFormat, // PAN shape wrong
		"00ABCDE1234F1Z5":  ErrStateCode,
		"45ABCDE1234F1Z5":  ErrStateCode,
	}
	for s, want := range invalid {
		if err := ValidateFormat(s); err != want {
			t.Errorf("%q: got %v, want %v", s, err, want)
		}
	}
}
