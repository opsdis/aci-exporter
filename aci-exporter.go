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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/ksuid"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"net/http"
)

type errormsg struct {
	Errormsg string `json:"error"`
	Status   int    `json:"status"`
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	length     int
}

// Implement interface WriteHeader
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

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {

	flag.Usage = func() {
		fmt.Printf("Usage of %s:\n", ExporterName)
		fmt.Printf("Version %s\n", version)
		flag.PrintDefaults()
	}

	SetDefaultValues()

	flag.Int("p", viper.GetInt("port"), "The port to start on")
	logFile := flag.String("logfile", viper.GetString("logfile"), "Set log file, default stdout")
	config := flag.String("config", viper.GetString("config"), "Set configuration file, default config.yaml")
	usage := flag.Bool("u", false, "Show usage")
	writeConfig := flag.Bool("default", false, "Write default config")

	flag.Parse()

	log.SetFormatter(&log.JSONFormatter{})

	viper.SetConfigName(*config) // name of config file (without extension)
	viper.SetConfigType("yaml")  // REQUIRED if the config file does not have the extension in the name

	viper.AddConfigPath(".")
	viper.AddConfigPath("$HOME/.mob-exporter")
	viper.AddConfigPath("/usr/local/etc/mob-exporter")
	viper.AddConfigPath("/etc/mob-exporter")

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Println(err)
		}
		log.SetOutput(f)
	}

	if *writeConfig {
		err := viper.WriteConfigAs("./mob_exporter_default_config.yaml")
		if err != nil {
			log.Error("Can not write default config file - ", err)
		}
		os.Exit(0)
	}

	// Find and read the config file
	err := viper.ReadInConfig()
	if err != nil {
		log.Info("No configuration file found - use defaults")
	}

	if *usage {
		flag.Usage()
		os.Exit(0)
	}

	// Create a Prometheus histogram for response time of the exporter
	responseTime := promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    MetricsPrefix + "request_duration_seconds",
		Help:    "Histogram of the time (in seconds) each request took to complete.",
		Buckets: []float64{0.001, 0.005, 0.010, 0.020, 0.100, 0.200, 0.500},
	},
		[]string{"url", "status"},
	)

	// Setup handler for backend provider mertics
	http.Handle("/probe", logcall(promMonitor(http.HandlerFunc(getMonitorMetrics), responseTime, "/probe")))
	http.Handle("/alive", logcall(promMonitor(http.HandlerFunc(alive), responseTime, "/alive")))

	// Setup handler for exporter metrics
	http.Handle("/metrics", promhttp.Handler())

	log.Info(fmt.Sprintf("%s starting on port %d", ExporterName, viper.GetInt("port")))

	s := &http.Server{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		Addr:         ":" + strconv.Itoa(viper.GetInt("port")),
	}
	log.Fatal(s.ListenAndServe())
}

func getMonitorMetrics(w http.ResponseWriter, r *http.Request) {

	hostname := r.URL.Query().Get("target")
	profile := r.URL.Query().Get("profile")

	username := viper.GetString(fmt.Sprintf("profile.%s.username", profile))
	password := viper.GetString(fmt.Sprintf("profile.%s.password", profile))

	api := *newAciAPI(hostname, username, password)

	fabricName, metrics, status := api.CollectMetrics()

	if status {

		commonLabels := make(map[string]string)
		commonLabels["aci"] = fabricName

		var bodyText = Metrics2Prometheus(metrics, api.metricPrefix, commonLabels)

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(bodyText)))

		lrw := loggingResponseWriter{ResponseWriter: w}
		if bodyText == "" {
			lrw.WriteHeader(404)
		}

		w.Write([]byte(bodyText))

	} else {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Content-Length", "0")

		lrw := loggingResponseWriter{ResponseWriter: w}
		lrw.WriteHeader(401)
	}
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
			"method": r.Method,
			"uri":    r.RequestURI,
			//"endpoint":  endpoint,
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
