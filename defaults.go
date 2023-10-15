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
	"strings"

	"github.com/spf13/viper"
)

const (
	// ExporterName name of the exporter
	ExporterName = "aci-exporter"

	// MetricsPrefix the prefix for all internal metrics
	MetricsPrefix = "aci_exporter_"
)

// ExporterNameAsEnv return the ExportName as an env prefix
func ExporterNameAsEnv() string {
	return strings.ToUpper(strings.ReplaceAll(ExporterName, "-", "_"))
}

// SetDefaultValues define all default values
func SetDefaultValues() {

	// If set as env vars use the ExporterName as prefix like ACI_EXPORTER_PORT for the port var
	viper.SetEnvPrefix(ExporterNameAsEnv())

	// All fields with . will be replaced with _ for ENV vars
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// aci-exporter
	viper.SetDefault("port", 9643)
	viper.BindEnv("port")
	viper.SetDefault("logfile", "")
	viper.BindEnv("logfile")
	viper.SetDefault("logformat", "json")
	viper.BindEnv("logformat")
	viper.SetDefault("config", "config")
	viper.BindEnv("config")
	viper.SetDefault("prefix", "aci_")
	viper.BindEnv("prefix")
	viper.SetDefault("pport", "localhost:6060")
	viper.BindEnv("pport")

	// If set to true response will always be in openmetrics format
	viper.SetDefault("openmetrics", false)
	viper.BindEnv("openmetrics")

	// HTTPCLient
	viper.SetDefault("HTTPClient.timeout", 0)
	viper.BindEnv("HTTPClient.timeout")

	viper.SetDefault("HTTPClient.keepalive", 15)
	viper.BindEnv("HTTPClient.keepalive")

	viper.SetDefault("HTTPClient.pagesize", 1000)
	viper.BindEnv("HTTPClient.pagesize")

	viper.SetDefault("HTTPClient.parallel_paging", true)
	viper.BindEnv("HTTPClient.parallel_paging")

	// This is currently not used
	viper.SetDefault("HTTPClient.tlshandshaketimeout", 10)
	viper.BindEnv("HTTPClient.tlshandshaketimeout")

	viper.SetDefault("HTTPClient.insecureHTTPS", true)
	viper.BindEnv("HTTPClient.insecureHTTPS")

	// HTTPServer
	viper.SetDefault("httpserver.read_timeout", 0)
	viper.BindEnv("httpserver.read_timeout")

	viper.SetDefault("httpserver.write_timeout", 0)
	viper.BindEnv("httpserver.write_timeout")

}
