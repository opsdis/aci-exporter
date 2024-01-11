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
// Copyright 2020-2023 Opsdis

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

var responseTimeMetric = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    MetricsPrefix + "response_time_from_apic",
	Help:    "Histogram of the time (in seconds) each request took to complete.",
	Buckets: []float64{0.050, 0.100, 0.200, 0.500, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0},
},
	[]string{"fabric", "class", "method", "status"},
)

var refreshMetric = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: MetricsPrefix + "auth_refresh",
	Help: "Authentication refresh counter",
},
	[]string{"fabric"},
)

var refreshFailedMetric = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: MetricsPrefix + "auth_refresh_failed",
	Help: "Authentication refresh failed counter",
},
	[]string{"fabric"},
)

type AciToken struct {
	token  string
	ttl    int64
	expire int64
}

// AciConnection is the connection object
type AciConnection struct {
	ctx              context.Context
	fabricConfig     *Fabric
	activeController *int
	URLMap           map[string]string
	Headers          map[string]string
	Client           http.Client
	token            *AciToken
	tokenMutex       sync.Mutex
	pageSize         uint64
	parallelPaging   bool
	//responseTime     *prometheus.HistogramVec
}

var connectionCache = make(map[*Fabric]*AciConnection)

func newAciConnection(ctx context.Context, fabricConfig *Fabric) *AciConnection {

	val, ok := connectionCache[fabricConfig]
	if ok {
		return val
	}

	var httpClient = HTTPClient{
		InsecureHTTPS:       viper.GetBool("httpclient.insecureHTTPS"),
		Timeout:             viper.GetInt("httpclient.timeout"),
		Keepalive:           viper.GetInt("httpclient.keepalive"),
		Tlshandshaketimeout: viper.GetInt("httpclient.tlshandshaketimeout"),
	}.GetClient()

	var headers = make(map[string]string)
	headers["Content-Type"] = "application/json"

	urlMap := make(map[string]string)

	urlMap["login"] = "/api/aaaLogin.json"
	urlMap["logout"] = "/api/aaaLogout.json"
	urlMap["refresh"] = "/api/aaaRefresh.json"
	urlMap["faults"] = "/api/class/faultCountsWithDetails.json"

	con := &AciConnection{
		ctx:              ctx,
		fabricConfig:     fabricConfig,
		activeController: new(int),
		URLMap:           urlMap,
		Headers:          headers,
		Client:           *httpClient,
		//responseTime:     responseTime,
		pageSize:       viper.GetUint64("httpclient.pagesize"),
		parallelPaging: viper.GetBool("httpclient.parallel_paging"),
	}
	connectionCache[fabricConfig] = con
	return connectionCache[fabricConfig]
}

// login get the existing token if valid or do a full /login
func (c *AciConnection) login() error {

	err, done := c.tokenProcessing()
	if done {
		return err
	}
	return c.loginProcessing()

}

// loginProcessing do a full /login
func (c *AciConnection) loginProcessing() error {
	c.tokenMutex.Lock()
	defer c.tokenMutex.Unlock()
	for i, controller := range c.fabricConfig.Apic {

		response, status, err := c.doPostJSON("login", fmt.Sprintf("%s%s", controller, c.URLMap["login"]),
			[]byte(fmt.Sprintf("{\"aaaUser\":{\"attributes\":{\"name\":\"%s\",\"pwd\":\"%s\"}}}", c.fabricConfig.Username, c.fabricConfig.Password)))

		if err != nil || status != 200 {

			err = fmt.Errorf("failed to login to %s, try next apic", controller)

			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
				"token":     fmt.Sprintf("login"),
			}).Error(err)
		} else {
			c.newToken(response)

			*c.activeController = i
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
				"token":     fmt.Sprintf("login"),
			}).Info(fmt.Sprintf("Using apic %s", controller))
			return nil
		}
	}
	return fmt.Errorf("failed to login to any apic controllers")
}

