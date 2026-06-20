package otx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithLogsPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "url without path gets /v1/logs", in: "http://host:4318", want: "http://host:4318/v1/logs"},
		{name: "url root path gets /v1/logs", in: "http://host:4318/", want: "http://host:4318/v1/logs"},
		{name: "https without path gets /v1/logs", in: "https://host:4318", want: "https://host:4318/v1/logs"},
		{name: "url with explicit path unchanged", in: "http://host:4318/custom", want: "http://host:4318/custom"},
		{name: "bare host:port unchanged", in: "host:4318", want: "host:4318"},
		{name: "empty unchanged", in: "", want: ""},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, withLogsPath(tt.in))
		})
	}
}
