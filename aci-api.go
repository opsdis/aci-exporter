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
	"context"
	"fmt"
	"github.com/Knetic/govaluate"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/tidwall/gjson"
	"github.com/umisama/go-regexpcache"
	"strconv"
	"strings"
	"time"
)

func newAciAPI(ctx context.Context, fabricConfig Fabric, configQueries AllQueries, queryFilter string) *aciAPI {

	executeQueries := configQueries
	queryArray := strings.Split(queryFilter, ",")
	if queryArray[0] != "" {
		// If there are some queries named
		executeQueries.ClassQueries = ClassQueries{}
		executeQueries.CompoundClassQueries = CompoundClassQueries{}
		for _, queryName := range queryArray {
			for configQueryName := range configQueries.ClassQueries {
				if queryName == configQueryName {
					executeQueries.ClassQueries[configQueryName] = configQueries.ClassQueries[configQueryName]
				}
			}
			for k := range configQueries.CompoundClassQueries {
				if queryName == k {
					executeQueries.CompoundClassQueries[k] = configQueries.CompoundClassQueries[k]
				}
			}
		}
	} else {
		// Use all configured
		executeQueries = configQueries
	}

	api := &aciAPI{
		ctx:                      ctx,
		connection:               *newAciConnction(ctx, fabricConfig),
		metricPrefix:             viper.GetString("prefix"),
		configQueries:            executeQueries.ClassQueries,
		configAggregationQueries: executeQueries.CompoundClassQueries,
		confgBuiltInQueries:      BuilitinQueries{},
	}

	// Make sure all built in queries are handled
	if queryArray[0] != "" {
		// If query parameter queries is used
		for _, v := range queryArray {
			if v == "faults" {
				api.confgBuiltInQueries["faults"] = api.faults
			}
			// Add all other builtin with if statements
		}
	} else {
		// If query parameter queries is NOT used, include all
		api.confgBuiltInQueries["faults"] = api.faults
		// Add all other builtin
	}

	return api
}

type aciAPI struct {
	ctx                      context.Context
	connection               AciConnection
	metricPrefix             string
	configQueries            ClassQueries
	configAggregationQueries CompoundClassQueries
	confgBuiltInQueries      BuilitinQueries
}

// CollectMetrics Gather all aci metrics and return name of the aci fabric, slice of metrics and status of
// successful login
func (p aciAPI) CollectMetrics() (string, []MetricDefinition, error) {
	start := time.Now()

	err := p.connection.login()
	defer p.connection.logout()

	if err != nil {
		return "", nil, err
	}

	fabricName, err := p.getFabricName()
	if err != nil {
		return "", nil, err
	}

	// Hold all metrics created during the session
	var metrics []MetricDefinition
	ch := make(chan []MetricDefinition)

	// Built-in
	go p.configuredBuiltInMetrics(ch)

	// Execute all configured class queries
	go p.configuredClassMetrics(ch)

	// Execute all configured compound queries
	go p.configuredCompoundsMetrics(ch)

	for i := 0; i < 3; i++ {
		metrics = append(metrics, <-ch...)
	}

	end := time.Since(start)
	metrics = append(metrics, *p.scrape(end.Seconds()))

	log.WithFields(log.Fields{
		"requestid": p.ctx.Value("requestid"),
		"exec_time": end.Microseconds(),
		"system":    "scrape",
	}).Info("total scrape time ")
	return fabricName, metrics, nil
}

func (p aciAPI) scrape(seconds float64) *MetricDefinition {
	metricDefinition := MetricDefinition{}
	metricDefinition.Name = "scrape_duration"
	metricDefinition.Description = MetricDesc{
		Help: "The duration, in seconds, of the last scrape of the fabric",
		Type: "gauge",
		Unit: "seconds",
	}
	metricDefinition.Metrics = []Metric{}

	metric := Metric{}
	metric.Labels = make(map[string]string)
	metric.Value = seconds

	metricDefinition.Metrics = append(metricDefinition.Metrics, metric)

	return &metricDefinition
}

func (p aciAPI) configuredBuiltInMetrics(chall chan []MetricDefinition) {
	var metricDefinitions []MetricDefinition
	ch := make(chan []MetricDefinition)
	for _, fun := range p.confgBuiltInQueries {
		go fun(ch)
	}

	for range p.confgBuiltInQueries {
		metricDefinitions = append(metricDefinitions, <-ch...)
	}

	chall <- metricDefinitions
}

func (p aciAPI) faults(ch chan []MetricDefinition) {
	data, err := p.connection.getByQuery("faults")
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": p.ctx.Value("requestid"),
		}).Error("faults not supported", err)
		ch <- nil
	}

	metricDefinitionFaults := MetricDefinition{}
	metricDefinitionFaults.Name = "faults"
	metricDefinitionFaults.Description = MetricDesc{
		Help: "Returns the total number of faults by type",
		Type: "gauge",
		Unit: "",
	}

	var metrics []Metric
	children := gjson.Get(data, "imdata.0.faultCountsWithDetails.children.#.faultTypeCounts")

	children.ForEach(func(key, value gjson.Result) bool {

		metric := Metric{}
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "crit"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.crit").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "maj"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.maj").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "minor"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.minor").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "warn"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.warn").Str)
		metrics = append(metrics, metric)

		return true // keep iterating
	})

	metricDefinitionFaults.Metrics = metrics

	metrics = []Metric{}
	metricDefinitionAcked := MetricDefinition{}
	metricDefinitionAcked.Name = "faults_acked"
	metricDefinitionAcked.Description = MetricDesc{
		Help: "Returns the total number of acknowledged faults by type",
		Type: "gauge",
		Unit: "",
	}

	children.ForEach(func(key, value gjson.Result) bool {

		metric := Metric{}
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "crit"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.critAcked").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "maj"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.majAcked").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "minor"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.minorAcked").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "warn"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.warnAcked").Str)
		metrics = append(metrics, metric)

		return true // keep iterating
	})

	metricDefinitionAcked.Metrics = metrics

	ch <- []MetricDefinition{metricDefinitionFaults, metricDefinitionAcked}
}