// tokenProcessing if token are valid reuse or try to do a /refresh
func (c *AciConnection) tokenProcessing() (error, bool) {
	if c.token != nil {
		c.tokenMutex.Lock()
		defer c.tokenMutex.Unlock()
		if c.token.expire < time.Now().Unix() {
			response, status, err := c.get("refresh", fmt.Sprintf("%s%s", c.fabricConfig.Apic[*c.activeController], c.URLMap["refresh"]))
			if err != nil || status != 200 {
				//errRe = fmt.Errorf("failed to refresh token %s", c.fabricConfig.Apic[*c.activeController])
				log.WithFields(log.Fields{
					"requestid": c.ctx.Value("requestid"),
					"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
					"token":     fmt.Sprintf("refersh"),
				}).Warning(err)
				refreshFailedMetric.With(prometheus.Labels{
					"fabric": fmt.Sprintf("%v", c.ctx.Value("fabric"))}).Inc()
				return err, false
			} else {
				c.newToken(response)
				log.WithFields(log.Fields{
					"requestid": c.ctx.Value("requestid"),
					"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
					"token":     fmt.Sprintf("refersh"),
				}).Info("refresh token")
				refreshMetric.With(prometheus.Labels{
					"fabric": fmt.Sprintf("%v", c.ctx.Value("fabric"))}).Inc()
				return nil, true
			}
		} else {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
				"token":     fmt.Sprintf("valid"),
			}).Info("token still valid")
			return nil, true
		}
	}
	// Need to do /login
	return nil, false
}

func (c *AciConnection) newToken(response []byte) {
	token := gjson.Get(string(response), "imdata.0.aaaLogin.attributes.token").String()
	ttl := gjson.Get(string(response), "imdata.0.aaaLogin.attributes.refreshTimeoutSeconds").Int()

	c.token = &AciToken{
		token:  token,
		ttl:    ttl,
		expire: time.Now().Unix() + ttl - 60,
	}
}

func (c *AciConnection) logout() bool {
	_, status, err := c.doPostJSON("logout", fmt.Sprintf("%s%s", c.fabricConfig.Apic[*c.activeController], c.URLMap["logout"]),
		[]byte(fmt.Sprintf("{\"aaaUser\":{\"attributes\":{\"name\":\"%s\"}}}", c.fabricConfig.Username)))
	if err != nil || status != 200 {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		return false
	}
	return true
}

func (c *AciConnection) getByQuery(table string) (string, error) {
	data, _, err := c.get(table, fmt.Sprintf("%s%s", c.fabricConfig.Apic[*c.activeController], c.URLMap[table]))
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(fmt.Sprintf("Request %s failed - %s.", c.URLMap[table], err))
		return "", err
	}
	return string(data), nil
}

func (c *AciConnection) getByClassQuery(class string, query string) (string, error) {
	if c.parallelPaging && !strings.Contains(query, "rsp-subtree-include=count") {
		data, _, err := c.getParallel(class, fmt.Sprintf("%s/api/class/%s.json%s", c.fabricConfig.Apic[*c.activeController], class, query))
		if err != nil {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(fmt.Sprintf("Class request %s failed - %s.", class, err))
			return "", err
		}
		return string(data), nil
	} else {
		data, _, err := c.get(class, fmt.Sprintf("%s/api/class/%s.json%s", c.fabricConfig.Apic[*c.activeController], class, query))
		if err != nil {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(fmt.Sprintf("Class request %s failed - %s.", class, err))
			return "", err
		}
		return string(data), nil
	}
	/*
		if err != nil {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(fmt.Sprintf("Class request %s failed - %s.", class, err))
			return "", err
		}
		return string(data), nil
	*/
}

/*
	func (c *AciConnection) getByClassQueryN(class string, query string) (string, error) {
		data, _, err := c.get(class, fmt.Sprintf("%s/api/class/%s.json%s", c.fabricConfig.Apic[*c.activeController], class, query))
		if err != nil {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(fmt.Sprintf("Class request %s failed - %s.", class, err))
			return "", err
		}
		return string(data), nil

}
*/
func (c *AciConnection) get(class string, url string) ([]byte, int, error) {
	start := time.Now()
	body, status, err := c.doGet(url)
	responseTime := time.Since(start).Seconds()
	responseTimeMetric.With(prometheus.Labels{
		"fabric": fmt.Sprintf("%v", c.ctx.Value("fabric")),
		"class":  class,
		"method": "GET",
		"status": strconv.Itoa(status)}).Observe(responseTime)

	log.WithFields(log.Fields{
		"method":    "GET",
		"uri":       url,
		"class":     class,
		"status":    status,
		"length":    len(body),
		"requestid": c.ctx.Value("requestid"),
		"exec_time": time.Since(start).Microseconds(),
		"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
	}).Info("api call fabric")
	return body, status, err
}

func (c *AciConnection) getParallel(class string, url string) ([]byte, int, error) {
	start := time.Now()
	body, status, count, err := c.doPGet(class, url)
	responseTime := time.Since(start).Seconds()
	responseTimeMetric.With(prometheus.Labels{
		"fabric": fmt.Sprintf("%v", c.ctx.Value("fabric")),
		"class":  class,
		"method": "GET",
		"status": strconv.Itoa(status)}).Observe(responseTime)

	log.WithFields(log.Fields{
		"method":         "GET",
		"uri":            url,
		"class":          class,
		"status":         status,
		"length":         len(body),
		"parallel_count": count,
		"requestid":      c.ctx.Value("requestid"),
		"exec_time":      time.Since(start).Microseconds(),
		"fabric":         fmt.Sprintf("%v", c.ctx.Value("fabric")),
	}).Info("api call fabric parallel")
	return body, status, err
}

