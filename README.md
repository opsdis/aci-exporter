aci-exporter - An Cisco ACI Prometheus exporter
------------
[![published](https://static.production.devnetcloud.com/codeexchange/assets/images/devnet-published.svg)](https://developer.cisco.com/codeexchange/github/repo/opsdis/aci-exporter)

# Overview
The aci-exporter provide metrics from a Cisco ACI fabric by using the ACI Rest API against ACPI controller(s).

The exporter can return data both in the [Prometheus](https://prometheus.io/) and the 
[Openmetrics](https://openmetrics.io/) (v1) exposition format. 

The metrics that are exported is configured by definitions of a query. The query can be of any supported ACI class.

# How to configure queries
 
The exporter provides three types of query configuration:

- Class queries - one query, many metrics - These are applicable where one query can result in multiple metric names 
sharing the same labels. 
A good example is queries on interfaces, ethpmPhysIf, that results in metrics for speed, state, etc.  

- Group class queries - multiple queries, on metric - These are applicable when multiple queries result in a single 
metrics name but with configured, common and uniq labels. 
Example of this is the metric `health`, where all the different objects health require different queries, 
but they are all health. So instead of xyz_health it becomes health and some label with value xyz.
 
- Compound queries - multiple queries, on metric and fixed labels - These are applicable where multiple queries result 
in single metric name with configured labels. This is typical when counting different entities with 
`?rsp-subtree-include=count` since no labels are returned that can be used for labels.

There also some so called built-in queries. These are hard coded queries.
 
> Example of queries can be found in the `example-config.yaml` file. 
> Make sure you understand the ACI api before changing or creating new ones.


## Class queries
Class queries can be done against the different ACI classes. For a single query multiple metrics can be collected. 
All metrics will share the same labels.  

Example of queries are:

- Node health of spine and leafs 
- Fabric health
- Tenant health
- Interface state

### Labels
Labels extraction is done by using regexp on one or more property from the json response using named expression.
In the below example we use the `topSystem.attributes.dn` property and parse it with the regexp 
`^topology/pod-(?P<podid>[1-9][0-9]*)/node-(?P<nodeid>[1-9][0-9]*)/sys` that will return label values for the label 
names `podid` and `nodid`. The property `topSystem.attributes.state` will return a label name `state` matching the
whole property value.

```
    labels:
      - property_name: topSystem.attributes.dn
        regex: "^topology/pod-(?P<podid>[1-9][0-9]*)/node-(?P<nodeid>[1-9][0-9]*)/sys"
      - property_name: topSystem.attributes.state
        regex: "^(?P<state>.*)"
```

## Group class queries
Group queries group a number of class queries under a single metrics name, unit, help and type. Both individual 
and common labels are supported.

## Compound queries 
The compound queries is used when a single metrics is "compounded" by different queries. In the 
`example-config.yaml` file is an example where the number of spines, leafs and controllers are counted. They will
all be of the metric `nodes` but require 3 different queries. Since no labels can be extracted from the response 
the label name and label value is configured.

The result is:
```
# HELP nodes Returns the current count of nodes
# TYPE nodes gauge
aci_nodes{aci="ACI Fabric1",fabric="cisco_sandbox",node="spine"} 3
aci_nodes{aci="ACI Fabric1",fabric="cisco_sandbox",node="leaf"} 7
aci_nodes{aci="ACI Fabric1",fabric="cisco_sandbox",node="controller"} 1
```

## Built-in queries  
The export has some standard metric "built-in". These are:
- `faults`, labeled by severity and type of fault, like operational, configuration and environment faults.

# Parsing metrics and labels
A metrics and label value is some part of the json returned by a query. The key for metrics value in all query types is
`value_name`.
The aci-exporter use [Gjson](https://github.com/tidwall/gjson) for parsing the metrics value and the label value. 
To get the state metrics value for the class ethpmPhysIf the parsing expression would be `ethpmPhysIf.attributes.operSt`. 

There are one additions to the Gjson syntax, and it's related to arrays returning objects.

The first example is for an array returning different kind of objects. A good example from the APIC api is the returning 
of children, like the following query:

    /api/class/fvAEPg.json?rsp-subtree-include=health,required
 
This will return a child structure like this:

```
"children": [
          {
            "healthNodeInst": {
              "attributes": {
                "childAction": "deleteNonPresent",
                "chng": "400",
                "cur": "100",
                "isExisting": "no",
                "lcOwn": "local",
                "maxSev": "cleared",
                "modTs": "never",
                "nodeId": "101",
                "podId": "1",
                "prev": "20",
                "rn": "nodehealth-101",
                "status": "",
                "twScore": "100",
                "updTs": "2020-08-11T17:41:24.154+02:00",
                "weight": "1"
              }
            }
          },
          {
            "healthNodeInst": {
              "attributes": {
                "childAction": "deleteNonPresent",
                "chng": "400",
                "cur": "100",
                "isExisting": "no",
                "lcOwn": "local",
                "maxSev": "cleared",
                "modTs": "never",
                "nodeId": "102",
                "podId": "1",
                "prev": "20",
                "rn": "nodehealth-102",
                "status": "",
                "twScore": "100",
                "updTs": "2020-08-11T17:41:31.400+02:00",
                "weight": "1"
              }
            }
          },
          {
            "healthInst": {
              "attributes": {
                "childAction": "",
                "chng": "400",
                "cur": "100",
                "maxSev": "cleared",
                "modTs": "never",
                "prev": "20",
                "rn": "health",
                "status": "",
                "twScore": "100",
                "updTs": "2020-08-11T17:41:32.306+02:00"
              }
            }
          }
        ]
```

 From the output, the health of the specific fvAEPg is defined in the third entry in the array, `healthInst`, and the 
 other entries are related to the ACI nodes of the application endpoint group. If we just want to to get the result of 
 `cur` from the `healthInst` we express the path as:
 
    fvAEPg.children.[healthInst].attributes.cur
 
 This defines that in the `children` array we want to extract data from the `healthInst` entry. 
 So the addition is to use the left and right bracket to define that its an array, and between the brackets is the 
 regular expression of the entry.
 
 If multiple instances of `healthInst`
 existed only the first found will be used. 
  
> This currently only work with one level of arrays.

If want to iterate over all children the expression would be `.[.*].`. 
This is useful when a class query return a number of different objects. 
Example of this would be for the class `ethpmDOMStats` using the query `?rsp-subtree=children`. This will return a number
of children objets, and for all the children classes we like to get the `hiAlarm` metric.
```
value_name: ethpmDOMStats.children.[.*].attributes.hiAlarm
```
The `.*` will be substituted with the children class name. So that means it can also be used as a label like:
```
    labels:
      # this will be the child class name
      - property_name: ethpmDOMStats.children.[.*]
        regex: "^(?P<class>.*)"
      # this will be the lanes of the child class
      - property_name: ethpmDOMStats.children.[.*].attributes.lanes
        regex: "^(?P<laneid>.*)"
```  

The full query configuration
```
  ethpmdomstats:
    class_name: ethpmDOMStats
    query_parameter: '?rsp-subtree=children'
    metrics:
      - name: ethpmDOMStats_hiAlarm
        value_name: ethpmDOMStats.children.[.*].attributes.hiAlarm
        type: "gauge"
        help: "Returns hiAlarm"
    labels:
      - property_name: ethpmDOMStats.attributes.dn
        regex: "^topology/pod-(?P<podid>[1-9][0-9]*)/node-(?P<nodeid>[1-9][0-9]*)/sys/phys-\\[(?P<interface>[^\\]]+)\\]/"
      - property_name: ethpmDOMStats.children.[.*]
        regex: "^(?P<class>.*)"
      - property_name: ethpmDOMStats.children.[.*].attributes.lanes
        regex: "^(?P<laneid>.*)"

```
The query will return a prometheus metrics response like this, where the `class` label is set to the name of each child 
class name:
```
curl -s 'http://localhost:9643/probe?target=XYZ&queries=ethpmdomstats'
# HELP ethpmDOMStats_hiAlarm Returns hiAlarm
# TYPE ethpmDOMStats_hiAlarm gauge
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMRxPwrStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="101",podid="1"} 0.999912
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTxPwrStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="101",podid="1"} 2.50005
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMCurrentStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="101",podid="1"} 90.000008
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTempStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="101",podid="1"} 90
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMVoltStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="101",podid="1"} 3.6
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMRxPwrStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="101",podid="1"} 3.0103
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTxPwrStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="101",podid="1"} 1.291741
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMCurrentStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="101",podid="1"} 100.000008
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTempStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="101",podid="1"} 90
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMVoltStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="101",podid="1"} 3.63
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMRxPwrStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="101",podid="1"} 3.000082
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTxPwrStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="101",podid="1"} 7.000024
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMCurrentStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="101",podid="1"} 110.000008
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTempStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="101",podid="1"} 100
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMVoltStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="101",podid="1"} 3.6
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMRxPwrStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="102",podid="1"} 3.000082
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTxPwrStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="102",podid="1"} 7.000024
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMCurrentStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="102",podid="1"} 110.000008
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTempStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="102",podid="1"} 100
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMVoltStats",fabric="miradot",interface="eth1/1",laneid="1",nodeid="102",podid="1"} 3.6
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMRxPwrStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="102",podid="1"} 3.0103
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTxPwrStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="102",podid="1"} 1.291741
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMCurrentStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="102",podid="1"} 100.000008
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTempStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="102",podid="1"} 90
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMVoltStats",fabric="miradot",interface="eth1/2",laneid="1",nodeid="102",podid="1"} 3.63
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMRxPwrStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="102",podid="1"} 3.000082
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTxPwrStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="102",podid="1"} 7.000024
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMCurrentStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="102",podid="1"} 110.000008
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTempStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="102",podid="1"} 100
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMVoltStats",fabric="miradot",interface="eth1/48",laneid="1",nodeid="102",podid="1"} 3.6
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMRxPwrStats",fabric="miradot",interface="eth1/3",laneid="1",nodeid="102",podid="1"} 1.000257
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTxPwrStats",fabric="miradot",interface="eth1/3",laneid="1",nodeid="102",podid="1"} -1.999707
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMCurrentStats",fabric="miradot",interface="eth1/3",laneid="1",nodeid="102",podid="1"} 17
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMTempStats",fabric="miradot",interface="eth1/3",laneid="1",nodeid="102",podid="1"} 95
aci_ethpmDOMStats_hiAlarm{aci="VBDC-Fabric1",class="ethpmDOMVoltStats",fabric="miradot",interface="eth1/3",laneid="1",nodeid="102",podid="1"} 3.9
# HELP scrape_duration_seconds The duration, in seconds, of the last scrape of the fabric
# TYPE scrape_duration_seconds gauge
aci_scrape_duration_seconds{aci="VBDC-Fabric1",fabric="miradot"} 0.116875019

```
# Metrics transformations
In the query configuration the attribute `value_name` define the entity in the response that will be used as a value 
for the metrics. Prometheus can only manage metrics value of the type float, so all values must be transformed to 
a float. The export automatically handle this for values of the type:
- Float
- Integers
- Time stamp in the format of rfc 3339, will be transformed to a UNIX timestamp in seconds

Some metrics from ACI api is returned as strings, and needs to be transformed to a float. 
This can be done with a `value_transform`. E.g. the speed of an interface:
```
        value_transform:
          'unknown':            0
          '100M':       100000000
          '1G':        1000000000
          '10G':      10000000000
          '25G':      25000000000
          '40G':      40000000000
          '100G':    100000000000

```
Or the state of an interface:
```
        value_transform:
           'unknown': 0
           'down': 1
           'up': 2
           'link-up': 3
```

It is also possible to recalculate a metrics value using `value_calculation`. Like present percentage in decimal: 
```
value_calculation: "value / 100"
```

>The `value` is the named variable for the metric value.

# Labels
Since all queries are configurable metrics name and label definitions are up to the person doing the configuration.
The recommendation is to follow the best practices for [Promethues](https://prometheus.io/docs/practices/naming/).

To make labels useful in the ACI context we think a good recommendation is to use the structure and naming in the 
ACI class model. 
Use the label name `class` when we relate to names from the class model like `fvTenant`and 
`fvBD`, where `fv` is the package and `Tenant` is the class name. 
If we want to have a label name for a specific instance of the class we use the class name in lower case like `tenant` 
and `bd`, like `tenant="opsdis"`   

For different identities like pods and nodes we use the type+id like `podid` and `nodeid`. So for node 201 the label is
`nodeid="201"`.


# Default labels
The aci-exporter will attach the following labels to all metrics

- `aci` the name of the ACI. This is done by an API call.
- `fabric` the name of the configuration.

# Configuration

> For configuration options please see the `example-config.yml` file.

All attributes in the configuration has default values, except for the fabric and the different query sections.
A fabric profile include the information specific to an ACI fabrics, like authentication and apic(s) url.

> The user need to have admin read-only rights in the domain `All` to allow all kinds of queries.

If there is multiple apic urls configured the exporter will use the first apic it can login to starting with the first
in the list.

All configuration properties can be set by using environment variables. The prefix is `ACI_EXPORTER_` and property 
must be in uppercase. So to set the property `port` with an environment variable `ACI_EXPORTER_PORT=7121`. 

# Openmetrics format
The exporter support [openmetrics](https://openmetrics.io/) format. This is done by adding the following accept header to the request:

    "Accept: application/openmetrics-text"

The configuration property `openmetrics` set to `true` will result in that all request will have an openmetrics 
response independent of the above header.

# Error handling
Any critical errors between the exporter and the apic controller will return 503. This is currently related to login 
failure and failure to get the fabric name.
 
There may be situations where the export will have failure against some api calls that collect data, due to timeout or
faulty configuration. They will just not be part of the metric output.

Any access failures to apic[s] are written to the log.

# Installation

## Build 
    go build -o build/aci-exporter  *.go

## Run
By default the exporter will look for a configuration file called `config.yaml`. The directory search paths are:

- Current directory
- $HOME/.aci-exporter
- usr/local/etc/aci-exporter
- etc/aci-exporter

```
    ./build/aci-exporter
```

To run against the Cisco ACI sandbox:
```
    ./build/aci-exporter -config example-config.yaml
```
> Make sure that the sandbox url and authentication is correct. Check out Cisco sandboxes on 
> https://devnetsandbox.cisco.com/RM/Topology - "ACI Simulator AlwaysOn"

## Test
To test against the Cisco ACI sandbox:

```
    curl -s 'http://localhost:9643/probe?target=cisco_sandbox'
```
    
The target is a named fabric in the configuration file.

There is also possible to run a limited number of queries by using the query parameter `queries`.
This should be a comma separated list of the query names in the config file. It may also contain built-in query names.

```
    curl -s 'http://localhost:9643/probe?target=cisco_sandbox&queries=node_health,faults'
```

# Internal metrics
Internal metrics is exposed in Prometheus exposition format on the endpoint `/metrics`.
To get the metrics in openmetrics format use the header `Accept: application/openmetrics-text`

# Prometheus configuration

Please see the example file prometheus/prometheus.yml.

# Docker 
The aci-export can be build and run as a docker container. 

    docker build . -t aci-exporter

To run as docker use environment variables to define configuration.

    export ACI_EXPORTER_CONFIG=config.yaml;docker run -p 9643:9643  aci-exporter

# Acknowledgements

Thanks to https://github.com/RavuAlHemio/prometheus_aci_exporter for the inspiration of the configuration of queries. 
Please check out that project especially if you like to contribute to a Python project.   

# License
This work is licensed under the GNU GENERAL PUBLIC LICENSE Version 3.
 
