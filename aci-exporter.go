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
	"flag"
	"fmt"
	"net/http/pprof"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"net/http"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/ksuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// Common constants
const (
	HeaderAPICCookie         = "APIC-cookie"
	ErrMsgInvalidStatusCode  = "Not a valid status code"
	LogFieldRequestID        = "requestid"
	LogFieldFabric           = "fabric"
	LogFieldExecTime         = "exec_time"
	ACIApiReturnedStatusCode = "ACI api returned %d"
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

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

var version = "undefined"
var querySet = mapset.NewSet[string]()

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
	logLevel := flag.String("loglevel", viper.GetString("loglevel"), "Set log level, default info")
	config := flag.String("config", viper.GetString("config"), "Set configuration file, default config.yaml")
	usage := flag.Bool("u", false, "Show usage")
	writeConfig := flag.Bool("default", false, "Write default config named aci_exporter_default_config.yaml. If config.d directory exist all queries will be merged into single file.")
	profiling := flag.Bool("pprof", false, "Enable profiling")

	cli := flag.Bool("cli", false, "Run single query")
	class := flag.String("class", viper.GetString("class"), "The class name - only cli")
	query := flag.String("query", viper.GetString("query"), "The query for the class - only cli")
	fabric := flag.String("fabric", viper.GetString("fabric"), "The fabric name - only cli")
	versionFlag := flag.Bool("v", false, "Show version")

	// configuration directory is always relative to the directory where the config file is located
	configDirName := flag.String("config_dir", viper.GetString("config_dir"), "The configuration directory, default config.d")

	flag.Parse()
	if *versionFlag {
		fmt.Printf("aci-exporter, version %s\n", version)
		os.Exit(0)
	}

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

	if *logLevel != "" {
		level, err := log.ParseLevel(*logLevel)
		if err != nil {
			log.Error(fmt.Sprintf("Not supported log level - %s", err))
			os.Exit(1)
		}
		log.SetLevel(level)
	}

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Error(fmt.Sprintf("Error open logfile %s - %s", *logFile, err))
			os.Exit(1)
		}
		log.SetOutput(f)
	}

	if *cli {
		data, err := cliQuery(context.TODO(), fabric, class, query)
		if err != nil {
			fmt.Printf("Error %s", err)
			os.Exit(1)
		}
		fmt.Printf("%s", data)
		os.Exit(0)
	}

	if *writeConfig {
		var queries = AllQueries{}
		_, err := os.Stat(*configDirName)
		if err == nil {
			readConfigDirectory(configDirName, ".", &queries)
			viper.Set("class_queries", queries.ClassQueries)
			viper.Set("group_class_queries", queries.GroupClassQueries)
			viper.Set("compound_queries", queries.CompoundClassQueries)
		} else {
			log.Info(fmt.Sprintf("No %s directory found - will not merge in queries", *configDirName))
		}
		err = viper.WriteConfigAs("./aci_exporter_default_config.yaml")
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

	// Check if the arguments for loglevel, logfile and logformat is set on command line
	// if not use the values from the config file if exists or defaults
	if !isFlagPassed("loglevel") {
		level, err := log.ParseLevel(viper.GetString("loglevel"))
		if err != nil {
			log.Error(fmt.Sprintf("Not supported log level - %s", err))
			os.Exit(1)
		}
		log.SetLevel(level)
	}

	if !isFlagPassed("logfile") {
		if viper.GetString("logfile") != "" {
			f, err := os.OpenFile(viper.GetString("logfile"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Error(fmt.Sprintf("Error open logfile %s - %s", viper.GetString("logfile"), err))
				os.Exit(1)
			}
			log.SetOutput(f)
		}
	}

	if !isFlagPassed("logformat") {
		if viper.GetString("logFormat") == "text" {
			log.SetFormatter(&log.TextFormatter{})
		}
	}

	if !isFlagPassed("config_dir") {
		dirName := viper.GetString("config_dir")
		configDirName = &dirName
	}

	// Read all config from config file and directory
	var queries = AllQueries{}

	readConfigDirectory(configDirName, filepath.Dir(viper.ConfigFileUsed()), &queries)

	// check for configurations in the main configuration file

	err = viper.UnmarshalKey("class_queries", &queries.ClassQueries)
	if err != nil {
		log.Error("Unable to decode class_queries into struct - ", err)
		os.Exit(1)
	}

	err = viper.UnmarshalKey("compound_queries", &queries.CompoundClassQueries)
	if err != nil {
		log.Error("Unable to decode compound_queries into struct - ", err)
		os.Exit(1)
	}

	err = viper.UnmarshalKey("qroup_class_queries", &queries.GroupClassQueries)
	if err != nil {
		log.Error("Unable to decode qroup_class_queries into struct - ", err)
		os.Exit(1)
	}

	allQueries := AllQueries{
		ClassQueries:         queries.ClassQueries,
		CompoundClassQueries: queries.CompoundClassQueries,
		GroupClassQueries:    queries.GroupClassQueries,
	}

	// Create a set of all query names - used to validate the query parameter
	createQueryNameSet(allQueries)

	// Init all fabrics
	allFabrics := make(map[string]*Fabric)

	err = viper.UnmarshalKey("fabrics", &allFabrics)
	if err != nil {
		log.Error("Unable to decode fabrics into struct - ", err)
		os.Exit(1)
	}

	// Init discovery settings
	for fabricName := range allFabrics {
		if allFabrics[fabricName].DiscoveryConfig.TargetFields == nil {
			allFabrics[fabricName].DiscoveryConfig.TargetFields = viper.GetStringSlice("service_discovery.target_fields")
		}
		if allFabrics[fabricName].DiscoveryConfig.LabelsKeys == nil {
			allFabrics[fabricName].DiscoveryConfig.LabelsKeys = viper.GetStringSlice("service_discovery.labels")
		}
		if allFabrics[fabricName].DiscoveryConfig.TargetFormat == "" {
			allFabrics[fabricName].DiscoveryConfig.TargetFormat = viper.GetString("service_discovery.target_format")
		}
	}
	// Overwrite username or password for APIC by environment variables if set
	for fabricName := range allFabrics {
		fabricEnv(fabricName, allFabrics)
	}

	for fabricName, fabric := range allFabrics {
		fabric.FabricName = fabricName
	}

	if val, exists := os.LookupEnv(fmt.Sprintf("%s_FABRIC_NAMES", ExporterNameAsEnv())); exists == true && val != "" {
		for _, fabricName := range strings.Split(val, ",") {
			fabricEnv(fabricName, allFabrics)
		}
	}

	for fabricName := range allFabrics {
		log.WithFields(log.Fields{
			LogFieldFabric: fabricName,
		}).Info("Configured fabric")
	}

	handler := &HandlerInit{allQueries, allFabrics}

	// Create a Prometheus histogram for response time of the exporter
	responseTime := promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    MetricsPrefix + "request_duration_seconds",
		Help:    "Histogram of the time (in seconds) each request took to complete.",
		Buckets: []float64{0.050, 0.100, 0.200, 0.500, 0.800, 1.00, 2.000, 3.000},
	},
		[]string{"url", "status"},
	)

	// Setup handler for aci destinations
	http.Handle("/probe", logCall(promMonitor(http.HandlerFunc(handler.getMonitorMetrics), responseTime, "/probe")))
	http.Handle("/alive", logCall(promMonitor(http.HandlerFunc(alive), responseTime, "/alive")))
	http.Handle("/sd", logCall(promMonitor(http.HandlerFunc(handler.discovery), responseTime, "/sd")))

	// Setup handler for exporter metrics
	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))
	// profiling endpoint
	if *profiling {
		log.Info(fmt.Sprintf("Starting profiling endpoint on %s", viper.GetString("pport")))
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		go func() { log.Fatal(http.ListenAndServe(viper.GetString("pport"), mux)) }()
	}

	s := &http.Server{
		ReadTimeout:  viper.GetDuration("httpserver.read_timeout") * time.Second,
		WriteTimeout: viper.GetDuration("httpserver.write_timeout") * time.Second,
		Addr:         ":" + strconv.Itoa(viper.GetInt("port")),
	}
	log.WithFields(log.Fields{
		"version":       version,
		"port":          viper.GetInt("port"),
		"config_file":   viper.ConfigFileUsed(),
		"read_timeout":  viper.GetDuration("httpserver.read_timeout") * time.Second,
		"write_timeout": viper.GetDuration("httpserver.write_timeout") * time.Second,
	}).Info("aci-exporter starting")
	log.Fatal(s.ListenAndServe())
}

