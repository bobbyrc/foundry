package harness

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bobbyrc/onett/internal/domain"
)

func TestProcKeepStdinOpen(t *testing.T) {
	proc, err := StartProcess(context.Background(), ProcSpec{
		Command:       []string{"/bin/sh", "-c", `while read l; do echo "$l"; done`},
		Stdin:         "a\n",
		KeepStdinOpen: true,
		ParseLine: func(line string) []Event {
			return []Event{{Kind: domain.LogText, Text: line}}
		},
	})
	require.NoError(t, err)

	require.NoError(t, proc.WriteStdin([]byte("b\n")))
	var got []string
	timeout := time.After(10 * time.Second)
	for len(got) < 2 {
		select {
		case ev := <-proc.Events():
			if ev.Kind == domain.LogText {
				got = append(got, ev.Text)
			}
		case <-timeout:
			t.Fatalf("timed out; got %v", got)
		}
	}
	assert.Equal(t, []string{"a", "b"}, got)

	proc.CloseStdin()
	proc.CloseStdin() // idempotent
	res, err := proc.Wait()
	require.NoError(t, err)
	assert.Equal(t, 0, res.ExitCode)
	assert.ErrorIs(t, proc.WriteStdin([]byte("c\n")), ErrInputClosed)
}

func TestProcStdinClosedOnCancel(t *testing.T) {
	proc, err := StartProcess(context.Background(), ProcSpec{
		Command:       []string{"/bin/sh", "-c", `cat >/dev/null`},
		KeepStdinOpen: true,
	})
	require.NoError(t, err)
	proc.Cancel()
	_, _ = proc.Wait()
	assert.ErrorIs(t, proc.WriteStdin([]byte("x\n")), ErrInputClosed)
}

func TestProcWriteStdinWithoutKeepOpen(t *testing.T) {
	proc, err := StartProcess(context.Background(), ProcSpec{Command: []string{"/bin/sh", "-c", "true"}})
	require.NoError(t, err)
	assert.ErrorIs(t, proc.WriteStdin([]byte("x")), ErrInputClosed)
	_, _ = proc.Wait()
}
