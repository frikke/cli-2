// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.23

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/context/store"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/cli/internal/test/output"
	"github.com/moby/moby/api/types"
	"github.com/moby/moby/client"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/fs"
)

var pluginFilename = "docker-buildx"

func TestBuildWithBuilder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	testcases := []struct {
		name         string
		context      string
		builder      string
		alias        bool
		expectedEnvs []string
	}{
		{
			name:         "default",
			context:      "default",
			alias:        false,
			expectedEnvs: []string{"BUILDX_BUILDER=default"},
		},
		{
			name:         "custom context",
			context:      "foo",
			alias:        false,
			expectedEnvs: []string{"BUILDX_BUILDER=foo"},
		},
		{
			name:         "custom builder name",
			builder:      "mybuilder",
			alias:        false,
			expectedEnvs: nil,
		},
		{
			name:         "buildx install",
			alias:        true,
			expectedEnvs: nil,
		},
	}

	dir := fs.NewDir(t, t.Name(),
		fs.WithFile(pluginFilename, `#!/bin/sh
echo '{"SchemaVersion":"0.1.0","Vendor":"Docker Inc.","Version":"v0.6.3","ShortDescription":"Build with BuildKit"}'`, fs.WithMode(0o777)),
	)
	defer dir.Remove()

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			ctx2, cancel2 := context.WithCancel(ctx)
			defer cancel2()

			if tc.builder != "" {
				t.Setenv("BUILDX_BUILDER", tc.builder)
			}

			var b bytes.Buffer
			dockerCli, err := command.NewDockerCli(
				command.WithBaseContext(ctx2),
				command.WithAPIClient(&fakeClient{}),
				command.WithInputStream(discard),
				command.WithCombinedStreams(&b),
			)
			assert.NilError(t, err)
			assert.NilError(t, dockerCli.Initialize(flags.NewClientOptions()))

			if tc.context != "" {
				if tc.context != command.DefaultContextName {
					assert.NilError(t, dockerCli.ContextStore().CreateOrUpdate(store.Metadata{
						Name: tc.context,
						Endpoints: map[string]any{
							"docker": map[string]any{
								"host": "unix://" + filepath.Join(t.TempDir(), "docker.sock"),
							},
						},
					}))
				}
				opts := flags.NewClientOptions()
				opts.Context = tc.context
				assert.NilError(t, dockerCli.Initialize(opts))
			}

			dockerCli.ConfigFile().CLIPluginsExtraDirs = []string{dir.Path()}
			if tc.alias {
				dockerCli.ConfigFile().Aliases = map[string]string{"builder": "buildx"}
			}

			tcmd := newDockerCommand(dockerCli)
			tcmd.SetArgs([]string{"build", "."})

			cmd, args, err := tcmd.HandleGlobalFlags()
			assert.NilError(t, err)

			var envs []string
			args, os.Args, envs, err = processBuilder(dockerCli, cmd, args, os.Args)
			assert.NilError(t, err)
			assert.DeepEqual(t, []string{builderDefaultPlugin, "build", "."}, args)
			if tc.expectedEnvs != nil {
				assert.DeepEqual(t, tc.expectedEnvs, envs)
			} else {
				assert.Check(t, len(envs) == 0)
			}
		})
	}
}

type fakeClient struct {
	client.Client
}

func (*fakeClient) Ping(context.Context) (types.Ping, error) {
	return types.Ping{OSType: "linux"}, nil
}

func TestBuildkitDisabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	t.Setenv("DOCKER_BUILDKIT", "0")

	dir := fs.NewDir(t, t.Name(),
		fs.WithFile(pluginFilename, `#!/bin/sh exit 1`, fs.WithMode(0o777)),
	)
	defer dir.Remove()

	b := bytes.NewBuffer(nil)

	dockerCli, err := command.NewDockerCli(
		command.WithBaseContext(ctx),
		command.WithAPIClient(&fakeClient{}),
		command.WithInputStream(discard),
		command.WithCombinedStreams(b),
	)
	assert.NilError(t, err)
	assert.NilError(t, dockerCli.Initialize(flags.NewClientOptions()))
	dockerCli.ConfigFile().CLIPluginsExtraDirs = []string{dir.Path()}

	tcmd := newDockerCommand(dockerCli)
	tcmd.SetArgs([]string{"build", "."})

	cmd, args, err := tcmd.HandleGlobalFlags()
	assert.NilError(t, err)

	var envs []string
	args, os.Args, envs, err = processBuilder(dockerCli, cmd, args, os.Args)
	assert.NilError(t, err)
	assert.DeepEqual(t, []string{"build", "."}, args)
	assert.Check(t, len(envs) == 0)

	output.Assert(t, b.String(), map[int]func(string) error{
		0: output.Suffix("DEPRECATED: The legacy builder is deprecated and will be removed in a future release."),
		1: output.Suffix("BuildKit is currently disabled; enable it by removing the DOCKER_BUILDKIT=0"),
	})
}