func createQueryNameSet(allQueries AllQueries) {
	for queryName, _ := range allQueries.ClassQueries {
		querySet.Add(queryName)
	}
	for queryName, _ := range allQueries.CompoundClassQueries {
		querySet.Add(queryName)
	}
	for queryName, _ := range allQueries.GroupClassQueries {
		querySet.Add(queryName)
	}
	querySet.Add("faults")
}

func readConfigDirectory(configDirName *string, dirPath string, queries *AllQueries) {
	configDir := filepath.Join(dirPath, *configDirName)
	_, err := os.Stat(configDir)
	if err != nil {
		log.Info("Configuration directory do not exist - ", err)
		return
	}

	files, err := os.ReadDir(configDir)
	if err != nil {
		log.Error("Unable to access files in the configuration directory - ", err)
		os.Exit(1)
	}

	for _, file := range files {

		yamlFile, err := os.ReadFile(filepath.Join(configDir, file.Name()))
		if err != nil {
			log.Error(fmt.Sprintf("Reading the config file %s failed - ", file.Name()), err)
			os.Exit(1)
		}
		err = yaml.Unmarshal(yamlFile, &queries)
		if err != nil {
			log.Error(fmt.Sprintf("Unmarshal the config file %s failed - ", file.Name()), err)
			os.Exit(1)
		}

		log.WithFields(log.Fields{
			"file": file.Name(),
		}).Info("Directory configuration files")
	}
}

