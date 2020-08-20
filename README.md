aci-exporter - An Cisco ACI Prometheus exporter
------------

> This project is still in alpha and everything may change. If this exporter is useful or might be, please share
> your experience and/or improvements.  

# Overview
The aci-exporter provide metrics from a Cisco ACI fabric by using the ACI Rest API against ACPI controller(s).

The exporter can return data both in the [Prometheus](https://prometheus.io/) and the 
[Openmetrics](https://openmetrics.io/) (v1) exposition format. 

The metrics that are exported is configured by definitions of a query. The query can be of any supported ACI class.

# How to configure queries
 
The exporter provides two types of query configuration:

- Class queries - These are applicable where one query can result in multiple metric names sharing the same labels. 
- Compound queries - These are applicable where multiple queries result in single metric name with configured labels. 
This is typical when counting different entities.

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
faulty configuration. They will just not be part of the output.

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

# Acknowledgements

Thanks to https://github.com/RavuAlHemio/prometheus_aci_exporter for the inspiration of the configuration of queries. 
Please check out that project especially if you like to contribute to a Python project.   

# License
This work is licensed under the GNU GENERAL PUBLIC LICENSE Version 3.
 
# Todo 
- Exclude configured queries for a specific fabric.

