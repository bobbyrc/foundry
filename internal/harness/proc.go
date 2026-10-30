package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/bobbyrc/onett/internal/domain"
	"github.com/bobbyrc/onett/internal/sysproc"
)

const (
	// procMaxLine caps one stdout line. Longer lines are cut at this size.
	procMaxLine = 10 << 20
	// procEventBuffer is the capacity of Events().
	procEventBuffer = 1024
	// procStderrTail is how many stderr lines StderrTail keeps.
	procStderrTail = 20
	// procKillGrace is how long Cancel waits after SIGTERM before SIGKILL.
	procKillGrace = 10 * time.Second
	// procExitGrace is how long a process group left behind by an exited leader gets after
	// SIGTERM, and then again after SIGKILL, before its pipes are cut.
	procExitGrace = 2 * time.Second
)

// ProcSpec describes one child process whose stdout is parsed into Events.
type ProcSpec struct {
	// Command is the binary and its full argument list.
	Command []string
	Dir     string
	// Env is added to the agent environment (sysproc.AgentEnv); entries win over inherited values.
	Env map[string]string
	// InheritEnv names onett environment variables to pass through, on top of the base
	// allowlist. Everything else in onett's environment is withheld.
	InheritEnv []string
	// Stdin is written to the process and then closed. Empty means no stdin.
	Stdin string
	// KeepStdinOpen leaves stdin open after Stdin is written, so WriteStdin can add more until
	// CloseStdin (or process exit or Cancel) closes it.
	KeepStdinOpen bool
	// ParseLine turns one stdout line into zero or more events. It is called from a single
	// goroutine, in line order. Events with a zero Time get the current time.
	ParseLine func(line string) []Event
	// OnExit builds the final Result from the exit code. It runs at most once, from Wait, after
	// all output has been parsed. The returned ExitCode is overwritten with exitCode. If nil,
	// the Result is {ExitCode, IsError: exitCode != 0}.
	OnExit func(exitCode int, waitErr error) (Result, error)
}

// Proc is a running child process. It implements Session.
type Proc struct {
	cmd    *exec.Cmd
	events chan Event
	onExit func(int, error) (Result, error)

	stdout *os.File // read end of the stdout pipe
	stderr *os.File // read end of the stderr pipe

	readers  sync.WaitGroup // stdout and stderr readers
	exited   chan struct{}  // closed after cmd.Wait returns
	done     chan struct{}  // closed when exited and both readers finished
	exitCode int
	waitErr  error

	finish    sync.Once
	result    Result
	resultErr error

	cancelOnce sync.Once

	stdinMu     sync.Mutex     // serialises writers only; never held by CloseStdin
	stdin       io.WriteCloser // nil unless KeepStdinOpen
	stdinClosed atomic.Bool
	stdinOnce   sync.Once

	tailMu sync.Mutex
	tail   []string
}

var _ Session = (*Proc)(nil)

