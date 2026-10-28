package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bobbyrc/foundry/internal/config"
	"go.yaml.in/yaml/v3"
)

// addProject adds a project to an existing config: `foundry project add NAME --repo OWNER/NAME`.
// It runs without the daemon, because it only edits the config file; the daemon reads that file
// when it starts or reloads.
func addProject(e *env, args []string) error {
	fs := newFlagSet("project add", e)
	cfgFlag := fs.String("config", "", "config file to edit (default $FOUNDRY_CONFIG or ~/.config/foundry/foundry.yaml)")
	repo := fs.String("repo", "", "the project's GitHub repository, owner/name (required)")
	dir := fs.String("path", "", "the project's local clone (default <data_dir>/repos/<name>)")
	branch := fs.String("branch", "", "the branch agents base their work on (default main)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "project add NAME --repo OWNER/NAME [--path DIR] [--branch BRANCH] [--config PATH]"); err != nil {
		return err
	}
	name := positional[0]
	if !projectName.MatchString(name) {
		return usagef("project name %q may use letters, digits, '.', '_' and '-'", name)
	}
	if *repo == "" {
		return usagef("--repo is required")
	}
	path, err := configPath(*cfgFlag)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no config at %s; run \"foundry init\" first", path)
	} else if err != nil {
		return err
	}
	out, err := insertProject(data, path, name, *repo, *dir, *branch)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, out); err != nil {
		return err
	}
	fmt.Fprintf(e.out, "added project %s (%s) to %s\n", name, *repo, path)
	fmt.Fprintln(e.out, `run "foundry config --reload" (or restart foundry) to start using it`)
	return nil
}

// insertProject returns the config text with the project added. The YAML node tree is used only
// to find where the new lines go. The text is never re-encoded: re-encoding would reflow the
// author's comments and blank lines, so the result is the original bytes with one block of lines
// inserted. The result must still load as a valid config; otherwise an error is returned and the
// caller writes nothing.
func insertProject(data []byte, path, name, repo, dir, branch string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// A file with no mapping at all (empty, or only comments) has no root and gets the new keys
	// appended.
	var root *yaml.Node
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	}
	var projKey, projects, selfProject *yaml.Node
	projIdx := -1
	if root != nil {
		if root.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s: top level is not a mapping", path)
		}
		if root.Style&yaml.FlowStyle != 0 {
			return nil, errFlowStyle(path)
		}
		for i := 0; i+1 < len(root.Content); i += 2 {
			switch root.Content[i].Value {
			case "projects":
				projIdx, projKey, projects = i, root.Content[i], root.Content[i+1]
			case "self_project":
				selfProject = root.Content[i+1]
			}
		}
	}

	lines := splitLines(string(data))
	var prefix []string
	var at int
	var indent string
	switch {
	case projects == nil:
		// No projects key yet: it is appended at the end of the file, after a blank line.
		at = len(lines)
		if len(lines) > 0 {
			prefix = append(prefix, "\n")
		}
		prefix = append(prefix, "projects:\n")
		indent = "  "
	case projects.ShortTag() == "!!null" && projects.Value == "":
		// An empty "projects:" line: the entry goes on the line right after it.
		at = projKey.Line
		indent = "  "
	case projects.ShortTag() == "!!null":
		return nil, errFlowStyle(path)
	case projects.Kind != yaml.MappingNode:
		return nil, fmt.Errorf("%s: projects is not a mapping", path)
	case projects.Style&yaml.FlowStyle != 0 || len(projects.Content) == 0:
		return nil, errFlowStyle(path)
	default:
		if mappingValue(projects, name) != nil {
			return nil, fmt.Errorf("project %q already exists in %s", name, path)
		}
		// The entry is indented to match the projects already there.
		indent = strings.Repeat(" ", projects.Content[0].Column-1)
		at = projectsEnd(lines, root, projIdx, projKey.Line-1)
	}

	entry, err := entryLines(indent, name, repo, dir, branch)
	if err != nil {
		return nil, err
	}
	lines = insertLines(lines, at, append(prefix, entry...))

	// Foundry's own repository becomes the self_project, as init does, but an existing choice
	// is never overwritten.
	if repo == selfRepo && selfProject == nil {
		lines = insertLines(lines, len(lines), []string{"self_project: " + name + "\n"})
	}

	out := []byte(strings.Join(lines, ""))
	if _, err := config.Parse(out, path); err != nil {
		return nil, fmt.Errorf("adding project %q would make %s invalid, so it was not changed: %w", name, path, err)
	}
	return out, nil
}

