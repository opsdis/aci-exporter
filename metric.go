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
	log "github.com/sirupsen/logrus"
	"regexp"
	"sort"
	"strings"
)

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

func NewMetricFormat(openmetrics bool, lowerCase bool, snakeCase bool) MetricFormat {
	return MetricFormat{openmetrics: openmetrics, snakeCase: snakeCase, lowerCase: lowerCase}
}

type MetricFormat struct {
	openmetrics bool
	snakeCase   bool
	lowerCase   bool
}

var matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
var matchAllCap = regexp.MustCompile("([a-z0-9])([A-Z])")

func toLowerLabels(key string, format MetricFormat) string {
	if format.snakeCase {
		return toSnakeCase(key)
	}
	if format.lowerCase {
		return strings.ToLower(key)
	}
	return key
}

func toSnakeCase(str string) string {
	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

// Labels2Prometheus create a string of all labels, sorted by label name
func (m Metric) Labels2Prometheus(commonLabels map[string]string, format MetricFormat) string {
	// append all common maps
	if len(commonLabels) != 0 {
		for k, v := range commonLabels {
			m.Labels[toLowerLabels(k, format)] = v
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
			labelstr = labelstr + fmt.Sprintf("%s%s=\"%s\"", sep, toLowerLabels(k, format), m.Labels[k])
			sep = ","
		}
	}
	return labelstr
}

// Metrics2Prometheus convert a slice of Metric to Prometheus text output
func Metrics2PrometheusOLD(metrics []MetricDefinition, prefix string, commonLabels map[string]string, format MetricFormat) string {
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
			if metricDefinition.Description.Help == "" {
				promFormat = promFormat + fmt.Sprintf("# HELP %s%s %s\n", prefix, metricName, "Missing description")
			} else {
				promFormat = promFormat + fmt.Sprintf("# HELP %s%s %s\n", prefix, metricName, metricDefinition.Description.Help)
			}

			promType := "gauge"
			if metricDefinition.Description.Type != "" {
				promType = metricDefinition.Description.Type
			}
			if format.openmetrics {
				if strings.HasSuffix(metricName, "_info") {
					promFormat = promFormat + fmt.Sprintf("# TYPE %s%s %s\n", prefix, metricName, "info")
				} else {
					promFormat = promFormat + fmt.Sprintf("# TYPE %s%s %s\n", prefix, metricName, promType)
				}
				promFormat = promFormat + fmt.Sprintf("# UNIT %s%s %s\n", prefix, metricName, metricDefinition.Description.Unit)
			} else {
				promFormat = promFormat + fmt.Sprintf("# TYPE %s%s %s\n", prefix, metricName, promType)
			}

			for _, metric := range metricDefinition.Metrics {
				promFormat = promFormat + fmt.Sprintf("%s%s{%s} %g\n", prefix, metricName, metric.Labels2Prometheus(commonLabels, format), metric.Value)
			}
		}
	}
	if format.openmetrics {
		promFormat = promFormat + "# EOF\n"
	}
	return promFormat
}

// Metrics2Prometheus convert a slice of Metric to Prometheus text output
func Metrics2Prometheus(metrics []MetricDefinition, prefix string, commonLabels map[string]string, format MetricFormat) string {
	var builder strings.Builder

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
			if metricDefinition.Description.Help == "" {
				addText(&builder, fmt.Sprintf("# HELP %s%s %s\n", prefix, metricName, "Missing description"))
			} else {
				addText(&builder, fmt.Sprintf("# HELP %s%s %s\n", prefix, metricName, metricDefinition.Description.Help))
			}

			promType := "gauge"
			if metricDefinition.Description.Type != "" {
				promType = metricDefinition.Description.Type
			}
			if format.openmetrics {
				if strings.HasSuffix(metricName, "_info") {
					addText(&builder, fmt.Sprintf("# TYPE %s%s %s\n", prefix, metricName, "info"))
				} else {
					addText(&builder, fmt.Sprintf("# TYPE %s%s %s\n", prefix, metricName, promType))
				}
				addText(&builder, fmt.Sprintf("# UNIT %s%s %s\n", prefix, metricName, metricDefinition.Description.Unit))
			} else {
				addText(&builder, fmt.Sprintf("# TYPE %s%s %s\n", prefix, metricName, promType))
			}

			for _, metric := range metricDefinition.Metrics {
				addText(&builder, fmt.Sprintf("%s%s{%s} %g\n", prefix, metricName, metric.Labels2Prometheus(commonLabels, format), metric.Value))
			}
		}
	}
	if format.openmetrics {
		addText(&builder, "# EOF\n")
	}
	return builder.String()
}

func addText(builder *strings.Builder, text string) {
	_, err := builder.WriteString(text)
	if err != nil {
		log.Fatal(err)
	}
}
