package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestManagementToolsProjectSkillAndHistory(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	call := func(name, stringArgs string) (any, error) { return s.invoke(ctx, name, json.RawMessage(stringArgs)) }
	list, err := call("skills_list", "{}")
	if err != nil || list == nil {
		t.Fatalf("skills: %v", err)
	}
	active, err := call("skills_read", `{"name":"ponytail-v2","offset":0}`)
	if err != nil || active == nil {
		t.Fatalf("Ponytail: %v", err)
	}
	if _, err = call("project_create", `{"project":"alpha","title":"Demo","repository":"owner/repo","notebook":"notebooks/run.ipynb"}`); err != nil {
		t.Fatal(err)
	}
	if _, err = call("project_memory_add", `{"project":"alpha","content":"Verificar entorno Kaggle"}`); err != nil {
		t.Fatal(err)
	}
	if _, err = call("tasks_add", `{"project":"alpha","title":"Ejecutar pruebas"}`); err != nil {
		t.Fatal(err)
	}
	data, err := call("projects_list", "{}")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(data)
	if !strings.Contains(string(b), "Verificar entorno Kaggle") || !strings.Contains(string(b), "Ejecutar pruebas") {
		t.Fatalf("estado no persistido: %s", b)
	}
	if _, err = call("project_verify", `{"project":"alpha","test_command":"","notebook_command":""}`); err == nil {
		t.Fatal("permitió comprobante sin ejecutar pruebas")
	}
	if _, err = call("gradio_scaffold", `{"project":"alpha","tests_passed":false}`); err == nil {
		t.Fatal("permitió Gradio sin verificación")
	}
	if _, err = call("gradio_scaffold", `{"project":"alpha","tests_passed":true}`); err == nil {
		t.Fatal("permitió Gradio basándose solo en tests_passed=true")
	}
	found := false
	for _, v := range s.definitions() {
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), `"name":"skills_read"`) {
			found = true
		}
	}
	if !found {
		t.Fatal("no se publicaron Skills en MCP")
	}
}
