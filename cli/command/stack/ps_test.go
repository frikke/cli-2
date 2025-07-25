package stack

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/internal/test"
	"github.com/docker/cli/internal/test/builders"
	"github.com/moby/moby/api/types/swarm"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/golden"
)

func TestStackPsErrors(t *testing.T) {
	testCases := []struct {
		args          []string
		taskListFunc  func(options swarm.TaskListOptions) ([]swarm.Task, error)
		expectedError string
	}{
		{
			args:          []string{},
			expectedError: "requires 1 argument",
		},
		{
			args:          []string{"foo", "bar"},
			expectedError: "requires 1 argument",
		},
		{
			args: []string{"foo"},
			taskListFunc: func(options swarm.TaskListOptions) ([]swarm.Task, error) {
				return nil, errors.New("error getting tasks")
			},
			expectedError: "error getting tasks",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.expectedError, func(t *testing.T) {
			cmd := newPsCommand(test.NewFakeCli(&fakeClient{
				taskListFunc: tc.taskListFunc,
			}))
			cmd.SetArgs(tc.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			assert.ErrorContains(t, cmd.Execute(), tc.expectedError)
		})
	}
}

func TestStackPs(t *testing.T) {
	testCases := []struct {
		doc                string
		taskListFunc       func(swarm.TaskListOptions) ([]swarm.Task, error)
		nodeInspectWithRaw func(string) (swarm.Node, []byte, error)
		config             configfile.ConfigFile
		args               []string
		flags              map[string]string
		expectedErr        string
		golden             string
	}{
		{
			doc:         "WithEmptyName",
			args:        []string{"'   '"},
			expectedErr: `invalid stack name: "'   '"`,
		},
		{
			doc: "WithEmptyStack",
			taskListFunc: func(options swarm.TaskListOptions) ([]swarm.Task, error) {
				return []swarm.Task{}, nil
			},
			args:        []string{"foo"},
			expectedErr: "nothing found in stack: foo",
		},
		{
			doc: "WithQuietOption",
			taskListFunc: func(options swarm.TaskListOptions) ([]swarm.Task, error) {
				return []swarm.Task{*builders.Task(builders.TaskID("id-foo"))}, nil
			},
			args: []string{"foo"},
			flags: map[string]string{
				"quiet": "true",
			},
			golden: "stack-ps-with-quiet-option.golden",
		},
		{
			doc: "WithNoTruncOption",
			taskListFunc: func(options swarm.TaskListOptions) ([]swarm.Task, error) {
				return []swarm.Task{*builders.Task(builders.TaskID("xn4cypcov06f2w8gsbaf2lst3"))}, nil
			},
			args: []string{"foo"},
			flags: map[string]string{
				"no-trunc": "true",
				"format":   "{{ .ID }}",
			},
			golden: "stack-ps-with-no-trunc-option.golden",
		},
		{
			doc: "WithNoResolveOption",
			taskListFunc: func(options swarm.TaskListOptions) ([]swarm.Task, error) {
				return []swarm.Task{*builders.Task(
					builders.TaskNodeID("id-node-foo"),
				)}, nil
			},
			nodeInspectWithRaw: func(ref string) (swarm.Node, []byte, error) {
				return *builders.Node(builders.NodeName("node-name-bar")), nil, nil
			},
			args: []string{"foo"},
			flags: map[string]string{
				"no-resolve": "true",
				"format":     "{{ .Node }}",
			},
			golden: "stack-ps-with-no-resolve-option.golden",
		},
		{
			doc: "WithFormat",
			taskListFunc: func(options swarm.TaskListOptions) ([]swarm.Task, error) {
				return []swarm.Task{*builders.Task(builders.TaskServiceID("service-id-foo"))}, nil
			},
			args: []string{"foo"},
			flags: map[string]string{
				"format": "{{ .Name }}",
			},
			golden: "stack-ps-with-format.golden",
		},
		{
			doc: "WithConfigFormat",
			taskListFunc: func(options swarm.TaskListOptions) ([]swarm.Task, error) {
				return []swarm.Task{*builders.Task(builders.TaskServiceID("service-id-foo"))}, nil
			},
			config: configfile.ConfigFile{
				TasksFormat: "{{ .Name }}",
			},
			args:   []string{"foo"},
			golden: "stack-ps-with-config-format.golden",
		},
		{
			doc: "WithoutFormat",
			taskListFunc: func(options swarm.TaskListOptions) ([]swarm.Task, error) {
				return []swarm.Task{*builders.Task(
					builders.TaskID("id-foo"),
					builders.TaskServiceID("service-id-foo"),
					builders.TaskNodeID("id-node"),
					builders.WithTaskSpec(builders.TaskImage("myimage:mytag")),
					builders.TaskDesiredState(swarm.TaskStateReady),
					builders.WithStatus(builders.TaskState(swarm.TaskStateFailed), builders.Timestamp(time.Now().Add(-2*time.Hour))),
				)}, nil
			},
			nodeInspectWithRaw: func(ref string) (swarm.Node, []byte, error) {
				return *builders.Node(builders.NodeName("node-name-bar")), nil, nil
			},
			args:   []string{"foo"},
			golden: "stack-ps-without-format.golden",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.doc, func(t *testing.T) {
			cli := test.NewFakeCli(&fakeClient{
				taskListFunc:       tc.taskListFunc,
				nodeInspectWithRaw: tc.nodeInspectWithRaw,
			})
			cli.SetConfigFile(&tc.config)

			cmd := newPsCommand(cli)
			cmd.SetArgs(tc.args)
			for key, value := range tc.flags {
				assert.Check(t, cmd.Flags().Set(key, value))
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			if tc.expectedErr != "" {
				assert.Error(t, cmd.Execute(), tc.expectedErr)
				assert.Check(t, is.Equal("", cli.OutBuffer().String()))
				return
			}
			assert.NilError(t, cmd.Execute())
			golden.Assert(t, cli.OutBuffer().String(), tc.golden)
		})
	}
}