// errFlowStyle is the error for a config whose projects section is written in flow style, such as
// "projects: {}". Inserting block-style lines into that would produce a different structure, so the
// user adds the project by hand.
func errFlowStyle(path string) error {
	return fmt.Errorf("cannot add to the projects block in %s automatically (flow style); add it by hand", path)
}

// projectsEnd returns the index of the line that a new project entry goes before: the line after
// the last line of the projects section. Blank lines and full-line comments directly above the next
// top-level key belong to that key, so the entry goes above them. When projects is the last key,
// the entry goes after its last line, with any trailing blank lines left at the end of the file.
// projLine is the index of the "projects:" line.
func projectsEnd(lines []string, root *yaml.Node, projIdx, projLine int) int {
	at := len(lines)
	if projIdx+2 < len(root.Content) {
		at = root.Content[projIdx+2].Line - 1
	}
	for at-1 > projLine && isBlankOrComment(lines[at-1]) {
		at--
	}
	return at
}

// isBlankOrComment reports whether a line holds nothing but whitespace or a full-line comment.
func isBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

// splitLines splits s into lines that keep their newline characters, so joining them gives back s
// exactly.
func splitLines(s string) []string {
	lines := strings.SplitAfter(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// insertLines returns lines with block inserted before index at. A last line that lacks a newline
// gets one first, so the inserted text starts on its own line.
func insertLines(lines []string, at int, block []string) []string {
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		lines[at-1] += "\n"
	}
	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:at]...)
	out = append(out, block...)
	return append(out, lines[at:]...)
}

// entryLines returns the text of one project entry: its name at indent, then its fields two spaces
// further in. Only the fields that were given are written, so the project keeps the defaults for
// the rest.
func entryLines(indent, name, repo, dir, branch string) ([]string, error) {
	fields := [][2]string{{"repo", repo}}
	if dir != "" {
		fields = append(fields, [2]string{"path", dir})
	}
	if branch != "" {
		fields = append(fields, [2]string{"default_branch", branch})
	}
	lines := []string{indent + name + ":\n"}
	for _, f := range fields {
		v, err := yamlScalar(f[1])
		if err != nil {
			return nil, err
		}
		lines = append(lines, indent+"  "+f[0]+": "+v+"\n")
	}
	return lines, nil
}

// yamlScalar returns s as a YAML value. It stays plain when it reads back unchanged, and is quoted
// when it would not (for example when it contains ": " or starts with a special character).
func yamlScalar(s string) (string, error) {
	if strings.ContainsAny(s, "\r\n") {
		return "", usagef("%q may not contain a line break", s)
	}
	out, err := yaml.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("quote %q: %w", s, err)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// mappingValue returns the value stored under key in the mapping node m, or nil when m has no
// such key.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// writeFileAtomic replaces the file at path with data. The new bytes go to a temporary file in the
// same directory, which is then renamed over the old file, so a crash or a full disk leaves either
// the old config or the new one, never a half-written one. The temporary file takes the old file's
// permissions, because a config can hold secrets.
func writeFileAtomic(path string, data []byte) error {
	// A symlinked config (for example one managed by a dotfile tool) is written through to its
	// target; renaming over the link itself would replace the link with a plain file.
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Chmod(info.Mode().Perm())
	}
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("write %s: %w", path, werr)
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
