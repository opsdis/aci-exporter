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
	"strings"
)

/*
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
*/

type MetricDefinition struct {
	Name        string // the name of the metrics
	Metrics     []Metric
	Description MetricDesc
}

// Metric the value, labels and timestamp of the metrics
type Metric struct {
	Value     float64
	Labels    map[string]string
	Timestamp float64
}

// MetricDesc the Prometheus help and type text
type MetricDesc struct {
	Help string
	Type string
	Unit string
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
		// Filter out empty labels
		if m.Labels[k] != "" {
			labelstr = labelstr + fmt.Sprintf("%s%s=\"%s\"", sep, k, m.Labels[k])
			sep = ","
		}
	}
	return labelstr
}

// Metrics2Prometheus convert a slice of Metric to Prometheus text output
func Metrics2Prometheus(metrics []MetricDefinition, prefix string, commonLabels map[string]string, openmetrics bool) string {
	promFormat := ""

	for _, metricDefinition := range metrics {

		// only format if the metrics slice include items
		metricName := metricDefinition.Name
		if metricDefinition.Description.Unit != "" {
			metricName = metricDefinition.Name + "_" + metricDefinition.Description.Unit
		}

		if metricDefinition.Description.Type == "counter" && metricDefinition.Description.Unit != "info" {
			metricName = metricName + "_total"
		}

		if len(metricDefinition.Metrics) > 0 {
			promFormat = promFormat + fmt.Sprintf("# HELP %s %s\n", metricName, metricDefinition.Description.Help)
			if openmetrics {
				if strings.HasSuffix(metricName, "_info") {
					promFormat = promFormat + fmt.Sprintf("# TYPE %s %s\n", metricName, "info")
				} else {
					promFormat = promFormat + fmt.Sprintf("# TYPE %s %s\n", metricName, metricDefinition.Description.Type)
				}
				promFormat = promFormat + fmt.Sprintf("# UNIT %s %s\n", metricName, metricDefinition.Description.Unit)
			} else {
				promFormat = promFormat + fmt.Sprintf("# TYPE %s %s\n", metricName, metricDefinition.Description.Type)
			}

			for _, metric := range metricDefinition.Metrics {
				promFormat = promFormat + fmt.Sprintf("%s%s{%s} %g\n", prefix, metricName, metric.Labels2Prometheus(commonLabels), metric.Value)
			}
		}
	}
	if openmetrics {
		promFormat = promFormat + "# EOF\n"
	}
	return promFormat
}
