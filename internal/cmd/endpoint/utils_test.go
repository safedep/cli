package endpoint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsULID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"valid ULID", "01KR0EKN6PMW0ZRFRN992H1PKX", true},
		{"empty", "", false},
		{"too short", "01KR0EKN6PMW0ZRFRN992H1PK", false},
		{"too long", "01KR0EKN6PMW0ZRFRN992H1PKXX", false},
		{"non-base32", "01KR0EKN6PMW0ZRFRN992H1PK!", false},
		{"hostname-shaped", "laptop-abhi", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isULID(tc.in))
		})
	}
}
