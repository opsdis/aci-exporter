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
	"encoding/json"
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

var arrayExtension = regexpcache.MustCompile("^(?P<stage_1>.*)\\.\\[(?P<child_name>.*)\\](?P<stage_2>.*)")

func newAciAPI(ctx context.Context, fabricConfig Fabric, configQueries AllQueries, queryFilter string) *aciAPI {

	executeQueries := configQueries
	queryArray := strings.Split(queryFilter, ",")
	if queryArray[0] != "" {
		// If there are some queries named
		executeQueries.ClassQueries = ClassQueries{}
		executeQueries.CompoundClassQueries = CompoundClassQueries{}
		executeQueries.GroupClassQueries = GroupClassQueries{}
		// Find the named queries for the different type
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
			for k := range configQueries.GroupClassQueries {
				if queryName == k {
					executeQueries.GroupClassQueries[k] = configQueries.GroupClassQueries[k]
				}
			}
		}
	} else {
		// Use all configured
		executeQueries = configQueries
	}

	api := &aciAPI{
		ctx:                   ctx,
		connection:            *newAciConnction(ctx, fabricConfig),
		metricPrefix:          viper.GetString("prefix"),
		configQueries:         executeQueries.ClassQueries,
		configCompoundQueries: executeQueries.CompoundClassQueries,
		configGroupQueries:    executeQueries.GroupClassQueries,
		confgBuiltInQueries:   BuilitinQueries{},
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
	ctx                   context.Context
	connection            AciConnection
	metricPrefix          string
	configQueries         ClassQueries
	configCompoundQueries CompoundClassQueries
	configGroupQueries    GroupClassQueries
	confgBuiltInQueries   BuilitinQueries
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

	aciName, err := p.getAciName()
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

	// Execute all configured group queries
	go p.configuredGroupMetrics(ch)

	for i := 0; i < 4; i++ {
		metrics = append(metrics, <-ch...)
	}

	end := time.Since(start)
	metrics = append(metrics, *p.scrape(end.Seconds()))

	log.WithFields(log.Fields{
		"requestid": p.ctx.Value("requestid"),
		"exec_time": end.Microseconds(),
		"fabric":    fmt.Sprintf("%v", p.ctx.Value("fabric")),
	}).Info("total scrape time ")
	return aciName, metrics, nil
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
			"fabric":    fmt.Sprintf("%v", p.ctx.Value("fabric")),
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

func (p aciAPI) getAciName() (string, error) {
	data, err := p.connection.getByQuery("aci_name")
	if err != nil {
		return "", err
	}

	return gjson.Get(data, "imdata.0.infraCont.attributes.fbDmNm").Str, nil
}

func (p aciAPI) configuredCompoundsMetrics(chall chan []MetricDefinition) {
	var metricDefinitions []MetricDefinition
	ch := make(chan []MetricDefinition)
	for _, v := range p.configCompoundQueries {
		go p.getCompoundMetrics(ch, v)
	}

	for range p.configCompoundQueries {
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

func (p aciAPI) configuredGroupMetrics(chall chan []MetricDefinition) {
	var metricDefinitions []MetricDefinition
	ch := make(chan []MetricDefinition)

	for _, v := range p.configGroupQueries {
		go p.getGroupClassMetrics(ch, *v)
	}

	for range p.configGroupQueries {
		metricDefinitions = append(metricDefinitions, <-ch...)
	}

	chall <- metricDefinitions
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
func (p aciAPI) getGroupClassMetrics(ch chan []MetricDefinition, v GroupClassQuery) {
	var metricDefinitions []MetricDefinition

	metricDefinition := MetricDefinition{}

	metricDefinition.Name = v.Name
	metricDefinition.Description.Help = v.Help
	metricDefinition.Description.Type = v.Type
	metricDefinition.Description.Unit = v.Unit

	/*
		labels := make(map[string]string)
		for _,v := range v.StaticLabels {
			labels[v.Key] = v.Value
		}
	*/
	var metrics []Metric
	metricDefinition.Metrics = metrics

	chsub := make(chan []MetricDefinition)

	for _, query := range v.Queries {
		// Need copy by value
		queryValue := ClassQuery{
			ClassName:      query.ClassName,
			QueryParameter: query.QueryParameter,
			Metrics:        query.Metrics,
			Labels:         query.Labels,
			StaticLabels:   query.StaticLabels,
		}

		go p.getClassMetrics(chsub, &queryValue)
	}

	for range v.Queries {
		md := <-chsub
		for _, vx := range md {
			for _, vy := range vx.Metrics {
				// Add any static labels
				for _, v := range v.StaticLabels {
					vy.Labels[v.Key] = v.Value
				}
			}
			metricDefinition.Metrics = append(metricDefinition.Metrics, vx.Metrics...)
		}
	}

	metricDefinitions = append(metricDefinitions, metricDefinition)
	ch <- metricDefinitions
}

func (p aciAPI) getClassMetrics(ch chan []MetricDefinition, v *ClassQuery) {

	var metricDefinitions []MetricDefinition
	data, err := p.connection.getByClassQuery(v.ClassName, v.QueryParameter)

	if err != nil {
		log.WithFields(log.Fields{
			"requestid": p.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", p.ctx.Value("fabric")),
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

func (p aciAPI) extractClassQueriesData(data string, classQuery *ClassQuery, mv ConfigMetric, metrics []Metric) []Metric {
	result := gjson.Get(data, "imdata")

	result.ForEach(func(key, value gjson.Result) bool {

		// Check if the value_name is in the format of fvAEPg.children.[healthInst].attributes.cur
		match := arrayExtension.FindStringSubmatch(mv.ValueName)
		if len(match) > 0 {

			// match is a string array of parsed if the .[regexp]. is part of the string
			// 0: the original string
			// 1: stage1 all before .[
			// 2: the child_name between []
			// 3: stage2 - the rest after ].

			var allChildren []map[string]interface{}

			allChildrenJson := gjson.Get(value.Raw, match[1])
			json.Unmarshal([]byte(allChildrenJson.Raw), &allChildren)

			for childIndex, child := range allChildren {
				for childKey, childValue := range child {
					// add a check if the childKey match the regexp of match[2]
					re := regexpcache.MustCompile(match[2])

					_, ok := childValue.(map[string]interface{})
					if ok && re.Match([]byte(childKey)) {
						metric := Metric{}
						metric.Labels = make(map[string]string)

						mvLocal := ConfigMetric{
							Name:             mv.Name,
							ValueName:        childKey + match[3],
							ValueCalculation: mv.ValueCalculation,
							Unit:             mv.Unit,
							Type:             mv.Type,
							Help:             mv.Help,
							ValueTransform:   mv.ValueTransform,
						}

						// Add all high level labels
						addLabels(classQuery.Labels, classQuery.StaticLabels, value.String(), metric)

						// Add all [*] labels that will be relative to the child key
						// Rewrite them from the relative path and add them as Config labels
						var childLabels []ConfigLabels
						for _, configLabel := range classQuery.Labels {
							matchLabels := arrayExtension.FindStringSubmatch(configLabel.PropertyName)

							//if len(matchLabels)  >0 && matchLabels[2] == "*" {
							if len(matchLabels) > 0 && re.Match([]byte(childKey)) {
								re := regexpcache.MustCompile(matchLabels[2])
								if re.Match([]byte(childKey)) {
									localLabel := ConfigLabels{}
									localLabel.PropertyName = childKey + matchLabels[3]
									localLabel.Regex = configLabel.Regex
									childLabels = append(childLabels, localLabel)
								}
							}
						}

						childJson, _ := json.Marshal(allChildren[childIndex])
						addLabels(childLabels, nil, string(childJson), metric)

						// Extract labels from child
						for _, keyLabel := range childLabels {
							if keyLabel.PropertyName == childKey {
								re := regexpcache.MustCompile(keyLabel.Regex)
								match := re.FindStringSubmatch(childKey)
								if len(match) != 0 {
									for i, expName := range re.SubexpNames() {
										if i != 0 && expName != "" {
											metric.Labels[expName] = match[i]
										}
									}
								}
							}
						}

						// extract the metrics value
						metric.Value = p.toFloatTransform(gjson.Get(string(childJson), mvLocal.ValueName).Str, mvLocal)
						valueReCalculation(mv, &metric)

						metrics = append(metrics, metric)
					}
				}
			}
		} else {
			// Just plain Gjson without any [] expressions
			metric := Metric{}

			// find and parse all labels
			metric.Labels = make(map[string]string)
			addLabels(classQuery.Labels, classQuery.StaticLabels, value.String(), metric)

			// get the metrics value
			metric.Value = p.toFloatTransform(gjson.Get(value.String(), mv.ValueName).Str, mv)

			// Post calculation on the value
			valueReCalculation(mv, &metric)

			metrics = append(metrics, metric)
		}

		return true
	})
	return metrics
}

func valueReCalculation(mv ConfigMetric, metric *Metric) {
	if mv.ValueCalculation != "" {
		expression, _ := govaluate.NewEvaluableExpression(mv.ValueCalculation)
		parameters := make(map[string]interface{}, 8)
		parameters["value"] = metric.Value //p.toFloat(gjson.Get(value.String(), mv.ValueName).Str)
		result, _ := expression.Evaluate(parameters)
		metric.Value = result.(float64)
	}
}

func addLabels(v []ConfigLabels, sv []StaticLabels, json string, metric Metric) {
	for _, lv := range v {
		re := regexpcache.MustCompile(lv.Regex)
		match := re.FindStringSubmatch(gjson.Get(json, lv.PropertyName).Str)
		if len(match) != 0 {
			for i, expName := range re.SubexpNames() {
				if i != 0 && expName != "" {
					metric.Labels[expName] = match[i]
				}
			}
		}
	}
	// Add static labels
	for _, slv := range sv {
		metric.Labels[slv.Key] = slv.Value
	}
}

func dumpMap(space string, m map[string]interface{}) {
	for k, v := range m {
		if mv, ok := v.(map[string]interface{}); ok {
			fmt.Printf("{ \"%v\": \n", k)
			dumpMap(space+"\t", mv)
			fmt.Printf("}\n")
		} else {
			fmt.Printf("%v %v : %v\n", space, k, v)
		}
	}
}

func (p aciAPI) toRatio(value string) float64 {
	rate, _ := strconv.ParseFloat(value, 64)
	return rate / 100.0
}

func (p aciAPI) toFloat(value string) float64 {
	rate, err := strconv.ParseFloat(value, 64)
	if err != nil {
		// if the value a date time convert to timestamp
		t, err := time.Parse(time.RFC3339, value)
		rate = float64(t.Unix())
		if err != nil {
			log.WithFields(log.Fields{
				"value": value,
			}).Info("could not convert value to float, will return 0.0 ")
			return 0.0
		}

	}
	return rate
}

func (p aciAPI) toFloatTransform(value string, mv ConfigMetric) float64 {
	var transformedValue = value
	if len(mv.ValueRegexTransform) != 0 {
		re := regexpcache.MustCompile(mv.ValueRegexTransform)
		match := re.FindStringSubmatch(value)
		if len(match) != 0 {
			transformedValue = match[1]
		}
	}

	if len(mv.ValueTransform) != 0 {
		if val, ok := mv.ValueTransform[transformedValue]; ok {
			return val
		}
	}

	return p.toFloat(transformedValue)

}
