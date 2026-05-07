package jfrog

import (
	"testing"
	"time"

	"github.com/safedep/cli/internal/app"
	"github.com/safedep/cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister_buildsJFrogTree(t *testing.T) {
	a := app.New(&config.Config{})
	root := &cobra.Command{Use: "safedep"}
	parent := &cobra.Command{Use: "integration"}
	root.AddCommand(parent)

	Register(parent, a)

	jfrogCmd, _, err := root.Find([]string{"integration", "jfrog"})
	require.NoError(t, err)
	require.NotNil(t, jfrogCmd)
	assert.Equal(t, "jfrog", jfrogCmd.Name())
	assert.NotEmpty(t, jfrogCmd.Short)
	assert.NotEmpty(t, jfrogCmd.Long)

	t.Run("run", func(t *testing.T) {
		leaf, _, err := root.Find([]string{"integration", "jfrog", "run"})
		require.NoError(t, err)
		require.NotNil(t, leaf)
		assert.Equal(t, "run", leaf.Name())
		assert.NotEmpty(t, leaf.Short)
		assert.NotEmpty(t, leaf.Long)
		assert.NotNil(t, leaf.Flags().Lookup("instance-url"))
		assert.NotNil(t, leaf.Flags().Lookup("instance-access-token"))
		assert.NotNil(t, leaf.Flags().Lookup("poll-interval"))
		// --cursor-file removed: cursor is now stored in the profile-scoped KV store.
		assert.Nil(t, leaf.Flags().Lookup("cursor-file"))
	})
}

func TestResolveConfig(t *testing.T) {
	tests := []struct {
		name      string
		in        runInput
		envURL    string
		envToken  string
		wantURL   string
		wantToken string
		wantPoll  time.Duration
		wantErr   bool
	}{
		{
			name: "flags supply both url and token",
			in: runInput{
				InstanceURL:         "https://example.jfrog.io",
				InstanceAccessToken: "tok",
				PollInterval:        30 * time.Second,
			},
			wantURL:   "https://example.jfrog.io",
			wantToken: "tok",
			wantPoll:  30 * time.Second,
		},
		{
			name: "missing url errors out",
			in: runInput{
				InstanceAccessToken: "tok",
				PollInterval:        time.Second,
			},
			wantErr: true,
		},
		{
			name: "missing token errors out",
			in: runInput{
				InstanceURL:  "https://example.jfrog.io",
				PollInterval: time.Second,
			},
			wantErr: true,
		},
		{
			name: "http url upgraded to https",
			in: runInput{
				InstanceURL:         "http://example.jfrog.io",
				InstanceAccessToken: "tok",
				PollInterval:        time.Second,
			},
			wantURL:   "https://example.jfrog.io",
			wantToken: "tok",
			wantPoll:  time.Second,
		},
		{
			name: "url without scheme gets https prefix",
			in: runInput{
				InstanceURL:         "example.jfrog.io",
				InstanceAccessToken: "tok",
				PollInterval:        time.Second,
			},
			wantURL:   "https://example.jfrog.io",
			wantToken: "tok",
			wantPoll:  time.Second,
		},
		{
			name: "https url left untouched",
			in: runInput{
				InstanceURL:         "https://example.jfrog.io",
				InstanceAccessToken: "tok",
				PollInterval:        time.Second,
			},
			wantURL:   "https://example.jfrog.io",
			wantToken: "tok",
			wantPoll:  time.Second,
		},
		{
			name: "env vars fill in when flags empty",
			in: runInput{
				PollInterval: time.Second,
			},
			envURL:    "https://from-env.jfrog.io",
			envToken:  "env-tok",
			wantURL:   "https://from-env.jfrog.io",
			wantToken: "env-tok",
			wantPoll:  time.Second,
		},
		{
			name: "flag wins over env",
			in: runInput{
				InstanceURL:         "https://from-flag.jfrog.io",
				InstanceAccessToken: "flag-tok",
				PollInterval:        time.Second,
			},
			envURL:    "https://from-env.jfrog.io",
			envToken:  "env-tok",
			wantURL:   "https://from-flag.jfrog.io",
			wantToken: "flag-tok",
			wantPoll:  time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Always set both env vars (empty string == unset for our reads)
			// so a leaked variable from the parent shell or a prior test
			// cannot pollute this case.
			t.Setenv(envJFrogURL, tt.envURL)
			t.Setenv(envJFrogToken, tt.envToken)

			cfg, err := resolveConfig(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantURL, cfg.JFrog.URL)
			assert.Equal(t, tt.wantToken, cfg.JFrog.AccessToken)
			assert.Equal(t, tt.wantPoll, cfg.Source.PollInterval)
		})
	}
}
