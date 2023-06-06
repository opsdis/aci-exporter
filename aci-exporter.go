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
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/ksuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	length     int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.statusCode == 0 {
		lrw.statusCode = http.StatusOK
	}
	n, err := lrw.ResponseWriter.Write(b)
	lrw.length += n
	return n, err
}

var version = "undefined"

func main() {

	flag.Usage = func() {
		fmt.Printf("Usage of %s:\n", ExporterName)
		fmt.Printf("Version %s\n", version)
		flag.PrintDefaults()
	}

	SetDefaultValues()

	flag.Int("p", viper.GetInt("port"), "The port to start on")
	logFile := flag.String("logfile", viper.GetString("logfile"), "Set log file, default stdout")
	logFormat := flag.String("logformat", viper.GetString("logformat"), "Set log format to text or json, default json")
	config := flag.String("config", viper.GetString("config"), "Set configuration file, default config.yaml")
	usage := flag.Bool("u", false, "Show usage")
	writeConfig := flag.Bool("default", false, "Write default config")

	cli := flag.Bool("cli", false, "Run single query")
	class := flag.String("class", viper.GetString("class"), "The class name - only cli")
	query := flag.String("query", viper.GetString("query"), "The query for the class - only cli")
	fabric := flag.String("fabric", viper.GetString("fabric"), "The fabric name - only cli")

	flag.Parse()

	log.SetFormatter(&log.JSONFormatter{})
	if *logFormat == "text" {
		log.SetFormatter(&log.TextFormatter{})
	}

	viper.SetConfigName(*config) // name of config file (without extension)
	viper.SetConfigType("yaml")  // REQUIRED if the config file does not have the extension in the name

	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME/.aci-exporter")
	viper.AddConfigPath("/usr/local/etc/aci-exporter")
	viper.AddConfigPath("/etc/aci-exporter")

	if *usage {
		flag.Usage()
		os.Exit(0)
	}

	if *cli {
		fmt.Printf("%s", cliQuery(fabric, class, query))

		os.Exit(0)
	}

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Println(err)
		}
		log.SetOutput(f)
	}

	if *writeConfig {
		err := viper.WriteConfigAs("./aci_exporter_default_config.yaml")
		if err != nil {
			log.Error("Can not write default config file - ", err)
		}
		os.Exit(0)
	}

	// Find and read the config file
	err := viper.ReadInConfig()
	if err != nil {
		log.Error("Configuration file not valid - ", err)
		os.Exit(1)
	}

	var classQueries = ClassQueries{}
	err = viper.UnmarshalKey("class_queries", &classQueries)
	if err != nil {
		log.Error("Unable to decode class_queries into struct - ", err)
		os.Exit(1)
	}

	var compoundClassQueries = CompoundClassQueries{}
	err = viper.UnmarshalKey("compound_queries", &compoundClassQueries)
	if err != nil {

		log.Error("Unable to decode compound_queries into struct - ", err)
		os.Exit(1)
	}

	var groupClassQueries = GroupClassQueries{}
	err = viper.UnmarshalKey("qroup_class_queries", &groupClassQueries)
	if err != nil {

		log.Error("Unable to decode compound_queries into struct - ", err)
		os.Exit(1)
	}
	allQueries := AllQueries{
		ClassQueries:         classQueries,
		CompoundClassQueries: compoundClassQueries,
		GroupClassQueries:    groupClassQueries,
	}

	handler := &HandlerInit{allQueries}

	// Create a Prometheus histogram for response time of the exporter
	responseTime := promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    MetricsPrefix + "request_duration_seconds",
		Help:    "Histogram of the time (in seconds) each request took to complete.",
		Buckets: []float64{0.050, 0.100, 0.200, 0.500, 0.800, 1.00, 2.000, 3.000},
	},
		[]string{"url", "status"},
	)

	// Setup handler for aci destinations
	http.Handle("/probe", logcall(promMonitor(http.HandlerFunc(handler.getMonitorMetrics), responseTime, "/probe")))
	http.Handle("/alive", logcall(promMonitor(http.HandlerFunc(alive), responseTime, "/alive")))

	// Setup handler for exporter metrics
	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))

	log.Info(fmt.Sprintf("%s starting on port %d", ExporterName, viper.GetInt("port")))
	log.Info(fmt.Sprintf("Read timeout %s, Write timeout %s", viper.GetDuration("httpserver.read_timeout")*time.Second, viper.GetDuration("httpserver.write_timeout")*time.Second))
	s := &http.Server{
		ReadTimeout:  viper.GetDuration("httpserver.read_timeout") * time.Second,
		WriteTimeout: viper.GetDuration("httpserver.write_timeout") * time.Second,
		Addr:         ":" + strconv.Itoa(viper.GetInt("port")),
	}
	log.Fatal(s.ListenAndServe())
}

