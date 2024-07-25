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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type ServiceDiscovery struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

func NewServiceDiscovery() ServiceDiscovery {
	return ServiceDiscovery{
		Targets: make([]string, 0),
		Labels:  make(map[string]string),
	}
}

type DiscoveryConfiguration struct {
	LabelsKeys   []string `mapstructure:"labels"`
	TargetFields []string `mapstructure:"target_fields"`
	TargetFormat string   `mapstructure:"target_format"`
}

type Discovery struct {
	Fabric  string
	Fabrics map[string]*Fabric
}

func (d Discovery) DoDiscovery(ctx context.Context) ([]ServiceDiscovery, error) {

	var serviceDiscoveries []ServiceDiscovery
	var topSystems []TopSystem
	if d.Fabric != "" {
		aci, err := d.getInfraCont(ctx, d.Fabric)
		if err != nil {
			return serviceDiscoveries, err
		}
		topSystems = d.getTopSystem(ctx, d.Fabric)
		sds, _ := d.parseToDiscoveryFormat(d.Fabric, topSystems)
		serviceDiscoveries = append(serviceDiscoveries, sds...)
		// Add the fabric as a target
		fabricSd := NewServiceDiscovery()
		fabricSd.Targets = append(fabricSd.Targets, d.Fabric)
		fabricSd.Labels["__meta_role"] = "aci_exporter_fabric"
		fabricSd.Labels["__meta_fabricDomain"] = aci
		serviceDiscoveries = append(serviceDiscoveries, fabricSd)
	} else {
		for key := range d.Fabrics {
			aci, err := d.getInfraCont(ctx, key)
			if err != nil {
				continue
			}
			topSystems := d.getTopSystem(ctx, key)
			sds, _ := d.parseToDiscoveryFormat(key, topSystems)
			serviceDiscoveries = append(serviceDiscoveries, sds...)
			fabricSd := NewServiceDiscovery()
			fabricSd.Targets = append(fabricSd.Targets, key)
			fabricSd.Labels["__meta_role"] = "aci_exporter_fabric"
			fabricSd.Labels["__meta_fabricDomain"] = aci
			serviceDiscoveries = append(serviceDiscoveries, fabricSd)

		}
	}

	return serviceDiscoveries, nil
}

// p.connection.GetByClassQuery("infraCont", "?query-target=self")
func (d Discovery) getInfraCont(ctx context.Context, fabricName string) (string, error) {
	class := "infraCont"
	query := "?query-target=self"
	data, err := cliQuery(ctx, &fabricName, &class, &query)

	if err != nil {
		log.WithFields(log.Fields{
			"function": "discovery",
			"class":    class,
			"fabric":   fabricName,
		}).Error(err)
		return "", err
	}

	if len(gjson.Get(data, "imdata.#.infraCont.attributes.fbDmNm").Array()) == 0 {
		err = fmt.Errorf("could not determine ACI name, no data returned from APIC")
		log.WithFields(log.Fields{
			"function": "discovery",
			"class":    class,
			"fabric":   fabricName,
		}).Error(err)
		return "", err
	}

	aciName := gjson.Get(data, "imdata.#.infraCont.attributes.fbDmNm").Array()[0].Str

	if aciName != "" {
		return aciName, nil
	}

	err = fmt.Errorf("could not determine ACI name")
	log.WithFields(log.Fields{
		"function": "discovery",
		"class":    class,
		"fabric":   fabricName,
	}).Error(err)
	return "", err
}

func (d Discovery) getTopSystem(ctx context.Context, fabricName string) []TopSystem {
	class := "topSystem"
	query := ""
	data, err := cliQuery(ctx, &fabricName, &class, &query)
	if err != nil {
		log.WithFields(log.Fields{
			"function": "discovery",
			"class":    class,
			"fabric":   fabricName,
		}).Error(err)
		return nil
	}

	var topSystems []TopSystem
	result := gjson.Get(data, "imdata")
	result.ForEach(func(key, value gjson.Result) bool {
		topSystemJson := gjson.Get(value.Raw, "topSystem.attributes").Raw
		topSystem := &TopSystem{}
		topSystem.ACIExporterFabric = fabricName
		_ = json.Unmarshal([]byte(topSystemJson), topSystem)
		topSystems = append(topSystems, *topSystem)
		return true
	})

	return topSystems
}

func (d Discovery) parseToDiscoveryFormat(fabricName string, topSystems []TopSystem) ([]ServiceDiscovery, error) {
	var serviceDiscovery []ServiceDiscovery
	for _, topSystem := range topSystems {
		sd := &ServiceDiscovery{}
		targetValue := make([]interface{}, len(d.Fabrics[fabricName].DiscoveryConfig.TargetFields))
		for i, field := range d.Fabrics[fabricName].DiscoveryConfig.TargetFields {
			val, err := d.getField(&topSystem, field)
			targetValue[i] = val
			if err != nil {
				return serviceDiscovery, err
			}
		}

		sd.Targets = append(sd.Targets, fmt.Sprintf(d.Fabrics[fabricName].DiscoveryConfig.TargetFormat, targetValue...))

		sd.Labels = make(map[string]string)
		sd.Labels[fmt.Sprintf("__meta_%s", "aci_exporter_fabric")] = topSystem.ACIExporterFabric

		for _, labelName := range d.Fabrics[fabricName].DiscoveryConfig.LabelsKeys {
			labelValue, err := d.getField(&topSystem, labelName)
			if err != nil {
				return serviceDiscovery, err
			}
			sd.Labels[fmt.Sprintf("__meta_%s", labelName)] = labelValue
		}
		serviceDiscovery = append(serviceDiscovery, *sd)
	}
	return serviceDiscovery, nil
}

