package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func schemaField(t *testing.T, fields []SchemaNode, key string) SchemaNode {
	t.Helper()
	for _, f := range fields {
		if f.Key == key {
			return f
		}
	}
	require.Failf(t, "missing field", "no field %q", key)
	return SchemaNode{}
}

func TestSchemaRootHasTopLevelSections(t *testing.T) {
	root := Schema()
	assert.Equal(t, "section", root.Type)
	assert.Equal(t, "", root.Key)
	for _, key := range []string{"server", "limits", "projects"} {
		schemaField(t, root.Fields, key)
	}
}

func TestSchemaProjectsIsMapOfSectionWithRepo(t *testing.T) {
	projects := schemaField(t, Schema().Fields, "projects")
	assert.Equal(t, "map", projects.Type)
	require.NotNil(t, projects.Item)
	assert.Equal(t, "section", projects.Item.Type)
	assert.Equal(t, "string", schemaField(t, projects.Item.Fields, "repo").Type)
}

func TestSchemaDurationHelpMentionsDefault(t *testing.T) {
	limits := schemaField(t, Schema().Fields, "limits")
	stall := schemaField(t, limits.Fields, "stall_timeout")
	assert.Equal(t, "duration", stall.Type)
	assert.Contains(t, stall.Help, "15m")
}

func TestSchemaHelpIsOneParagraph(t *testing.T) {
	limits := schemaField(t, Schema().Fields, "limits")
	daily := schemaField(t, limits.Fields, "daily_budget_usd")
	assert.Equal(t, "float", daily.Type)
	assert.NotContains(t, daily.Help, "\n")
	assert.Contains(t, daily.Help, "Chat with the orchestrator is exempt")
}

func TestSchemaHasNoInternalFieldsAndValidTypes(t *testing.T) {
	valid := map[string]bool{"section": true, "map": true, "list": true, "string": true, "int": true, "float": true, "bool": true, "duration": true, "any": true}
	var walk func(n SchemaNode, where string)
	walk = func(n SchemaNode, where string) {
		assert.True(t, valid[n.Type], "%s: type %q", where, n.Type)
		assert.NotEqual(t, "-", n.Key, "%s", where)
		for _, f := range n.Fields {
			walk(f, where+"."+f.Key)
		}
		if n.Item != nil {
			walk(*n.Item, where+"[]")
		}
	}
	walk(Schema(), "root")

	// Config.Path is yaml "-" and must not appear at the root. Project.Path (yaml "path") is a real setting.
	for _, f := range Schema().Fields {
		assert.NotEqual(t, "path", f.Key)
	}
	projects := schemaField(t, Schema().Fields, "projects")
	schemaField(t, projects.Item.Fields, "path")
}

func TestSchemaHelpComesFromConfigComments(t *testing.T) {
	server := schemaField(t, Schema().Fields, "server")
	hosts := schemaField(t, server.Fields, "allowed_hosts")
	assert.True(t, strings.Contains(hosts.Help, "DNS-rebinding guard"), hosts.Help)
}

func TestSchemaDeployOptionsIsAny(t *testing.T) {
	projects := schemaField(t, Schema().Fields, "projects")
	deploy := schemaField(t, projects.Item.Fields, "deploy")
	assert.Equal(t, "section", deploy.Type)
	options := schemaField(t, deploy.Fields, "options")
	assert.Equal(t, "any", options.Type)
	assert.Empty(t, options.Fields)
	assert.Nil(t, options.Item, "an any value is not descended into")
}