func fabricEnv(fabricName string, allFabrics map[string]*Fabric) {
	fabricNameAsEnv := strings.ToUpper(strings.ReplaceAll(fabricName, "-", "_"))
	if allFabrics[fabricName] == nil {
		allFabrics[fabricName] = &Fabric{}
	}
	if val, exists := os.LookupEnv(fmt.Sprintf("%s_FABRICS_%s_USERNAME", ExporterNameAsEnv(), fabricNameAsEnv)); exists == true && val != "" {
		allFabrics[fabricName].Username = val
	}
	if val, exists := os.LookupEnv(fmt.Sprintf("%s_FABRICS_%s_PASSWORD", ExporterNameAsEnv(), fabricNameAsEnv)); exists == true && val != "" {
		allFabrics[fabricName].Password = val
	}
	if val, exists := os.LookupEnv(fmt.Sprintf("%s_FABRICS_%s_ACI_NAME", ExporterNameAsEnv(), fabricNameAsEnv)); exists == true && val != "" {
		allFabrics[fabricName].AciName = val
	}
	if val, exists := os.LookupEnv(fmt.Sprintf("%s_FABRICS_%s_APIC", ExporterNameAsEnv(), fabricNameAsEnv)); exists == true && val != "" {
		for _, url := range strings.Split(val, ",") {
			allFabrics[fabricName].Apic = append(allFabrics[fabricName].Apic, url)
		}
	}
}

