aci-exporter
------------

> This project is still in alpha and everything may change 

# Overview
The aci-exporter provide metrics from a Cisco ACI fabric by using api calls to an ACPI controller(s).

The export has some standard metric that is "built-in", but all metrics related to a class query can be configured. 
For configuration of metrics based on class queries please look at `example-config.yaml` 

The built-in metrics are based on queries that are not related to class queries like:
TODO

In the `exemple-config.yaml` are different examples of metrics queries like:

- Node health of spine and leafs 
- Fabric health
- Tenant health
- EPG health (TODO)

It also includes metrics of number of faults and severity, labeled by type of
fault like operational and configuration.

> Make sure you understand the ACI api before changing or creating new ones.

# Configuration

> For configuration please see `example-config.yml`

All attributes in the configuration has default values, except for the profile section.
A profile include the information need specific to one or many ACI fabrics, like authentication.

# Openmetrics format
The exporter support openmetrics format. This is done by adding the following
accept header to the request:

    "Accept: application/openmetrics-text"

The configuration property `openmetrics` set to `true` will result in that all
reguest will have an openmetrics response.

# Installation

## Build 
    go build -o build/aci-exporter  *.go

## Run
    .build/aci-exporter 
    
## Test
    curl -s 'http://localhost:8080/probe?target=https://apichost&profile=profile-fabric-01'
    
# Prometheus configuration

Please see file prometheus/prometheus.yml.

# Grafana dashboards
Todo 

# TODO
- Add support to access any healthy apic in the fabric
- Exclude configuration of the built in metrics
- Add fabric specific configuration queries