package main

import "testing"

func TestOriginOf(t *testing.T) {
	cases := map[string]string{
		"http://localhost:8765":           "http://localhost:8765",
		"http://127.0.0.1:41234/chat":     "http://127.0.0.1:41234",
		"https://pando.example.com/a?b=c": "https://pando.example.com",
		"not a url":                       "",
		"":                                "",
	}
	for in, want := range cases {
		if got := originOf(in); got != want {
			t.Errorf("originOf(%q) = %q, want %q", in, got, want)
		}
	}
}