func cliQuery(ctx context.Context, fabric *string, class *string, query *string) (string, error) {
	err := viper.ReadInConfig()
	if err != nil {
		log.Error("Configuration file not valid - ", err)
		return "", err
	}
	username := viper.GetString(fmt.Sprintf("fabrics.%s.username", *fabric))
	password := viper.GetString(fmt.Sprintf("fabrics.%s.password", *fabric))
	apicControllers := viper.GetStringSlice(fmt.Sprintf("fabrics.%s.apic", *fabric))
	aciName := viper.GetString(fmt.Sprintf("fabrics.%s.aci_name", *fabric))

	fabricConfig := Fabric{Username: username, Password: password, Apic: apicControllers, FabricName: *fabric, AciName: aciName}

	con := newAciConnection(&fabricConfig, nil)
	err = con.login(ctx)
	if err != nil {
		fmt.Printf("Login error %s", err)
		return "", err
	}

	var data string

	if len(*query) > 0 && string((*query)[0]) != "?" {
		data, err = con.GetByClassQuery(ctx, *class, fmt.Sprintf("?%s", *query))
	} else {
		data, err = con.GetByClassQuery(ctx, *class, *query)
	}

	if err != nil {
		fmt.Printf("Error %s", err)
		return "", err
	}
	return fmt.Sprintf("%s", data), nil
}

type HandlerInit struct {
	AllQueries AllQueries
	AllFabrics map[string]*Fabric
}

