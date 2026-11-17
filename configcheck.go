package main

import (
	"errors"
	"fmt"
	"sort"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

// The check* types mirror the query configuration for the unknown key check only.

// checkGroupEntry is one entry of a group query. By convention an entry starts with a
// name key without value (e.g. "- node_health:"), which Extra collects. Keys with a value
// in Extra are unknown options.
type checkGroupEntry struct {
	ClassQuery `yaml:",inline"`
	Extra      map[string]interface{} `yaml:",inline"`
}

type checkGroupQuery struct {
	Name         string            `yaml:"name"`
	Unit         string            `yaml:"unit"`
	Type         string            `yaml:"type"`
	Help         string            `yaml:"help"`
	Queries      []checkGroupEntry `yaml:"queries"`
	StaticLabels []StaticLabels    `yaml:"staticlabels"`
}

// checkDirQueries is a file in the config directory, it only holds queries.
type checkDirQueries struct {
	ClassQueries         ClassQueries                `yaml:"class_queries"`
	CompoundClassQueries CompoundClassQueries        `yaml:"compound_queries"`
	GroupClassQueries    map[string]*checkGroupQuery `yaml:"group_class_queries"`
}

// checkMainQueries is the main config file. Only the query sections are checked,
// Rest takes all other top-level keys.
type checkMainQueries struct {
	ClassQueries         ClassQueries                `yaml:"class_queries"`
	CompoundClassQueries CompoundClassQueries        `yaml:"compound_queries"`
	GroupClassQueries    map[string]*checkGroupQuery `yaml:"qroup_class_queries"`
	Rest                 map[string]interface{}      `yaml:",inline"`
}

// unknownConfigKeys returns one message per unknown key in a query configuration.
// Unknown keys are otherwise ignored silently, so a misspelled option is just dropped.
// Syntax errors are not reported here, the regular parsing handles them.
func unknownConfigKeys(data []byte, mainFile bool) []string {
	var groups map[string]*checkGroupQuery
	var err error
	if mainFile {
		cfg := checkMainQueries{}
		err = yaml.UnmarshalStrict(data, &cfg)
		groups = cfg.GroupClassQueries
	} else {
		cfg := checkDirQueries{}
		err = yaml.UnmarshalStrict(data, &cfg)
		groups = cfg.GroupClassQueries
	}

	var msgs []string
	var typeErr *yaml.TypeError
	if err != nil {
		if !errors.As(err, &typeErr) {
			return nil
		}
		msgs = append(msgs, typeErr.Errors...)
	}

	groupNames := make([]string, 0, len(groups))
	for name := range groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	for _, name := range groupNames {
		if groups[name] == nil {
			continue
		}
		for _, entry := range groups[name].Queries {
			keys := make([]string, 0, len(entry.Extra))
			for key, value := range entry.Extra {
				if value != nil {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for _, key := range keys {
				msgs = append(msgs, fmt.Sprintf("group query %s: field %s not found in type main.ClassQuery", name, key))
			}
		}
	}
	return msgs
}

// warnUnknownConfigKeys logs a warning for every unknown key, it never stops the exporter.
func warnUnknownConfigKeys(file string, data []byte, mainFile bool) {
	for _, msg := range unknownConfigKeys(data, mainFile) {
		log.WithFields(log.Fields{
			"file": file,
		}).Warn("Unknown configuration - ", msg)
	}
}
