package summon_test

import (
	"context"
	"testing"

	"github.com/daaku/ensure"
	"github.com/daaku/summon"
)

func TestCheckInternet(t *testing.T) {
	t.Parallel()
	ensure.Nil(t, summon.Run(context.Background(), summon.CheckInternet))
}
