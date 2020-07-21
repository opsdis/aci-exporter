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
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var responseTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    MetricsPrefix + "response_time_from_mointor_system",
	Help:    "Histogram of the time (in seconds) each request took to complete.",
	Buckets: []float64{0.010, 0.020, 0.100, 0.200, 0.500, 1.0, 2.0},
},
	[]string{"type", "method", "status"},
)

// AciConnection is the connection object
type AciConnection struct {
	hostname     string
	username     string
	password     string
	URLMap       map[string]string
	Headers      map[string]string
	Client       http.Client
	responseTime *prometheus.HistogramVec
}

type monitorCountResponse struct {
	Count int `json:"count"`
}

func newAciConnction(apichostname string, username string, password string) *AciConnection {
	// Empty cookie jar
	jar, _ := cookiejar.New(nil)

	var httpClient = HTTPClient{
		InsecureHTTPS:       viper.GetBool("HTTPClient.insecureHTTPS"),
		Timeout:             viper.GetInt("HTTPClient.timeout"),
		Keepalive:           viper.GetInt("HTTPClient.keepalive"),
		Tlshandshaketimeout: viper.GetInt("HTTPClient.tlshandshaketimeout"),
		cookieJar:           jar,
	}.GetClient()

	var headers = make(map[string]string)
	headers["Content-Type"] = "application/json"

	urlMap := make(map[string]string)

	urlMap["login"] = fmt.Sprintf("%s/api/mo/aaaLogin.xml", apichostname)
	urlMap["logout"] = fmt.Sprintf("%s/api/mo/aaaLogout.xml", apichostname)
	urlMap["fabric_health"] = fmt.Sprintf("%s/api/class/fabricHealthTotal.json", apichostname)
	urlMap["node_health"] = fmt.Sprintf("%s/api/class/topSystem.json?rsp-subtree-include=health", apichostname)
	urlMap["tenant_health"] = fmt.Sprintf("%s/api/class/fvTenant.json?rsp-subtree-include=health", apichostname)
	urlMap["faults"] = fmt.Sprintf("%s/api/class/faultCountsWithDetails.json", apichostname)
	urlMap["infra_node_health"] = fmt.Sprintf("%s/api/class/infraWiNode.json", apichostname)
	// Used to get the fabric name
	urlMap["fabric_name"] = fmt.Sprintf("%s/api/mo/topology/pod-1/node-1/av.json", apichostname)

	return &AciConnection{
		hostname:     apichostname,
		username:     username,
		password:     password,
		URLMap:       urlMap,
		Headers:      headers,
		Client:       *httpClient,
		responseTime: responseTime,
	}
}

func (c AciConnection) login() bool {
	_, status, err := c.doPostXML(c.URLMap["login"], []byte(fmt.Sprintf("<aaaUser name=%s pwd=%s/>", c.username, c.password)))
	if err != nil || status != 200 {
		log.Error(err)
		return false
	}
	return true
}

func (c AciConnection) logout() bool {
	_, status, err := c.doPostXML(c.URLMap["logout"], []byte(fmt.Sprintf("<aaaUser name=%s/>", c.username)))
	if err != nil || status != 200 {
		log.Error(err)
		return false
	}
	return true
}

func (c AciConnection) getByQuery(table string) (string, error) {
	data, err := c.get(c.URLMap[table])
	if err != nil {
		log.Error(err)
		return "", err
	}
	return string(data), nil
}

func (c AciConnection) getByClassQuery(class string, query string) (string, error) {
	data, err := c.get(fmt.Sprintf("%s/api/class/%s.json%s", c.hostname, class, query))
	if err != nil {
		log.Error(err)
		return "", err
	}
	return string(data), nil
}

func (c AciConnection) get(url string) ([]byte, error) {
	start := time.Now()
	body, status, err := c.doGet(url)
	responseTime := time.Since(start).Seconds()
	c.responseTime.WithLabelValues("monitor", "GET", strconv.Itoa(status)).Observe(responseTime)
	log.WithFields(log.Fields{
		"method": "GET",
		"uri":    url,
		//"endpoint":  endpoint,
		"status": status,
		"length": len(body),
		//"requestid": requestid,
		"exec_time": time.Since(start).Microseconds(),
		"system":    "monitor",
	}).Info("api call monitor system")
	return body, err
}

func (c AciConnection) doGet(url string) ([]byte, int, error) {

	req, err := http.NewRequest("GET", url, bytes.NewBuffer([]byte{}))
	if err != nil {
		log.Error(err)
		return nil, 0, err
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		log.Error(err)
		return nil, 0, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error(err)
			return nil, resp.StatusCode, err
		}

		return bodyBytes, resp.StatusCode, nil
	}
	return nil, resp.StatusCode, fmt.Errorf("ACI api returned %d", resp.StatusCode)
}

func (c AciConnection) doPostXML(url string, requestBody []byte) ([]byte, int, error) {

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		log.Error(err)
		return nil, 0, err
	}

	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/xml")

	start := time.Now()
	resp, err := c.Client.Do(req)
	if err != nil {
		log.Error(err)
		return nil, 0, err
	}
	responseTime := time.Since(start).Seconds()
	var status = resp.StatusCode
	c.responseTime.WithLabelValues("monitor", "POST", strconv.Itoa(status)).Observe(responseTime)
	log.WithFields(log.Fields{
		"method": "POST",
		"uri":    url,
		//"endpoint":  endpoint,
		"status": status,
		//"length": len(),
		//"requestid": requestid,
		"exec_time": time.Since(start).Microseconds(),
		"system":    "monitor",
	}).Info("api call monitor system")

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error(err)
			return nil, resp.StatusCode, err
		}

		return bodyBytes, resp.StatusCode, nil
	}
	return nil, resp.StatusCode, fmt.Errorf("ACI api returned %d", resp.StatusCode)
}
