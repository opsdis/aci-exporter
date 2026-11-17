package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestUnknownConfigKeysExamples(t *testing.T) {
	data, err := os.ReadFile("example-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if msgs := unknownConfigKeys(data, true); len(msgs) != 0 {
		t.Errorf("example-config.yaml: unexpected warnings %v", msgs)
	}

	files, err := filepath.Glob("config.d/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatal("no files in config.d", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if msgs := unknownConfigKeys(data, false); len(msgs) != 0 {
			t.Errorf("%s: unexpected warnings %v", file, msgs)
		}
	}
}

func TestUnknownConfigKeys(t *testing.T) {
	tests := []struct {
		name     string
		mainFile bool
		data     string
		want     []string
	}{
		{
			name: "misspelled query option",
			data: `
class_queries:
  faults:
    class_name: faultInst
    query_paramter: "?query-target-filter=eq(faultInst.lc,\"raised\")"
`,
			want: []string{"line 5: field query_paramter not found in type main.ClassQuery"},
		},
		{
			name: "misspelled metric and label option",
			data: `
compound_queries:
  count:
    classnames:
      - class_name: fvTenant
        label_value: tenant
    metrics:
      - name: count
        value_nam: count
    labelname: class
`,
			want: []string{"line 9: field value_nam not found in type main.ConfigMetric"},
		},
		{
			name: "group query entry name without value is fine",
			data: `
group_class_queries:
  health:
    name: health
    queries:
      - node_health:
        class_name: topSystem
        query_parameter: "?rsp-subtree-include=health"
`,
		},
		{
			name: "misspelled group query entry option",
			data: `
group_class_queries:
  health:
    name: health
    queries:
      - node_health:
        class_name: topSystem
        query_paramter: "?rsp-subtree-include=health"
`,
			want: []string{"group query health: field query_paramter not found in type main.ClassQuery"},
		},
		{
			name: "unknown top-level key in config directory",
			data: `
class_querys:
  faults:
    class_name: faultInst
`,
			want: []string{"line 2: field class_querys not found in type main.checkDirQueries"},
		},
		{
			name:     "other top-level keys in main file are not checked",
			mainFile: true,
			data: `
port: 9643
fabrics:
  fab1:
    apic: [https://apic1]
qroup_class_queries:
  health:
    name: health
    queries:
      - node_health:
        class_name: topSystem
`,
		},
		{
			name: "syntax errors are left to the regular parsing",
			data: "class_queries: [",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unknownConfigKeys([]byte(tt.data), tt.mainFile)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
