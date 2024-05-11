package main

import (
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

type Discovery struct {
	Fabric      string
	LabelsKeys  []string
	TargetField string
}

func (d Discovery) DoDiscovery() ([]ServiceDiscovery, error) {
	class := "topSystem"
	query := ""
	data := cliQuery(&d.Fabric, &class, &query)

	var topSystems []TopSystem
	result := gjson.Get(data, "imdata")
	result.ForEach(func(key, value gjson.Result) bool {
		topSystemJson := gjson.Get(value.Raw, "topSystem.attributes").Raw
		topSystem := &TopSystem{}
		json.Unmarshal([]byte(topSystemJson), topSystem)
		topSystems = append(topSystems, *topSystem)
		return true
	})

	return d.parseToDiscoveryFormat(topSystems)
}

func (d Discovery) parseToDiscoveryFormat(topSystems []TopSystem) ([]ServiceDiscovery, error) {
	var serviceDiscovery []ServiceDiscovery
	for _, topSystem := range topSystems {
		sd := &ServiceDiscovery{}
		sd.Labels = make(map[string]string)
		targetValue, err := d.getField(&topSystem, d.TargetField)
		if err != nil {
			return serviceDiscovery, err
		}

		sd.Targets = append(sd.Targets, targetValue)
		for _, labelName := range d.LabelsKeys {
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
		}).Error("field %s does not exist within the provided item")
		return "", fmt.Errorf("field %s does not exist within the provided item", fieldName)
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
}