func cliQuery(fabric *string, class *string, query *string) string {
	err := viper.ReadInConfig()
	if err != nil {
		log.Error("Configuration file not valid - ", err)
		os.Exit(1)
	}
	username := viper.GetString(fmt.Sprintf("fabrics.%s.username", *fabric))
	password := viper.GetString(fmt.Sprintf("fabrics.%s.password", *fabric))
	apicControllers := viper.GetStringSlice(fmt.Sprintf("fabrics.%s.apic", *fabric))

	fabricConfig := Fabric{Username: username, Password: password, Apic: apicControllers}
	ctx := context.TODO()
	con := *newAciConnction(ctx, fabricConfig)
	err = con.login()
	if err != nil {
		fmt.Printf("Login error %s", err)
		return ""
	}
	defer con.logout()
	var data string

	if string((*query)[0]) != "?" {
		data, err = con.getByClassQuery(*class, fmt.Sprintf("?%s", *query))
	} else {
		data, err = con.getByClassQuery(*class, *query)
	}

	if err != nil {
		fmt.Printf("Error %s", err)
	}
	return fmt.Sprintf("%s", data)
}

type HandlerInit struct {
	AllQueries AllQueries
}

func (h HandlerInit) getMonitorMetrics(w http.ResponseWriter, r *http.Request) {

	openmetrics := false
	// Check accept header for open metrics
	if r.Header.Get("Accept") == "application/openmetrics-text" || viper.GetBool("openmetrics") || viper.GetBool("metric_format.openmetrics") {
		openmetrics = true
	}

	fabric := r.URL.Query().Get("target")
	queries := r.URL.Query().Get("queries")

	// Check if a valid target
	if !viper.IsSet(fmt.Sprintf("fabrics.%s", fabric)) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Content-Length", "0")

		lrw := loggingResponseWriter{ResponseWriter: w}
		lrw.WriteHeader(404)
		return
	}

	username := viper.GetString(fmt.Sprintf("fabrics.%s.username", fabric))
	password := viper.GetString(fmt.Sprintf("fabrics.%s.password", fabric))
	apicControllers := viper.GetStringSlice(fmt.Sprintf("fabrics.%s.apic", fabric))

	fabricConfig := Fabric{Username: username, Password: password, Apic: apicControllers}

	ctx := r.Context()
	ctx = context.WithValue(ctx, "fabric", fabric)
	api := *newAciAPI(ctx, fabricConfig, h.AllQueries, queries)

	aciName, metrics, err := api.CollectMetrics()

	commonLabels := make(map[string]string)
	commonLabels["aci"] = aciName
	commonLabels["fabric"] = fabric

	metricsFormat := NewMetricFormat(openmetrics, viper.GetBool("metric_format.label_key_to_lower_case"),
		viper.GetBool("metric_format.label_key_to_snake_case"))
	var bodyText = Metrics2Prometheus(metrics, api.metricPrefix, commonLabels, metricsFormat)
	if openmetrics {
		w.Header().Set("Content-Type", "application/openmetrics-text; version=0.0.1; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(bodyText)))

	lrw := loggingResponseWriter{ResponseWriter: w}
	if bodyText == "" {
		lrw.WriteHeader(404)
	}
	if err != nil {
		lrw.WriteHeader(503)
	}
	w.Write([]byte(bodyText))
	return
}

func alive(w http.ResponseWriter, r *http.Request) {

	var alive = fmt.Sprintf("Alive!\n")
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(alive)))
	lrw := loggingResponseWriter{ResponseWriter: w}
	lrw.WriteHeader(200)

	w.Write([]byte(alive))
}
func nextRequestID() ksuid.KSUID {
	return ksuid.New()
}

func logcall(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		start := time.Now()

		lrw := loggingResponseWriter{ResponseWriter: w}
		requestid := nextRequestID()

		ctx := context.WithValue(r.Context(), "requestid", requestid)
		next.ServeHTTP(&lrw, r.WithContext(ctx)) // call original

		w.Header().Set("Content-Length", strconv.Itoa(lrw.length))
		log.WithFields(log.Fields{
			"method":    r.Method,
			"uri":       r.RequestURI,
			"fabric":    r.URL.Query().Get("target"),
			"status":    lrw.statusCode,
			"length":    lrw.length,
			"requestid": requestid,
			"exec_time": time.Since(start).Microseconds(),
		}).Info("api call")
	})

}

func promMonitor(next http.Handler, ops *prometheus.HistogramVec, endpoint string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		start := time.Now()

		lrw := loggingResponseWriter{ResponseWriter: w}

		next.ServeHTTP(&lrw, r) // call original

		response := time.Since(start).Seconds()

		ops.With(prometheus.Labels{"url": endpoint, "status": strconv.Itoa(lrw.statusCode)}).Observe(response)
	})
}
