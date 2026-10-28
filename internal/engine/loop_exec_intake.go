package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bobbyrc/onlet/internal/config"
	"github.com/bobbyrc/onlet/internal/domain"
	"github.com/bobbyrc/onlet/internal/store"
	"github.com/bobbyrc/onlet/internal/sysproc"
)

// lpIntakeStdoutMax caps the stdout kept from one run of an intake command. Bytes past the cap
// are read and dropped, so the command never blocks on a full pipe.
const lpIntakeStdoutMax = 1 << 20

// lpIntakeStderrTail is how much of an intake command's stderr is kept for its error.
const lpIntakeStderrTail = 4 << 10

// lpIntakeDefaultTimeout bounds one run of an intake command whose Timeout is unset.
const lpIntakeDefaultTimeout = time.Minute

// lpIntakeKinds are the task kinds an intake line may name. A plugin never creates a review (a
// review belongs to a pull request) or an incident (incidents come from observers). A line that
// names any other kind, or none, gets its source's default kind.
var lpIntakeKinds = map[domain.TaskKind]bool{
	domain.KindFeature:  true,
	domain.KindBugfix:   true,
	domain.KindChore:    true,
	domain.KindImprove:  true,
	domain.KindQuestion: true,
}

// lpIntakeLine is one JSON object an intake command prints. ID and Title are required.
type lpIntakeLine struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Kind     string `json:"kind"`
	Priority int    `json:"priority"`
	Role     string `json:"role"`
}

// lpIntakeStdout keeps the first lpIntakeStdoutMax bytes written to it and drops the rest.
type lpIntakeStdout struct {
	buf       bytes.Buffer
	truncated bool
}

func (w *lpIntakeStdout) Write(p []byte) (int, error) {
	room := lpIntakeStdoutMax - w.buf.Len()
	if room >= len(p) {
		w.buf.Write(p)
	} else {
		if room > 0 {
			w.buf.Write(p[:room])
		}
		w.truncated = true
	}
	return len(p), nil
}

// execIntakeLoop runs each of the project's intake sources and creates a task for every new line
// it prints. A source whose command fails is reported, and the other sources still run.
func (e *Engine) execIntakeLoop(ctx context.Context, p *config.Project) error {
	var errs []error
	for _, src := range p.Intake {
		if err := e.lpRunIntakeSource(ctx, p, src); err != nil {
			errs = append(errs, fmt.Errorf("intake %s: %w", src.Name, err))
		}
	}
	return errors.Join(errs...)
}

// lpRunIntakeSource runs one source's command and creates a task for each new, valid line. A
// non-zero exit, a timeout or a failure to start is an error, and none of that run's lines are
// used.
func (e *Engine) lpRunIntakeSource(ctx context.Context, p *config.Project, src config.IntakeSource) error {
	timeout := src.Timeout.Duration
	if timeout <= 0 {
		timeout = lpIntakeDefaultTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "sh", "-c", src.Command)
	cmd.Dir = e.lpIntakeDir(p)
	cmd.Env = lpIntakeEnv(p)
	stdout := &lpIntakeStdout{}
	stderr := &tailWriter{max: lpIntakeStderrTail}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	sysproc.Configure(cmd)

	err := cmd.Start()
	if err == nil {
		err = sysproc.Wait(cmd)
	}
	if err != nil {
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("timed out after %s", timeout)
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	// Output cut at the cap ends mid-line. That partial line is dropped, so it is neither parsed
	// nor counted as invalid.
	output := stdout.buf.Bytes()
	if stdout.truncated {
		output = output[:bytes.LastIndexByte(output, '\n')+1]
	}

	var errs []error
	invalid := 0
	var firstInvalid error
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64<<10), lpIntakeStdoutMax)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var line lpIntakeLine
		if err := json.Unmarshal(raw, &line); err != nil {
			invalid++
			firstInvalid = lpFirstErr(firstInvalid, fmt.Errorf("line %d: %w", lineNo, err))
			continue
		}
		if line.ID == "" || strings.TrimSpace(line.Title) == "" {
			invalid++
			firstInvalid = lpFirstErr(firstInvalid, fmt.Errorf("line %d: id and title are required", lineNo))
			continue
		}
		if err := e.lpCreateIntakeTask(ctx, p, src, line); err != nil {
			errs = append(errs, fmt.Errorf("item %s: %w", line.ID, err))
		}
	}
	if err := scanner.Err(); err != nil {
		errs = append(errs, fmt.Errorf("read output: %w", err))
	}
	if stdout.truncated {
		errs = append(errs, fmt.Errorf("output exceeded %d bytes; lines after that were dropped", lpIntakeStdoutMax))
	}
	if invalid > 0 {
		e.d.Logger.Warn("exec intake: skipped invalid lines", "project", p.Name, "source", src.Name,
			"count", invalid, "first", firstInvalid.Error())
	}
	return errors.Join(errs...)
}

// lpCreateIntakeTask creates the task for one line unless a task with the same source ref already
// exists. The ref is "intake:<source>:<id>", so a line that is printed again is not queued twice.
func (e *Engine) lpCreateIntakeTask(ctx context.Context, p *config.Project, src config.IntakeSource, line lpIntakeLine) error {
	ref := "intake:" + src.Name + ":" + line.ID
	_, err := e.d.Store.FindTaskBySourceRef(ctx, p.Name, ref)
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, store.ErrNotFound):
		return err
	}

	kind := domain.TaskKind(src.Kind)
	if k := domain.TaskKind(line.Kind); lpIntakeKinds[k] {
		kind = k
	}

	footer := fmt.Sprintf("From intake source `%s` (id `%s`).", src.Name, line.ID)
	body := footer
	if b := strings.TrimRight(line.Body, "\n"); strings.TrimSpace(b) != "" {
		body = b + "\n\n---\n" + footer
	}

	_, err = e.createTask(ctx, domain.Task{
		Project:   p.Name,
		Kind:      kind,
		Title:     line.Title,
		Body:      body,
		Role:      line.Role,
		Priority:  line.Priority,
		Source:    "intake",
		SourceRef: ref,
	})
	return err
}

// lpIntakeDir is the directory an intake command runs in: the project's clone when it exists,
// otherwise the data directory, so a command can run before the first clone.
func (e *Engine) lpIntakeDir(p *config.Project) string {
	if fi, err := os.Stat(p.Path); err == nil && fi.IsDir() {
		return p.Path
	}
	return e.Config().Server.DataDir
}

// lpIntakeEnv is onlet's full environment plus the project's env and ONLET_PROJECT. Unlike an
// agent, an intake command is the operator's own script, so it gets the whole environment.
func lpIntakeEnv(p *config.Project) []string {
	env := os.Environ()
	for k, v := range p.Env {
		env = append(env, k+"="+v)
	}
	return append(env, "ONLET_PROJECT="+p.Name)
}

// lpFirstErr returns the first non-nil error of the two.
func lpFirstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}
