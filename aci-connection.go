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
	"context"
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
	Name:    MetricsPrefix + "response_time_from_apic",
	Help:    "Histogram of the time (in seconds) each request took to complete.",
	Buckets: []float64{0.050, 0.100, 0.200, 0.500, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0},
},
	[]string{"fabric", "class", "method", "status"},
)

// AciConnection is the connection object
type AciConnection struct {
	ctx              context.Context
	fabricConfig     *Fabric
	activeController *int
	URLMap           map[string]string
	Headers          map[string]string
	Client           http.Client
	responseTime     *prometheus.HistogramVec
}

func newAciConnction(ctx context.Context, fabricConfig *Fabric) *AciConnection {
	// Empty cookie jar
	jar, _ := cookiejar.New(nil)

	var httpClient = HTTPClient{
		InsecureHTTPS:       viper.GetBool("httpclient.insecureHTTPS"),
		Timeout:             viper.GetInt("httpclient.timeout"),
		Keepalive:           viper.GetInt("httpclient.keepalive"),
		Tlshandshaketimeout: viper.GetInt("httpclient.tlshandshaketimeout"),
		cookieJar:           jar,
	}.GetClient()

	var headers = make(map[string]string)
	headers["Content-Type"] = "application/json"

	urlMap := make(map[string]string)

	urlMap["login"] = "/api/aaaLogin.xml"
	urlMap["logout"] = "/api/aaaLogout.xml"
	urlMap["faults"] = "/api/class/faultCountsWithDetails.json"
	urlMap["aci_name"] = "/api/mo/topology/pod-1/node-1/av.json"

	return &AciConnection{
		ctx:              ctx,
		fabricConfig:     fabricConfig,
		activeController: new(int),
		URLMap:           urlMap,
		Headers:          headers,
		Client:           *httpClient,
		responseTime:     responseTime,
	}
}

func (c AciConnection) login() error {
	for i, controller := range c.fabricConfig.Apic {
		_, status, err := c.doPostXML("login", fmt.Sprintf("%s%s", controller, c.URLMap["login"]),
			[]byte(fmt.Sprintf("<aaaUser name=%s pwd=%s/>", c.fabricConfig.Username, c.fabricConfig.Password)))
		if err != nil || status != 200 {

			err = fmt.Errorf("failed to login to %s, try next apic", controller)

			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(err)
		} else {
			*c.activeController = i
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Info(fmt.Sprintf("Using apic %s", controller))
			return nil
		}
	}
	return fmt.Errorf("failed to login to any apic controllers")

}

func (c AciConnection) logout() bool {
	_, status, err := c.doPostXML("logout", fmt.Sprintf("%s%s", c.fabricConfig.Apic[*c.activeController], c.URLMap["logout"]),
		[]byte(fmt.Sprintf("<aaaUser name=%s/>", c.fabricConfig.Username)))
	if err != nil || status != 200 {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		return false
	}
	return true
}

func (c AciConnection) getByQuery(table string) (string, error) {
	data, err := c.get(table, fmt.Sprintf("%s%s", c.fabricConfig.Apic[*c.activeController], c.URLMap[table]))
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(fmt.Sprintf("Request %s failed - %s.", c.URLMap[table], err))
		return "", err
	}
	return string(data), nil
}

func (c AciConnection) getByClassQuery(class string, query string) (string, error) {
	data, err := c.get(class, fmt.Sprintf("%s/api/class/%s.json%s", c.fabricConfig.Apic[*c.activeController], class, query))
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(fmt.Sprintf("Class request %s failed - %s.", class, err))
		return "", err
	}
	return string(data), nil
}

func (c AciConnection) get(label string, url string) ([]byte, error) {
	start := time.Now()
	body, status, err := c.doGet(url)
	responseTime := time.Since(start).Seconds()
	c.responseTime.With(prometheus.Labels{
		"fabric": fmt.Sprintf("%v", c.ctx.Value("fabric")),
		"class":  label,
		"method": "GET",
		"status": strconv.Itoa(status)}).Observe(responseTime)

	log.WithFields(log.Fields{
		"method":    "GET",
		"uri":       url,
		"class":     label,
		"status":    status,
		"length":    len(body),
		"requestid": c.ctx.Value("requestid"),
		"exec_time": time.Since(start).Microseconds(),
		"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
	}).Info("api call fabric")
	return body, err
}

func (c AciConnection) doGet(url string) ([]byte, int, error) {

	req, err := http.NewRequest("GET", url, bytes.NewBuffer([]byte{}))
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		return nil, 0, err
	}
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		return nil, 0, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(err)
			return nil, resp.StatusCode, err
		}

		return bodyBytes, resp.StatusCode, nil
	}
	return nil, resp.StatusCode, fmt.Errorf("ACI api returned %d", resp.StatusCode)
}

func (c AciConnection) doPostXML(label string, url string, requestBody []byte) ([]byte, int, error) {

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		return nil, 0, err
	}

	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/xml")

	start := time.Now()
	resp, err := c.Client.Do(req)
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		return nil, 0, err
	}
	responseTime := time.Since(start).Seconds()
	var status = resp.StatusCode

	c.responseTime.With(prometheus.Labels{
		"fabric": fmt.Sprintf("%v", c.ctx.Value("fabric")),
		"class":  label,
		"method": "POST",
		"status": strconv.Itoa(status)}).Observe(responseTime)

	log.WithFields(log.Fields{
		"method":    "POST",
		"uri":       url,
		"status":    status,
		"requestid": c.ctx.Value("requestid"),
		"exec_time": time.Since(start).Microseconds(),
		"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
	}).Info("api call fabric")

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(err)
			return nil, resp.StatusCode, err
		}

		return bodyBytes, resp.StatusCode, nil
	}
	return nil, resp.StatusCode, fmt.Errorf("ACI api returned %d", resp.StatusCode)
}
