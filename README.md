aci-exporter
------------

> This project is still in alpha

# Overview
The aci-exporter provide metrics from an Cisco ACI fabric by using api calls to
an ACPI controller(s).

Most metrics are based on the health scoring provided by ACI. This includes:

- Node health of spine and leafs 
- Fabric health
- Tenant health
- EPG health (TODO)

It also include metrics of number of faults and severity, labeled by type of
fault like operational and configuration.

# Configuration

> For configuration please see `example-config.yml`

All attributes in the configuration has default values, except for the profile section.
A profile include the information need specific to one or many ACI fabrics, like authentication.


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

TODO
