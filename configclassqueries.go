// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
//
// Copyright 2020 Opsdis AB

package main

type ClassQueries map[string]*ClassQuery
type CompoundClassQueries map[string]*CompoundClassQuery
type GroupClassQueries map[string]*GroupClassQuery

// Builtin queries named and point to a function to execute
type BuilitinQueries map[string]func(chan []MetricDefinition)

type AllQueries struct {
	ClassQueries         ClassQueries
	CompoundClassQueries CompoundClassQueries
	GroupClassQueries    GroupClassQueries
}

type GroupClassQuery struct {
	Name         string         `mapstructure:"name"`
	Unit         string         `mapstructure:"unit"`
	Type         string         `mapstructure:"type"`
	Help         string         `mapstructure:"help"`
	Queries      []ClassQuery   `string:"queries"`
	StaticLabels []StaticLabels `string:"staticlabels"`
}

// ClassQuery define the structure of configured queries
type ClassQuery struct {
	ClassName      string         `mapstructure:"class_name"`
	QueryParameter string         `mapstructure:"query_parameter"`
	Metrics        []ConfigMetric `string:"metrics"`
	Labels         []ConfigLabels `string:"labels"`
	StaticLabels   []StaticLabels `string:"staticlabels"`
}

// ConfigMetric define the configuration of metric
type ConfigMetric struct {
	Name                string             `mapstructure:"name"`
	ValueName           string             `mapstructure:"value_name"`
	ValueCalculation    string             `mapstructure:"value_calculation"`
	Unit                string             `mapstructure:"unit"`
	Type                string             `mapstructure:"type"`
	Help                string             `mapstructure:"help"`
	ValueTransform      map[string]float64 `mapstructure:"value_transform"`
	ValueRegexTransform string             `mapstructure:"value_regex_transformation"`
}

// ConfigLabels define the configuration of label to parse
type ConfigLabels struct {
	PropertyName string `mapstructure:"property_name"`
	Regex        string `mapstructure:"regex"`
}

type StaticLabels struct {
	Key   string `mapstructure:"key"`
	Value string `mapstructure:"value"`
}

// CompoundClassQuery define aggregation by common label, typical used for counting
type CompoundClassQuery struct {
	ClassNames []ClassLabelMapping `string:"classnames"`
	Metrics    []ConfigMetric      `string:"metrics"`
	LabelName  string              `mapstructure:"labelname"`
}

type ClassLabelMapping struct {
	Class          string `mapstructure:"class_name"`
	Label          string `mapstructure:"label_value"`
	QueryParameter string `mapstructure:"query_parameter"`
	ValueName      string `mapstructure:"value_name"`
}
