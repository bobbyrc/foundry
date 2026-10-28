package main

import (
	"context"
	"fmt"
	"strings"
)

// cmdDeploys lists releases, or with `show ID` prints one release and its evidence log.
func cmdDeploys(e *env, args []string) error {
	if len(args) > 0 && args[0] == "show" {
		return deploysShow(e, args[1:])
	}
	fs := newFlagSet("deploys", e)
	project := fs.String("project", "", "only this project")
	limit := fs.Int("limit", 0, "maximum rows (default 200)")
	jsonOut := fs.Bool("json", false, "print the raw JSON")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 0, "deploys [--project P] [--limit N] [--json] | deploys show ID"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	deps, err := c.ListDeployments(context.Background(), *project, *limit)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(e.out, deps)
	}
	rows := make([][]string, 0, len(deps))
	for _, d := range deps {
		ref := []rune(oneLine(d.Ref))
		if len(ref) > 7 {
			ref = ref[:7]
		}
		notes := []rune(oneLine(d.Notes))
		if len(notes) > 80 {
			notes = notes[:80]
		}
		rows = append(rows, []string{d.ID, d.Project, string(ref), string(d.State), stamp(d.StartedAt), orDash(string(notes))})
	}
	table(e.out, []string{"ID", "PROJECT", "REF", "STATE", "STARTED", "NOTES"}, rows)
	return nil
}

func deploysShow(e *env, args []string) error {
	fs := newFlagSet("deploys show", e)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := requireArgs(positional, 1, "deploys show ID"); err != nil {
		return err
	}
	c, err := apiClient()
	if err != nil {
		return err
	}
	// There is no single-deployment endpoint: list and pick.
	deps, err := c.ListDeployments(context.Background(), "", 0)
	if err != nil {
		return err
	}
	for _, d := range deps {
		if d.ID != positional[0] {
			continue
		}
		field := func(name, value string) { fmt.Fprintf(e.out, "%s: %s\n", name, orDash(oneLine(value))) }
		field("id", d.ID)
		field("project", d.Project)
		field("task_id", d.TaskID)
		field("ref", d.Ref)
		field("provider_id", d.ProviderID)
		field("url", d.URL)
		field("state", string(d.State))
		field("notes", d.Notes)
		field("verifying_since", stamp(derefTime(d.VerifyingSince)))
		field("started_at", stamp(d.StartedAt))
		field("updated_at", stamp(d.UpdatedAt))
		log := strings.TrimRight(cleanText(d.Log), "\n")
		if log == "" {
			log = "(no log)"
		}
		fmt.Fprintf(e.out, "\n%s\n", log)
		return nil
	}
	return fmt.Errorf("no deployment %q", positional[0])
}
