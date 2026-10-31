package cloudpolicy

import (
	"strings"
	"testing"

	messagescontroltowerv1 "buf.build/gen/go/safedep/api/protocolbuffers/go/safedep/messages/controltower/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCustomLabels(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr string
	}{
		{name: "one label", in: []string{"custom/criticality=high"}, want: []string{"custom/criticality=high"}},
		{name: "empty value", in: []string{"custom/flag="}, want: []string{"custom/flag="}},
		{name: "value keeps an equals sign", in: []string{"custom/expr=a=b"}, want: []string{"custom/expr=a=b"}},
		{name: "no labels", in: nil, wantErr: "at least one label"},
		{name: "no value", in: []string{"custom/team"}, wantErr: "has no value"},
		{name: "other prefix", in: []string{"scm/forge=github"}, wantErr: `an admin writes only "custom/" labels`},
		{name: "managed prefix", in: []string{"managed/department=x"}, wantErr: `"managed/" labels`},
		{name: "bad name", in: []string{"custom/a b=x"}, wantErr: "is not valid"},
		{name: "long value", in: []string{"custom/a=" + strings.Repeat("x", 64)}, wantErr: "the limit is 63"},
		{name: "duplicate", in: []string{"custom/a=1", "custom/a=2"}, wantErr: "given twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCustomLabels(tt.in)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, FormatLabels(got))
		})
	}
}

func TestValidateCustomKeys(t *testing.T) {
	require.NoError(t, ValidateCustomKeys([]string{"custom/team", "custom/criticality"}))
	assert.ErrorContains(t, ValidateCustomKeys(nil), "at least one label key")
	assert.ErrorContains(t, ValidateCustomKeys([]string{"safedep.io/project-id"}), `"safedep.io/" labels`)
}

func TestParseSelector(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr string
	}{
		{in: "custom/criticality=high", want: "custom/criticality=high"},
		{in: "custom/a==1, scm/forge=github", want: "custom/a=1, scm/forge=github"},
		{in: "custom/a!=1", want: "custom/a notin (1)"},
		{in: "custom/tier in (gold, silver)", want: "custom/tier in (gold,silver)"},
		{in: "custom/tier notin (bronze)", want: "custom/tier notin (bronze)"},
		{in: "custom/owner", want: "custom/owner"},
		{in: "!custom/legacy", want: "!custom/legacy"},
		{in: "", wantErr: "the selector is empty"},
		{in: "custom/tier in ()", wantErr: "needs at least one value"},
		{in: "custom/tier in (a", wantErr: "has a '(' with no ')'"},
		{in: "team=release", wantErr: "is not valid"},
		{in: "custom/a=1,custom/a=2", wantErr: "conflicts with"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseSelector(tt.in)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, FormatSelector(got))
		})
	}
}

func TestFormatSelector_EmptyMatchesEverySubject(t *testing.T) {
	assert.Equal(t, "(every subject)", FormatSelector(nil))
	assert.Equal(t, "(every subject)", FormatSelector(&messagescontroltowerv1.LabelSelector{}))
}