// StartProcess starts p.Command in its own process group and streams its output. If ctx is
// cancelled before the process exits, the process is cancelled.
func StartProcess(ctx context.Context, p ProcSpec) (*Proc, error) {
	if len(p.Command) == 0 {
		return nil, errors.New("start process: empty command")
	}
	cmd := exec.Command(p.Command[0], p.Command[1:]...)
	cmd.Dir = p.Dir
	cmd.Env = sysproc.AgentEnv(p.InheritEnv, p.Env)
	// Own process group, so Cancel and reapGroup can signal everything the agent started.
	// sysproc.Configure is not used: it needs exec.CommandContext, and this uses Cancel itself.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdinPipe io.WriteCloser
	if p.KeepStdinOpen {
		w, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("start %s: stdin pipe: %w", p.Command[0], err)
		}
		stdinPipe = w
	} else if p.Stdin != "" {
		cmd.Stdin = strings.NewReader(p.Stdin)
	}

	// Plain OS pipes, not cmd.StdoutPipe: exec then does not copy output itself, so Wait
	// cannot race with our readers.
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("start %s: stdout pipe: %w", p.Command[0], err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		closeAll(outR, outW)
		return nil, fmt.Errorf("start %s: stderr pipe: %w", p.Command[0], err)
	}
	cmd.Stdout = outW
	cmd.Stderr = errW

	if err := cmd.Start(); err != nil {
		closeAll(outR, outW, errR, errW)
		return nil, fmt.Errorf("start %s: %w", p.Command[0], err)
	}
	// The child holds its own copies of the write ends now.
	closeAll(outW, errW)

	proc := &Proc{
		cmd:    cmd,
		events: make(chan Event, procEventBuffer),
		onExit: p.OnExit,
		stdout: outR,
		stderr: errR,
		exited: make(chan struct{}),
		done:   make(chan struct{}),
		stdin:  stdinPipe,
	}
	if stdinPipe == nil {
		proc.stdinClosed.Store(true)
	}

	proc.readers.Add(2)
	go proc.readStdout(outR, p.ParseLine)
	go proc.readStderr(errR)
	go func() {
		waitErr := cmd.Wait()
		if cmd.ProcessState != nil {
			proc.exitCode = cmd.ProcessState.ExitCode()
		} else {
			proc.exitCode = -1
		}
		proc.waitErr = waitErr
		close(proc.exited)
		proc.CloseStdin()
		proc.reapGroup()
	}()
	go func() {
		proc.readers.Wait()
		<-proc.exited
		close(proc.events)
		close(proc.done)
	}()

	if ctx != nil {
		go func() {
			select {
			case <-ctx.Done():
				proc.Cancel()
			case <-proc.done:
			}
		}()
	}
	if stdinPipe != nil && p.Stdin != "" {
		// Hold the writer lock while the prompt goes out, so a WriteStdin cannot jump ahead of
		// it, and write from a goroutine so a prompt larger than the pipe buffer does not block
		// Start. A failed write means the process is gone or stdin was closed.
		proc.stdinMu.Lock()
		go func() {
			defer proc.stdinMu.Unlock()
			if _, err := proc.stdin.Write([]byte(p.Stdin)); err != nil {
				proc.CloseStdin()
			}
		}()
	}
	return proc, nil
}

