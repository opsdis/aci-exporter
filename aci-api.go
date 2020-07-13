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
	"github.com/spf13/viper"
	"github.com/tidwall/gjson"
	"regexp"
	"strconv"
)

/*
 For mor information about health scoring in ACI please see:
 https://www.cisco.com/c/en/us/td/docs/switches/datacenter/aci/apic/sw/1-x/Operating_ACI/guide/b_Cisco_Operating_ACI/b_Cisco_Operating_ACI_chapter_01010.pdf
*/
var re_health = regexp.MustCompile("topology/pod-(.*?)/health")

//var re_podId = regexp.MustCompile("pod-(.*?)")

func newAciAPI(apichostname string, username string, password string) *aciAPI {

	ip := &aciAPI{
		connection: *newAciConnction(apichostname, username, password),

		//batchFetch:    viper.GetBool("monitor.batchfetch"),
		//batchFilter:   viper.GetString("monitor.batchfilter"),
		//batchInterval: viper.GetInt("monitor.batchinterval"),
		metricPrefix: viper.GetString("prefix"),
	}

	return ip
}

type aciAPI struct {
	connection    AciConnection
	batchFetch    bool
	batchFilter   string
	batchInterval int
	metricPrefix  string
}

// CollectMetrics Gather all aci metrics and return name of the aci fabric, slice of metrics and status of
// successful login
func (p aciAPI) CollectMetrics() (string, []Metric, bool) {

	status := p.connection.login()
	defer p.connection.logout()

	if !status {
		return "", nil, status
	}

	fabricName := p.getFabricName()
	// Hold all metrics created during the session
	metrics := []Metric{}

	metrics = append(metrics, p.fabricHealth()...)
	metrics = append(metrics, p.nodeHealth()...)
	metrics = append(metrics, p.tenantHealth()...)
	metrics = append(metrics, p.faults()...)
	metrics = append(metrics, p.infraNodeInfo()...)

	// Todo EPG health

	return fabricName, metrics, true
}

func (p aciAPI) fabricHealth() []Metric {
	data, err := p.connection.getByQuery("fabric_health")
	if err != nil {
		log.Error("fabric_health not supported", err)
		return nil
	}
	metrics := []Metric{}
	result := gjson.Get(data, "imdata")

	setDesc := true
	result.ForEach(func(key, value gjson.Result) bool {
		dn := gjson.Get(value.String(), "fabricHealthTotal.attributes.dn")

		metric := Metric{}

		match := re_health.FindStringSubmatch(dn.Str)
		if len(match) == 0 {
			metric.Name = "fabric_health_overall_ratio"

			metric.Labels = make(map[string]string)

			metric.Value = p.toRatio(gjson.Get(value.String(), "fabricHealthTotal.attributes.cur").Str)
			metric.Description = MetricDesc{
				Help: fmt.Sprintf("%s Returns the health score of the overall fabric", metric.Name),
				Type: fmt.Sprintf("%s gauge", metric.Name),
			}
			metrics = append(metrics, metric)
		} else {
			metric.Name = "pod_health_ratio"

			metric.Labels = make(map[string]string)
			metric.Labels["podid"] = match[1]

			metric.Value = p.toRatio(gjson.Get(value.String(), "fabricHealthTotal.attributes.cur").Str)
			if setDesc {
				metric.Description = MetricDesc{
					Help: fmt.Sprintf("%s Returns the health score of a pod", metric.Name),
					Type: fmt.Sprintf("%s gauge", metric.Name),
				}
				setDesc = false
			}
			metrics = append(metrics, metric)
		}
		return true
	})
	return metrics
}