func TestBuilderBroken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	dir := fs.NewDir(t, t.Name(),
		fs.WithFile(pluginFilename, `#!/bin/sh exit 1`, fs.WithMode(0o777)),
	)
	defer dir.Remove()

	b := bytes.NewBuffer(nil)

	dockerCli, err := command.NewDockerCli(
		command.WithBaseContext(ctx),
		command.WithAPIClient(&fakeClient{}),
		command.WithInputStream(discard),
		command.WithCombinedStreams(b),
	)
	assert.NilError(t, err)
	assert.NilError(t, dockerCli.Initialize(flags.NewClientOptions()))
	dockerCli.ConfigFile().CLIPluginsExtraDirs = []string{dir.Path()}

	tcmd := newDockerCommand(dockerCli)
	tcmd.SetArgs([]string{"build", "."})

	cmd, args, err := tcmd.HandleGlobalFlags()
	assert.NilError(t, err)

	var envs []string
	args, os.Args, envs, err = processBuilder(dockerCli, cmd, args, os.Args)
	assert.NilError(t, err)
	assert.DeepEqual(t, []string{"build", "."}, args)
	assert.Check(t, len(envs) == 0)

	output.Assert(t, b.String(), map[int]func(string) error{
		0: output.Prefix("failed to fetch metadata:"),
		2: output.Suffix("DEPRECATED: The legacy builder is deprecated and will be removed in a future release."),
	})
}

func TestBuilderBrokenEnforced(t *testing.T) {
	t.Setenv("DOCKER_BUILDKIT", "1")
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	dir := fs.NewDir(t, t.Name(),
		fs.WithFile(pluginFilename, `#!/bin/sh exit 1`, fs.WithMode(0o777)),
	)
	defer dir.Remove()

	b := bytes.NewBuffer(nil)

	dockerCli, err := command.NewDockerCli(
		command.WithBaseContext(ctx),
		command.WithAPIClient(&fakeClient{}),
		command.WithInputStream(discard),
		command.WithCombinedStreams(b),
	)
	assert.NilError(t, err)
	assert.NilError(t, dockerCli.Initialize(flags.NewClientOptions()))
	dockerCli.ConfigFile().CLIPluginsExtraDirs = []string{dir.Path()}

	tcmd := newDockerCommand(dockerCli)
	tcmd.SetArgs([]string{"build", "."})

	cmd, args, err := tcmd.HandleGlobalFlags()
	assert.NilError(t, err)

	var envs []string
	args, os.Args, envs, err = processBuilder(dockerCli, cmd, args, os.Args)
	assert.DeepEqual(t, []string{"build", "."}, args)
	assert.Check(t, len(envs) == 0)

	output.Assert(t, err.Error(), map[int]func(string) error{
		0: output.Prefix("failed to fetch metadata:"),
		2: output.Suffix("ERROR: BuildKit is enabled but the buildx component is missing or broken."),
	})
}

func TestHasBuilderName(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		envs     []string
		expected bool
	}{
		{
			name:     "no args",
			args:     []string{"docker", "build", "."},
			envs:     []string{"FOO=bar"},
			expected: false,
		},
		{
			name:     "env var",
			args:     []string{"docker", "build", "."},
			envs:     []string{"BUILDX_BUILDER=foo"},
			expected: true,
		},
		{
			name:     "empty env var",
			args:     []string{"docker", "build", "."},
			envs:     []string{"BUILDX_BUILDER="},
			expected: false,
		},
		{
			name:     "flag",
			args:     []string{"docker", "build", "--builder", "foo", "."},
			envs:     []string{"FOO=bar"},
			expected: true,
		},
		{
			name:     "both",
			args:     []string{"docker", "build", "--builder", "foo", "."},
			envs:     []string{"BUILDX_BUILDER=foo"},
			expected: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, hasBuilderName(tc.args, tc.envs))
		})
	}
}