func (c *AciConnection) doGet(url string) ([]byte, int, error) {

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

	cookie := http.Cookie{
		Name:       "APIC-cookie",
		Value:      c.token.token,
		Path:       "",
		Domain:     "",
		Expires:    time.Time{},
		RawExpires: "",
		MaxAge:     0,
		Secure:     false,
		HttpOnly:   false,
		SameSite:   0,
		Raw:        "",
		Unparsed:   nil,
	}

	req.AddCookie(&cookie)

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
		bodyBytes, err := io.ReadAll(resp.Body)
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

func (c *AciConnection) doPostJSON(label string, url string, requestBody []byte) ([]byte, int, error) {

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
	req.Header.Set("Content-Type", "application/json")

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

	responseTimeMetric.With(prometheus.Labels{
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
		bodyBytes, err := io.ReadAll(resp.Body)
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

// ACIResponse Parallel
type ACIResponse struct {
	TotalCount uint64                   `json:"totalCount"`
	ImData     []map[string]interface{} `json:"imdata"`
}

func getNumberOfParallel(numerator uint64, denominator uint64) uint64 {
	quotient, remainder := divMod(numerator, denominator)
	if remainder > 0 {
		return quotient + 1
	}
	return quotient
}

func divMod(numerator uint64, denominator uint64) (quotient uint64, remainder uint64) {
	quotient = numerator / denominator // integer division, decimals are truncated
	remainder = numerator % denominator
	return quotient, remainder
}

func (c *AciConnection) doPGet(class string, url string) ([]byte, int, uint64, error) {
	if class == "eqptLC" {
		print("STOP")
	}
	//var resp http.Response

	aciResponse := ACIResponse{
		TotalCount: 0,
		ImData:     make([]map[string]interface{}, 0, c.pageSize),
	}

	var count uint64 = 0

	pagedUrl := url

	for {
		// Create paging url
		if strings.Contains(url, "?") {
			pagedUrl = fmt.Sprintf("%s&order-by=%s.dn&page=%d&page-size=%d", url, class, count, c.pageSize)
		} else {
			pagedUrl = fmt.Sprintf("%s?order-by=%s.dn&page=%d&page-size=%d", url, class, count, c.pageSize)
		}

		req, err := http.NewRequest("GET", pagedUrl, bytes.NewBuffer([]byte{}))
		log.Debug(fmt.Sprintf("url %s\n", pagedUrl))

		if err != nil {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(err)
			return nil, 0, 0, err
		}
		// Set headers
		for k, v := range c.Headers {
			req.Header.Set(k, v)
		}
		cookie := http.Cookie{
			Name:       "APIC-cookie",
			Value:      c.token.token,
			Path:       "",
			Domain:     "",
			Expires:    time.Time{},
			RawExpires: "",
			MaxAge:     0,
			Secure:     false,
			HttpOnly:   false,
			SameSite:   0,
			Raw:        "",
			Unparsed:   nil,
		}

		req.AddCookie(&cookie)

		// Will append the APIC-cookie
		resp, err := c.Client.Do(req)
		if req != nil {
			req.Body.Close()
		}

		if err != nil {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(err)
			return nil, 0, 0, err
		}

		if resp.StatusCode == http.StatusOK {
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				log.WithFields(log.Fields{
					"requestid": c.ctx.Value("requestid"),
					"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
				}).Error(err)
				return nil, resp.StatusCode, 0, err
			}
			// return the total and not the amount to be collected, but only first count
			if count == 0 {
				aciResponse.TotalCount = gjson.Get(string(bodyBytes), "totalCount").Uint()
			}
			tmpAciResponse := ACIResponse{
				TotalCount: 0,
				ImData:     make([]map[string]interface{}, 0, c.pageSize),
			}
			_ = json.Unmarshal(bodyBytes, &tmpAciResponse)
			//fmt.Printf("size returned %d\n", len(tmpAciResponse.ImData))
			for _, x := range tmpAciResponse.ImData {
				if x != nil {
					aciResponse.ImData = append(aciResponse.ImData, x)
				}
			}
			count = count + 1
			if count*c.pageSize >= aciResponse.TotalCount {
				break
			}

			resp.Body.Close()
		} else {
			// if not 200
			resp.Body.Close()
			return nil, resp.StatusCode, count, fmt.Errorf("ACI api returned %d", resp.StatusCode)
		}

	}
	data, _ := json.Marshal(aciResponse)

	return data, http.StatusOK, count, nil
}

func (c *AciConnection) doGetParallel(class string, url string) ([]byte, int, uint64, error) {

	var resp http.Response

	aciResponse := ACIResponse{
		TotalCount: 0,
		ImData:     make([]map[string]interface{}, 0, c.pageSize),
	}

	pagedUrl := url

	// do a single call to get totalCount
	if strings.Contains(url, "?") {
		pagedUrl = fmt.Sprintf("%s&order-by=%s.dn&page-size=%d&page=%d", url, class, c.pageSize, 0)
	} else {
		pagedUrl = fmt.Sprintf("%s?order-by=%s.dn&page-size=%d&page=%d", url, class, c.pageSize, 0)
	}

	req1, err := http.NewRequest("GET", pagedUrl+"0", bytes.NewBuffer([]byte{}))
	log.Debug(fmt.Sprintf("url %s\n", pagedUrl))

	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		return nil, 0, 0, err
	}
	// Set headers
	for k, v := range c.Headers {
		req1.Header.Set(k, v)
	}

	// Will append the APIC-cookie
	resp1, err := c.Client.Do(req1)
	if req1 != nil {
		req1.Body.Close()
	}

	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		return nil, 0, 0, err
	}

	if resp1.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(resp1.Body)
		if err != nil {
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(err)
			return nil, resp.StatusCode, 0, err
		}

		// Get the first (0) page data
		aciResponse.TotalCount = gjson.Get(string(bodyBytes), "totalCount").Uint()
		_ = json.Unmarshal(bodyBytes, &aciResponse)
		log.Info(fmt.Sprintf("Fetched page %d", 0))
	}

	numberOfParallel := getNumberOfParallel(aciResponse.TotalCount, c.pageSize)
	ch := make(chan ACIResponse)
	for ii := 1; ii < int(numberOfParallel); ii++ {
		go c.collectPage(class, url, pagedUrl, ch, ii)
	}

	//mu := sync.Mutex{}
	for i := 1; i < int(numberOfParallel); i++ {
		//mu.Lock()
		comm := <-ch
		for _, imData := range comm.ImData {
			aciResponse.ImData = append(aciResponse.ImData, imData)
			//log.Debug(fmt.Sprintf("%d:%s\n", i, imData["ethpmPhysIf"].(map[string]interface{})["attributes"].(map[string]interface{})["dn"].(string)))
		}
		//mu.Unlock()
		log.Debug(fmt.Sprintf("Fetched page %d", i))
	}

	data, _ := json.Marshal(aciResponse)

	return data, resp.StatusCode, numberOfParallel, nil
}

