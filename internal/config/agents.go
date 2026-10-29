package config

import (
	"sort"

	"github.com/bobbyrc/onett/internal/domain"
)

// AgentOrigin says where an agent's settings come from. The UI shows it so an owner can tell a
// built-in agent from one they changed or added.
type AgentOrigin string

const (
	// OriginBuiltin is an agent the file does not change: the factory's own default.
	OriginBuiltin AgentOrigin = "builtin"
	// OriginEdited is a built-in agent that the file replaces with its own settings.
	OriginEdited AgentOrigin = "edited"
	// OriginCustom is an agent the file adds; no built-in has its name.
	OriginCustom AgentOrigin = "custom"
)

// AgentUse says what an agent is used for: a task kind it is routed, the project that routes
// it (empty when the global routing does), or the fixed roles of chat and delegation.
type AgentUse struct {
	Kind        string `json:"kind,omitempty"`    // task kind; "" for chat and delegation
	Project     string `json:"project,omitempty"` // set when the route is a project's own
	Description string `json:"description"`
}

// kindUses says in plain words what each task kind's agent does, for AgentUses.
var kindUses = map[string]string{
	string(domain.KindFeature):  "builds features",
	string(domain.KindBugfix):   "fixes bugs",
	string(domain.KindIssue):    "works on GitHub issues",
	string(domain.KindChore):    "does chores",
	string(domain.KindIncident): "handles failed deploys and production signals",
	string(domain.KindReview):   "reviews every pull request before merge",
	string(domain.KindImprove):  "fixes friction and review rejections (retrospectives)",
	string(domain.KindQuestion): "answers questions and triages issues",
}

// KindUse returns the phrase that describes the agent for a task kind, or "" for an unknown kind.
func KindUse(kind string) string { return kindUses[kind] }

// routeUse is one route to an agent: a task kind, and the project that owns the route ("" for
// the global routing).
type routeUse struct{ kind, project string }

// AgentUses lists, for each enabled agent, what it is used for. An empty project gives the
// global view: the global routing and every project's own routing, with each project's routes
// marked by its name. A project name gives that project's effective view: the routing it sees,
// with a mark only where the project's own routing set the route. Disabled agents have no
// uses. The global view lists only the agents in the global map.
func (c *Config) AgentUses(project string) map[string][]AgentUse {
	agents := c.AgentsFor(project)
	routes := map[string][]routeUse{}
	if project == "" {
		for kind, agent := range c.Routing {
			routes[agent] = append(routes[agent], routeUse{kind: kind})
		}
		for _, name := range c.ProjectNames() {
			for kind, agent := range c.Projects[name].Routing {
				routes[agent] = append(routes[agent], routeUse{kind: kind, project: name})
			}
		}
	} else {
		p := c.Projects[project]
		kinds := map[string]bool{}
		for kind := range c.Routing {
			kinds[kind] = true
		}
		if p != nil {
			for kind := range p.Routing {
				kinds[kind] = true
			}
		}
		for kind := range kinds {
			agent, ok := c.routeFor(project, domain.TaskKind(kind))
			if !ok {
				continue
			}
			owner := ""
			if p != nil {
				if _, own := p.Routing[kind]; own {
					owner = project
				}
			}
			routes[agent] = append(routes[agent], routeUse{kind: kind, project: owner})
		}
	}

	out := map[string][]AgentUse{}
	for name, a := range agents {
		if a.Disabled {
			continue
		}
		var uses []AgentUse
		if name == "orchestrator" {
			uses = append(uses, AgentUse{Description: "the agent you chat with"})
		}
		rs := routes[name]
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].kind != rs[j].kind {
				return rs[i].kind < rs[j].kind
			}
			return rs[i].project < rs[j].project
		})
		for _, r := range rs {
			uses = append(uses, AgentUse{Kind: r.kind, Project: r.project, Description: KindUse(r.kind)})
		}
		if name != "orchestrator" {
			uses = append(uses, AgentUse{Description: "the orchestrator may also hand it work by name"})
		}
		out[name] = uses
	}
	return out
}
