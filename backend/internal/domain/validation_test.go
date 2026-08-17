package domain

import "testing"

func TestValidListener(t *testing.T) {
	valid := []struct {
		host string
		port int
	}{
		{host: "0.0.0.0", port: 3000},
		{host: "::1", port: 39100},
		{host: "localhost", port: 8080},
		{host: "review-hub.example.test", port: 443},
	}
	for _, value := range valid {
		if !ValidListener(value.host, value.port) {
			t.Errorf("valid listener %q:%d was rejected", value.host, value.port)
		}
	}

	invalid := []struct {
		host string
		port int
	}{
		{host: "", port: 3000},
		{host: "not/a-host", port: 3000},
		{host: "bad host", port: 3000},
		{host: "-leading-dash.example", port: 3000},
		{host: "review-hub.example.", port: 3000},
		{host: "127.0.0.1", port: 0},
		{host: "127.0.0.1", port: 65536},
	}
	for _, value := range invalid {
		if ValidListener(value.host, value.port) {
			t.Errorf("invalid listener %q:%d was accepted", value.host, value.port)
		}
	}
}
