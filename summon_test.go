package summon_test

import (
	"context"
	"testing"

	"github.com/daaku/ensure"
	"github.com/daaku/summon"
	"github.com/gkampitakis/go-snaps/snaps"
)

func TestCheckInternet(t *testing.T) {
	t.Parallel()
	ensure.Nil(t, summon.Run(context.Background(), summon.CheckInternet))
}

func TestShellf(t *testing.T) {
	cases := []struct {
		name   string
		format string
		args   []any
	}{
		{name: "lone command", format: "true"},
		{name: "fixed args", format: "echo hello world"},
		{name: "arg with space", format: "echo %q", args: []any{"hello world"}},
		{name: "path quoting", format: "echo %q/foo/bar", args: []any{"/home/naitik shah"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, args, err := summon.Shellf(c.format, c.args...)
			snaps.MatchSnapshot(t, name, args, err)
		})
	}
}