// nodeHealth only leaf and spine nodes
func (p aciAPI) nodeHealth() []Metric {
	data, err := p.connection.getByQuery("node_health")
	if err != nil {
		log.Error("node_health not supported", err)
		return nil
	}

	metrics := []Metric{}
	result := gjson.Get(data, "imdata")

	result.ForEach(func(key, value gjson.Result) bool {
		role := gjson.Get(value.String(), "topSystem.attributes.role").Str
		metric := Metric{}

		if role != "controller" {

			metric.Name = "node_health_ratio"

			metric.Labels = make(map[string]string)
			metric.Labels["podid"] = gjson.Get(value.String(), "topSystem.attributes.podId").Str
			metric.Labels["state"] = gjson.Get(value.String(), "topSystem.attributes.state").Str
			metric.Labels["oobmgmtaddr"] = gjson.Get(value.String(), "topSystem.attributes.oobMgmtAddr").Str
			metric.Labels["nodeid"] = gjson.Get(value.String(), "topSystem.attributes.id").Str
			metric.Labels["name"] = gjson.Get(value.String(), "topSystem.attributes.name").Str
			metric.Labels["role"] = role

			metric.Value = p.toRatio(gjson.Get(value.String(), "topSystem.children.0.healthInst.attributes.cur").Str)

			metrics = append(metrics, metric)
		}
		return true // keep iterating
	})

	metrics[0].Description = MetricDesc{
		Help: fmt.Sprintf("%s Returns the health score of a fabric node", metrics[0].Name),
		Type: fmt.Sprintf("%s gauge", metrics[0].Name),
	}

	return metrics
}

func (p aciAPI) tenantHealth() []Metric {
	data, err := p.connection.getByQuery("tenant_health")
	if err != nil {
		log.Error("tenant_health not supported", err)
		return nil
	}

	metrics := []Metric{}
	result := gjson.Get(data, "imdata")

	result.ForEach(func(key, value gjson.Result) bool {

		metric := Metric{}

		metric.Name = "tenant_health_ratio"

		metric.Labels = make(map[string]string)
		metric.Labels["domain"] = gjson.Get(value.String(), "fvTenant.attributes.name").Str

		metric.Value = p.toRatio(gjson.Get(value.String(), "fvTenant.children.0.healthInst.attributes.cur").Str)

		metrics = append(metrics, metric)

		return true // keep iterating
	})

	metrics[0].Description = MetricDesc{
		Help: fmt.Sprintf("%s Returns the health score of a tenant", metrics[0].Name),
		Type: fmt.Sprintf("%s gauge", metrics[0].Name),
	}

	return metrics
}

func (p aciAPI) faults() []Metric {
	data, err := p.connection.getByQuery("faults")
	if err != nil {
		log.Error("faults not supported", err)
		return nil
	}

	metrics := []Metric{}
	//result := gjson.Get(data, "imdata")
/*
	metric_name := "faults"
	crit := gjson.Get(result.Raw, "0.faultCountsWithDetails.attributes.crit")
	metric := Metric{}
	metric.Name = metric_name
	metric.Labels = make(map[string]string)
	metric.Labels["severity"] = "crit"
	metric.Value = p.toFloat(crit.Str)

	metric.Description = MetricDesc{
		Help: fmt.Sprintf("%s Returns the total number of faults", metric.Name),
		Type: fmt.Sprintf("%s gauge", metric.Name),
	}
	metrics = append(metrics, metric)

	maj := gjson.Get(result.Raw, "0.faultCountsWithDetails.attributes.maj")
	metric = Metric{}
	metric.Name = metric_name
	metric.Labels = make(map[string]string)
	metric.Labels["severity"] = "maj"
	metric.Value = p.toFloat(maj.Str)
	metrics = append(metrics, metric)

	minor := gjson.Get(result.String(), "0.faultCountsWithDetails.attributes.minor")
	metric = Metric{}
	metric.Name = metric_name
	metric.Labels = make(map[string]string)
	metric.Labels["severity"] = "minor"
	metric.Value = p.toFloat(minor.Str)
	metrics = append(metrics, metric)

	warn := gjson.Get(result.String(), "0.faultCountsWithDetails.attributes.warn")
	metric = Metric{}
	metric.Name = metric_name
	metric.Labels = make(map[string]string)
	metric.Labels["severity"] = "warn"
	metric.Value = p.toFloat(warn.Str)
	metrics = append(metrics, metric)
*/
	children := gjson.Get(data, "imdata.0.faultCountsWithDetails.children.#.faultTypeCounts")

	setDesc := true
	metric_name := "faults"
	children.ForEach(func(key, value gjson.Result) bool {

		metric := Metric{}
		metric.Name = metric_name
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "crit"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.crit").Str)
		if setDesc {
			metric.Description = MetricDesc{
				Help: fmt.Sprintf("%s Returns the total number of faults by type", metric.Name),
				Type: fmt.Sprintf("%s gauge", metric.Name),
			}
			setDesc = false
		}
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Name = metric_name
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "maj"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.maj").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Name = metric_name
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "minor"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.minor").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Name = metric_name
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "warn"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.warn").Str)
		metrics = append(metrics, metric)

		return true // keep iterating
	})

	setDescAcked := true

	metric_name = "faults_acked"
	children.ForEach(func(key, value gjson.Result) bool {

		metric := Metric{}
		metric.Name = metric_name
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "crit"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.critAcked").Str)
		if setDescAcked {
			metric.Description = MetricDesc{
				Help: fmt.Sprintf("%s Returns the total number of acknowladged faults by type", metric.Name),
				Type: fmt.Sprintf("%s gauge", metric.Name),
			}
			setDescAcked = false
		}

		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Name = metric_name
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "maj"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.majAcked").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Name = metric_name
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "minor"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.minorAcked").Str)
		metrics = append(metrics, metric)

		metric = Metric{}
		metric.Name = metric_name
		metric.Labels = make(map[string]string)
		metric.Labels["type"] = gjson.Get(value.String(), "attributes.type").Str
		metric.Labels["severity"] = "warn"
		metric.Value = p.toFloat(gjson.Get(value.String(), "attributes.warnAcked").Str)
		metrics = append(metrics, metric)

		return true // keep iterating
	})

	return metrics
}