func (c *AciConnection) collectPage(class string, url string, pagedUrl string, ch chan ACIResponse, page int) {
	if strings.Contains(url, "?") {
		pagedUrl = fmt.Sprintf("%s&order-by=%s.dn&page-size=%d&page=%d", url, class, c.pageSize, page)
	} else {
		pagedUrl = fmt.Sprintf("%s?order-by=%s.dn&page-size=%d&page=%d", url, class, c.pageSize, page)
	}

	tmpAciResponse := ACIResponse{
		TotalCount: 0,
		ImData:     make([]map[string]interface{}, 0, c.pageSize),
	}

	req, err := http.NewRequest("GET", pagedUrl, bytes.NewBuffer([]byte{}))
	log.Debug(fmt.Sprintf("url %s\n", pagedUrl))

	if err != nil {
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		ch <- tmpAciResponse
		return
	}
	// Set headers
	for k, v := range c.Headers {
		req.Header.Set(k, v)
	}

	// Will append the APIC-cookie
	start := time.Now()
	resp, err := c.Client.Do(req)

	if err != nil {
		req.Body.Close()
		log.WithFields(log.Fields{
			"requestid": c.ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
		}).Error(err)
		ch <- tmpAciResponse
		return
	}

	responseTime := time.Since(start).Seconds()
	log.WithFields(log.Fields{
		"exec_time": responseTime,
		"class":     class,
		"page":      page,
		"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
	}).Info("call fabric paging")

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			req.Body.Close()
			log.WithFields(log.Fields{
				"requestid": c.ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", c.ctx.Value("fabric")),
			}).Error(err)
			ch <- tmpAciResponse
			return
		}

		_ = json.Unmarshal(bodyBytes, &tmpAciResponse)

		req.Body.Close()
	}
	ch <- tmpAciResponse
}