func (h HandlerInit) discovery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	fabric := r.URL.Query().Get("target")
	if fabric != strings.ToLower(fabric) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Content-Length", "0")
		log.WithFields(log.Fields{
			LogFieldFabric: fabric,
		}).Warning("fabric target must be in lower case")
		lrw := loggingResponseWriter{ResponseWriter: w}
		lrw.WriteHeader(400)
		return
	}

	if fabric != "" {
		_, ok := h.AllFabrics[fabric]
		if !ok {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			w.Header().Set("Content-Length", "0")
			log.WithFields(log.Fields{
				LogFieldFabric: fabric,
			}).Warning("fabric target do not exists")
			lrw := loggingResponseWriter{ResponseWriter: w}
			lrw.WriteHeader(404)
			return
		}
	}

	discovery := Discovery{
		Fabric:  fabric,
		Fabrics: h.AllFabrics,
	}

	lrw := loggingResponseWriter{ResponseWriter: w}

	serviceDiscoveries, err := discovery.DoDiscovery(ctx)
	if err != nil || len(serviceDiscoveries) == 0 {
		lrw.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	lrw.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "    ")
	if err := enc.Encode(serviceDiscoveries); err != nil {
		lrw.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func formatQueries(queries string) string {
	var trimQueries string
	trimQueries = strings.ReplaceAll(queries, " ", "")
	trimQueries = strings.ReplaceAll(trimQueries, "\n", "")
	trimQueries = strings.ReplaceAll(trimQueries, "\t", "")
	return trimQueries
}

func (h HandlerInit) getMonitorMetrics(w http.ResponseWriter, r *http.Request) {

	openmetrics := false
	// Check accept header for open metrics
	if r.Header.Get("Accept") == "application/openmetrics-text" || viper.GetBool("openmetrics") || viper.GetBool("metric_format.openmetrics") {
		openmetrics = true
	}

	var node *string
	fabric := r.URL.Query().Get("target")
	queryArray := r.URL.Query()["queries"]
	nodeName := r.URL.Query().Get("node")

	if nodeName != "" {
		// Check if the nodeName is a valid url if not append https://
		if queryArray == nil {
			lrw := loggingResponseWriter{ResponseWriter: w}
			lrw.WriteHeader(400)
			return
		}
		_, err := url.ParseRequestURI(nodeName)
		if err != nil {
			nodeName = fmt.Sprintf("https://%s", nodeName)
		}
		node = &nodeName
	} else {
		node = nil
	}

	// Handle multiple queries
	var queries []string
	for _, queryString := range queryArray {
		// If the queries query parameter include a comma, split it and add to the queries array
		querySplit := strings.Split(queryString, ",")
		for _, query := range querySplit {
			// Validate that the query is a valid query
			if !querySet.Contains(query) {
				w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
				w.Header().Set("Content-Length", "0")
				log.WithFields(log.Fields{
					LogFieldFabric: fabric,
					"query":        query,
				}).Warning("not a valid query")
				lrw := loggingResponseWriter{ResponseWriter: w}
				lrw.WriteHeader(400)
				return
			}
			queries = append(queries, strings.TrimSpace(query))
		}
	}

	if fabric != strings.ToLower(fabric) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Content-Length", "0")
		log.WithFields(log.Fields{
			LogFieldFabric: fabric,
		}).Warning("fabric target must be in lower case")
		lrw := loggingResponseWriter{ResponseWriter: w}
		lrw.WriteHeader(400)
		return
	}

	// Check if a valid target
	_, ok := h.AllFabrics[fabric]
	if !ok {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Content-Length", "0")
		log.WithFields(log.Fields{
			LogFieldFabric: fabric,
		}).Warning("fabric target do not exists")
		lrw := loggingResponseWriter{ResponseWriter: w}
		lrw.WriteHeader(404)
		return
	}

	ctx := r.Context()
	ctx = context.WithValue(ctx, LogFieldFabric, fabric)
	api := newAciAPI(ctx, h.AllFabrics[fabric], h.AllQueries, queries, node)

	start := time.Now()
	aciName, metrics, err := api.CollectMetrics()
	log.WithFields(log.Fields{
		LogFieldRequestID: ctx.Value(LogFieldRequestID),
		LogFieldExecTime:  time.Since(start).Microseconds(),
		LogFieldFabric:    fmt.Sprintf("%v", ctx.Value(LogFieldFabric)),
	}).Info("total query collection time")

	commonLabels := make(map[string]string)
	commonLabels["aci"] = aciName
	commonLabels["fabric"] = fabric

	start = time.Now()
	metricsFormat := NewMetricFormat(openmetrics, viper.GetBool("metric_format.label_key_to_lower_case"),
		viper.GetBool("metric_format.label_key_to_snake_case"))
	var bodyText = Metrics2Prometheus(metrics, api.metricPrefix, commonLabels, metricsFormat)

	log.WithFields(log.Fields{
		LogFieldRequestID: ctx.Value(LogFieldRequestID),
		LogFieldExecTime:  time.Since(start).Microseconds(),
		LogFieldFabric:    fmt.Sprintf("%v", ctx.Value(LogFieldFabric)),
	}).Info("metrics to prometheus format")

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
	_, _ = w.Write([]byte(bodyText))

	return
}

func alive(w http.ResponseWriter, r *http.Request) {

	var alive = fmt.Sprintf("Alive!\n")
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(alive)))
	lrw := loggingResponseWriter{ResponseWriter: w}
	lrw.WriteHeader(200)

	_, _ = w.Write([]byte(alive))
}

func nextRequestID() ksuid.KSUID {
	return ksuid.New()
}

func logCall(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		start := time.Now()

		lrw := loggingResponseWriter{ResponseWriter: w}
		requestId := nextRequestID()

		ctx := context.WithValue(r.Context(), LogFieldRequestID, requestId)
		next.ServeHTTP(&lrw, r.WithContext(ctx)) // call original

		w.Header().Set("Content-Length", strconv.Itoa(lrw.length))
		log.WithFields(log.Fields{
			"method":          r.Method,
			"uri":             r.RequestURI,
			LogFieldFabric:    r.URL.Query().Get("target"),
			"status":          lrw.statusCode,
			"length":          lrw.length,
			LogFieldRequestID: ctx.Value(LogFieldRequestID),
			LogFieldExecTime:  time.Since(start).Microseconds(),
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