func (p aciAPI) getFabricName() (string, error) {
	data, err := p.connection.getByQuery("fabric_name")
	if err != nil {
		return "", err
	}

	return gjson.Get(data, "imdata.0.infraCont.attributes.fbDmNm").Str, nil
}

func (p aciAPI) configuredCompoundsMetrics(chall chan []MetricDefinition) {
	var metricDefinitions []MetricDefinition
	ch := make(chan []MetricDefinition)
	for _, v := range p.configAggregationQueries {
		go p.getCompoundMetrics(ch, v)
	}

	for range p.configAggregationQueries {
		metricDefinitions = append(metricDefinitions, <-ch...)
	}

	chall <- metricDefinitions
}

func (p aciAPI) getCompoundMetrics(ch chan []MetricDefinition, v *CompoundClassQuery) {
	var metricDefinitions []MetricDefinition
	metricDefinition := MetricDefinition{}
	metricDefinition.Name = v.Metrics[0].Name
	metricDefinition.Description.Help = v.Metrics[0].Help
	metricDefinition.Description.Type = v.Metrics[0].Type
	metricDefinition.Description.Unit = v.Metrics[0].Unit

	var metrics []Metric
	for _, classlabel := range v.ClassNames {
		metric := Metric{}
		data, _ := p.connection.getByClassQuery(classlabel.Class, classlabel.QueryParameter)
		if classlabel.ValueName == "" {
			metric.Value = p.toFloat(gjson.Get(data, fmt.Sprintf("imdata.0.%s", v.Metrics[0].ValueName)).Str)
		} else {
			metric.Value = p.toFloat(gjson.Get(data, fmt.Sprintf("imdata.0.%s", classlabel.ValueName)).Str)
		}
		metric.Labels = make(map[string]string)
		metric.Labels[v.LabelName] = classlabel.Label
		metrics = append(metrics, metric)
	}
	metricDefinition.Metrics = metrics
	metricDefinitions = append(metricDefinitions, metricDefinition)
	ch <- metricDefinitions
}

func (p aciAPI) configuredClassMetrics(chall chan []MetricDefinition) {
	var metricDefinitions []MetricDefinition
	ch := make(chan []MetricDefinition)
	for _, v := range p.configQueries {
		go p.getClassMetrics(ch, v)
	}

	for range p.configQueries {
		metricDefinitions = append(metricDefinitions, <-ch...)
	}

	chall <- metricDefinitions
}

func (p aciAPI) getClassMetrics(ch chan []MetricDefinition, v *ClassQuery) {
	var metricDefinitions []MetricDefinition
	data, err := p.connection.getByClassQuery(v.ClassName, v.QueryParameter)

	if err != nil {
		log.WithFields(log.Fields{
			"requestid": p.ctx.Value("requestid"),
		}).Error(fmt.Sprintf("%s not supported", v.ClassName), err)
		ch <- nil
	}

	// For each metrics in the config
	for _, mv := range v.Metrics {
		metricDefinition := MetricDefinition{}
		metricDefinition.Name = mv.Name
		metricDefinition.Description.Help = mv.Help
		metricDefinition.Description.Type = mv.Type
		metricDefinition.Description.Unit = mv.Unit

		var metrics []Metric

		metrics = p.extractClassQueriesData(data, v, mv, metrics)

		metricDefinition.Metrics = metrics

		metricDefinitions = append(metricDefinitions, metricDefinition)
	}
	ch <- metricDefinitions
}

func (p aciAPI) extractClassQueriesData(data string, v *ClassQuery, mv ConfigMetric, metrics []Metric) []Metric {
	result := gjson.Get(data, "imdata")

	result.ForEach(func(key, value gjson.Result) bool {

		metric := Metric{}

		// find and parse all labels
		metric.Labels = make(map[string]string)
		for _, lv := range v.Labels {
			re := regexpcache.MustCompile(lv.Regex)
			match := re.FindStringSubmatch(gjson.Get(value.String(), lv.PropertyName).Str)
			if len(match) != 0 {
				for i, name := range re.SubexpNames() {
					if i != 0 && name != "" {
						metric.Labels[name] = match[i]
					}
				}
			}
		}

		metric.Value = p.toFloat(gjson.Get(value.String(), mv.ValueName).Str)

		// Post calculation on the value
		if mv.ValueCalculation != "" {
			expression, _ := govaluate.NewEvaluableExpression(mv.ValueCalculation)
			parameters := make(map[string]interface{}, 8)
			parameters["value"] = p.toFloat(gjson.Get(value.String(), mv.ValueName).Str)
			result, _ := expression.Evaluate(parameters)
			metric.Value = result.(float64)
		}

		// Transform on string value table to float
		if len(mv.ValueTransform) != 0 {
			if val, ok := mv.ValueTransform[gjson.Get(value.String(), mv.ValueName).Str]; ok {
				metric.Value = val
			}
		}

		metrics = append(metrics, metric)
		return true
	})
	return metrics
}

func (p aciAPI) toRatio(value string) float64 {
	rate, _ := strconv.ParseFloat(value, 64)
	return rate / 100.0
}

func (p aciAPI) toFloat(value string) float64 {
	rate, _ := strconv.ParseFloat(value, 64)
	return rate
}
