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

import (
	"fmt"
	"sort"
)

// Metric is a Promethues structure of the data
type Metric struct {
	Name        string
	Value       float64
	Labels      map[string]string
	Timestamp   float64
	State       int
	Description MetricDesc
}

// MetricDesc the Promethues help and type text
type MetricDesc struct {
	Help string
	Type string
}

// Labels2Prometheus create a string of all labels, sorted by label name
func (m Metric) Labels2Prometheus(commonLabels map[string]string) string {
	// append all common maps
	if len(commonLabels) != 0 {
		for k, v := range commonLabels {
			m.Labels[k] = v
		}
	}

	keys := make([]string, 0, len(m.Labels))
	for k := range m.Labels {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	labelstr := ""
	sep := ""
	for _, k := range keys {
		labelstr = labelstr + fmt.Sprintf("%s%s=\"%s\"", sep, k, m.Labels[k])
		sep = ","
	}
	return labelstr
}

// Metrics2Prometheus convert a slice of Metric to Prometheus text output
func Metrics2Prometheus(metrics []Metric, prefix string, commonLabels map[string]string) string {
	promFormat := ""

	for _, metric := range metrics {
		if metric.Description.Help != "" && metric.Description.Type != "" {
			promFormat = promFormat + fmt.Sprintf("# HELP %s\n", metric.Description.Help)
			promFormat = promFormat + fmt.Sprintf("# TYPE %s\n", metric.Description.Type)
		}
		promFormat = promFormat + fmt.Sprintf("%s%s{%s} %g\n", prefix, metric.Name, metric.Labels2Prometheus(commonLabels), metric.Value)
	}

	return promFormat
}