func closeAll(files ...*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

func (p *Proc) readStdout(r *os.File, parse func(string) []Event) {
	defer p.readers.Done()
	defer r.Close()
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := readLine(br, procMaxLine)
		if len(line) > 0 && parse != nil {
			text := strings.TrimRight(line, "\r\n")
			if text != "" {
				for _, ev := range parse(text) {
					p.emit(ev)
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// readLine returns the next line without reading past max bytes. Bytes beyond max are
// discarded up to the newline. At EOF it returns any unterminated tail with io.EOF.
func readLine(br *bufio.Reader, max int) (string, error) {
	var buf []byte
	truncated := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !truncated {
			if len(buf)+len(chunk) > max {
				keep := max - len(buf)
				buf = append(buf, chunk[:keep]...)
				truncated = true
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return string(buf), err
	}
}

func (p *Proc) readStderr(r *os.File) {
	defer p.readers.Done()
	defer r.Close()
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := readLine(br, procMaxLine)
		text := strings.TrimRight(line, "\r\n")
		if text != "" {
			p.tailMu.Lock()
			p.tail = append(p.tail, text)
			if len(p.tail) > procStderrTail {
				p.tail = p.tail[len(p.tail)-procStderrTail:]
			}
			p.tailMu.Unlock()
			p.emit(Event{Kind: domain.LogStderr, Text: text})
		}
		if err != nil {
			return
		}
	}
}

func (p *Proc) emit(ev Event) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	p.events <- ev
}

// Events returns the output stream. It is closed after the process has exited and all of its
// output has been delivered.
func (p *Proc) Events() <-chan Event {
	return p.events
}

// Wait blocks until the process has exited and all of its output has been parsed, then returns
// the result from OnExit. It is safe to call repeatedly and concurrently. Because it waits for
// the parser to finish, call it while draining Events or after draining them.
func (p *Proc) Wait() (Result, error) {
	p.readers.Wait()
	<-p.exited
	p.finish.Do(func() {
		if p.onExit != nil {
			p.result, p.resultErr = p.onExit(p.exitCode, p.waitErr)
		} else {
			p.result = Result{IsError: p.exitCode != 0}
		}
		p.result.ExitCode = p.exitCode
	})
	return p.result, p.resultErr
}

// Cancel asks the process group to stop: SIGTERM now, SIGKILL after a 10 second grace period
// if anything in the group is still running. It returns immediately and is idempotent.
func (p *Proc) Cancel() {
	p.cancelOnce.Do(func() {
		pgid := p.cmd.Process.Pid
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		go func() {
			select {
			case <-p.done:
			case <-time.After(procKillGrace):
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
		}()
		p.CloseStdin()
	})
}

// WriteStdin writes b to the process's stdin. It returns ErrInputClosed once stdin is closed,
// including after the process exits or is cancelled, and when a write fails. Writers are
// serialised so two messages never interleave. Only valid with ProcSpec.KeepStdinOpen.
func (p *Proc) WriteStdin(b []byte) error {
	p.stdinMu.Lock()
	defer p.stdinMu.Unlock()
	if p.stdinClosed.Load() {
		return ErrInputClosed
	}
	if _, err := p.stdin.Write(b); err != nil {
		p.CloseStdin()
		return fmt.Errorf("%w: %v", ErrInputClosed, err)
	}
	return nil
}

// CloseStdin closes the process's stdin. It is safe to call more than once and concurrently
// with WriteStdin; it does not wait for a write in progress, and closing the pipe makes that
// write fail.
func (p *Proc) CloseStdin() {
	p.stdinClosed.Store(true)
	p.stdinOnce.Do(func() {
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
	})
}

// reapGroup runs once the leader has exited. A background child the leader started (`cmd &`)
// is still in its process group, and it holds the stdout pipe open, so the readers would never
// see EOF. The group gets SIGTERM, then SIGKILL procExitGrace later if the readers still have
// not finished. A process that moved itself out of the group (setsid) can still hold the pipe;
// as a last resort the read ends get an expired deadline so the readers stop. That last step
// can drop output still buffered in the pipe, but only after the consumer has stalled for
// several seconds.
func (p *Proc) reapGroup() {
	pgid := p.cmd.Process.Pid
	readersDone := make(chan struct{})
	go func() {
		p.readers.Wait()
		close(readersDone)
	}()

	_ = sysproc.KillGroup(pgid, syscall.SIGTERM)
	if waitChan(readersDone, procExitGrace) {
		return
	}
	_ = sysproc.KillGroup(pgid, syscall.SIGKILL)
	if waitChan(readersDone, procExitGrace) {
		return
	}
	_ = p.stdout.SetReadDeadline(time.Now())
	_ = p.stderr.SetReadDeadline(time.Now())
	<-readersDone
}

// waitChan reports whether ch closed within d.
func waitChan(ch <-chan struct{}, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ch:
		return true
	case <-t.C:
		return false
	}
}

// StderrTail returns the last lines the process wrote to stderr, oldest first. Its value is
// complete once Wait has returned.
func (p *Proc) StderrTail() []string {
	p.tailMu.Lock()
	defer p.tailMu.Unlock()
	return append([]string(nil), p.tail...)
}

// Truncate shortens s to at most max bytes, cutting on a UTF-8 boundary and appending "..."
// when it cuts.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// CompactJSON returns raw as single-line JSON truncated to max bytes. Invalid JSON is returned
// as-is (then truncated).
func CompactJSON(raw []byte, max int) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return Truncate(strings.TrimSpace(string(raw)), max)
	}
	return Truncate(b.String(), max)
}
