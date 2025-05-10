package stubsrv

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathMatch(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		givenTplSegs []string
		givenRawPath string
		expected     bool
	}{
		{
			name:         "exact match",
			givenTplSegs: []string{"users", "42", "orders"},
			givenRawPath: "/users/42/orders",
			expected:     true,
		},
		{
			name:         "template with params matches any value",
			givenTplSegs: []string{"users", ":id", "orders", ":orderId"},
			givenRawPath: "/users/99/orders/123",
			expected:     true,
		},
		{
			name:         "segment mismatch returns false",
			givenTplSegs: []string{"users", "42"},
			givenRawPath: "/accounts/42",
			expected:     false,
		},
		{
			name:         "different number of segments returns false",
			givenTplSegs: []string{"foo", "bar"},
			givenRawPath: "/foo/bar/baz",
			expected:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := pathMatch(tc.givenTplSegs, tc.givenRawPath)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestQueryMatch(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		givenTpl     map[string]string
		givenURLVals url.Values
		expected     bool
	}{
		{
			name:         "empty template always matches",
			givenTpl:     map[string]string{},
			givenURLVals: url.Values{"foo": []string{"1"}},
			expected:     true,
		},
		{
			name:         "exact query match",
			givenTpl:     map[string]string{"status": "shipped", "type": "expedited"},
			givenURLVals: mustParseQuery(t, "status=shipped&type=expedited"),
			expected:     true,
		},
		{
			name:         "value mismatch returns false",
			givenTpl:     map[string]string{"status": "shipped"},
			givenURLVals: mustParseQuery(t, "status=pending"),
			expected:     false,
		},
		{
			name:         "template subset matches even with extra query params",
			givenTpl:     map[string]string{"foo": "1"},
			givenURLVals: mustParseQuery(t, "foo=1&bar=2"),
			expected:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := queryMatch(tc.givenTpl, tc.givenURLVals)
			assert.Equal(t, tc.expected, got)
		})
	}
}

func mustParseQuery(t *testing.T, q string) url.Values {
	t.Helper()
	v, err := url.ParseQuery(q)
	require.NoError(t, err)
	return v
}