func (d Discovery) getField(item interface{}, fieldName string) (string, error) {
	v := reflect.ValueOf(item).Elem()
	if !v.CanAddr() {
		log.WithFields(log.Fields{
			"function":  "discovery",
			"fabric":    d.Fabric,
			"fieldName": fieldName,
		}).Error("cannot assign to the item passed, item must be a pointer in order to assign")
		return "", fmt.Errorf("cannot assign to the item passed, item must be a pointer in order to assign")
	}
	// It's possible we can cache this, which is why precompute all these ahead of time.
	findJsonName := func(t reflect.StructTag) (string, error) {
		if jt, ok := t.Lookup("json"); ok {
			return strings.Split(jt, ",")[0], nil
		}
		log.WithFields(log.Fields{
			"function":  "discovery",
			"fabric":    d.Fabric,
			"fieldName": fieldName,
		}).Error("tag provided does not define a json tag")
		return "", fmt.Errorf("tag provided does not define a json tag")
	}
	fieldNames := map[string]int{}
	for i := 0; i < v.NumField(); i++ {
		typeField := v.Type().Field(i)
		tag := typeField.Tag
		jname, _ := findJsonName(tag)
		fieldNames[jname] = i
	}

	fieldNum, ok := fieldNames[fieldName]
	if !ok {
		log.WithFields(log.Fields{
			"function":  "discovery",
			"fabric":    d.Fabric,
			"fieldName": fieldName,
		}).Error("field does not exist within the provided item")
		return "", fmt.Errorf("field does not exist within the provided item")
	}
	fieldVal := v.Field(fieldNum)
	return fieldVal.String(), nil
}

type TopSystem struct {
	Address                 string `json:"address"`
	BootstrapState          string `json:"bootstrapState"`
	ChildAction             string `json:"childAction"`
	ClusterTimeDiff         string `json:"clusterTimeDiff"`
	ConfigIssues            string `json:"configIssues"`
	ControlPlaneMTU         string `json:"controlPlaneMTU"`
	CurrentTime             string `json:"currentTime"`
	Dn                      string `json:"dn"`
	EnforceSubnetCheck      string `json:"enforceSubnetCheck"`
	EtepAddr                string `json:"etepAddr"`
	FabricDomain            string `json:"fabricDomain"`
	FabricID                string `json:"fabricId"`
	FabricMAC               string `json:"fabricMAC"`
	ID                      string `json:"id"`
	InbMgmtAddr             string `json:"inbMgmtAddr"`
	InbMgmtAddr6            string `json:"inbMgmtAddr6"`
	InbMgmtAddr6Mask        string `json:"inbMgmtAddr6Mask"`
	InbMgmtAddrMask         string `json:"inbMgmtAddrMask"`
	InbMgmtGateway          string `json:"inbMgmtGateway"`
	InbMgmtGateway6         string `json:"inbMgmtGateway6"`
	LastRebootTime          string `json:"lastRebootTime"`
	LastResetReason         string `json:"lastResetReason"`
	LcOwn                   string `json:"lcOwn"`
	ModTs                   string `json:"modTs"`
	Mode                    string `json:"mode"`
	MonPolDn                string `json:"monPolDn"`
	Name                    string `json:"name"`
	NameAlias               string `json:"nameAlias"`
	NodeType                string `json:"nodeType"`
	OobMgmtAddr             string `json:"oobMgmtAddr"`
	OobMgmtAddr6            string `json:"oobMgmtAddr6"`
	OobMgmtAddr6Mask        string `json:"oobMgmtAddr6Mask"`
	OobMgmtAddrMask         string `json:"oobMgmtAddrMask"`
	OobMgmtGateway          string `json:"oobMgmtGateway"`
	OobMgmtGateway6         string `json:"oobMgmtGateway6"`
	PodID                   string `json:"podId"`
	RemoteNetworkID         string `json:"remoteNetworkId"`
	RemoteNode              string `json:"remoteNode"`
	RlOperPodID             string `json:"rlOperPodId"`
	RlRoutableMode          string `json:"rlRoutableMode"`
	RldirectMode            string `json:"rldirectMode"`
	Role                    string `json:"role"`
	Serial                  string `json:"serial"`
	ServerType              string `json:"serverType"`
	SiteID                  string `json:"siteId"`
	State                   string `json:"state"`
	Status                  string `json:"status"`
	SystemUpTime            string `json:"systemUpTime"`
	TepPool                 string `json:"tepPool"`
	UnicastXrEpLearnDisable string `json:"unicastXrEpLearnDisable"`
	Version                 string `json:"version"`
	VirtualMode             string `json:"virtualMode"`
	ACIExporterFabric       string `json:"aci_exporter_fabric"`
}