func (p aciAPI) infraNodeInfo() []Metric {
	data, err := p.connection.getByQuery("infra_node_health")
	if err != nil {
		log.Error("infra_node_health not supported", err)
		return nil
	}

	metrics := []Metric{}
	result := gjson.Get(data, "imdata")

	result.ForEach(func(key, value gjson.Result) bool {

		metric := Metric{}

		metric.Name = "infra_node_info"

		metric.Labels = make(map[string]string)
		metric.Labels["name"] = gjson.Get(value.String(), "infraWiNode.attributes.nodeName").Str
		metric.Labels["address"] = gjson.Get(value.String(), "infraWiNode.attributes.addr").Str
		metric.Labels["health"] = gjson.Get(value.String(), "infraWiNode.attributes.health").Str
		metric.Labels["apicmode"] = gjson.Get(value.String(), "infraWiNode.attributes.apicMode").Str
		metric.Labels["adminstatus"] = gjson.Get(value.String(), "infraWiNode.attributes.adminSt").Str
		metric.Labels["operstatus"] = gjson.Get(value.String(), "infraWiNode.attributes.operSt").Str
		metric.Labels["failoverStatus"] = gjson.Get(value.String(), "infraWiNode.attributes.failoverStatus").Str
		metric.Labels["podid"] = gjson.Get(value.String(), "infraWiNode.attributes.podId").Str

		metric.Value = 1.0

		metrics = append(metrics, metric)

		return true
	})

	metrics[0].Description = MetricDesc{
		Help: fmt.Sprintf("%s Returns the info of the infrastructure apic node", metrics[0].Name),
		Type: fmt.Sprintf("%s counter", metrics[0].Name),
	}

	return metrics
}

func (p aciAPI) getFabricName() string {
	data, err := p.connection.getByQuery("fabric_name")
	if err != nil {
		log.Error("fabric_health not supported", err)
		return ""
	}

	return gjson.Get(data, "imdata.0.infraCont.attributes.fbDmNm").Str

}

func (p aciAPI) toRatio(value string) float64 {
	rate, _ := strconv.ParseFloat(value, 64)
	return rate / 100.0
}

func (p aciAPI) toFloat(value string) float64 {
	rate, _ := strconv.ParseFloat(value, 64)
	return rate
}
