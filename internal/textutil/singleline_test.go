package textutil

import "testing"

func TestSingleLine(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"simple string", "simple string"},
		{"line 1\nline 2", "line 1 line 2"},
		{"line 1\r\nline 2\r\nline 3", "line 1 line 2 line 3"},
		{"\n\n  lots   of   spaces   \n\n", "lots of spaces"},
		{"http 400:\n{\n  \"error\": \"bad_request\"\n}", "http 400: { \"error\": \"bad_request\" }"},
		{"tab\tseparated\tvalues", "tab separated values"},
	}

	for _, tc := range cases {
		if got := SingleLine(tc.in); got != tc.want {
			t.Errorf("SingleLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
