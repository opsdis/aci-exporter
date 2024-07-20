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
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/tidwall/gjson"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const TTLOffset = 120

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
	token    string
	ttl      int64
	expire   int64
	lifetime int64
}

// AciConnection is the connection object
type AciConnection struct {
	fabricConfig     *Fabric
	activeController *int
	URLMap           map[string]string
	Headers          map[string]string
	Client           http.Client
	token            *AciToken
	tokenMutex       sync.Mutex
	// If a node query this is set to the instance
	Node *string
}

var connectionCache = make(map[string]*AciConnection)

// cacheName returns a unique name for the connection. Every connection is unique per fabric and node with own
// cache entry
func cacheName(aciName string, node *string) string {
	if node == nil {
		return aciName
	}
	return aciName + *node
}

func newAciConnection(fabricConfig *Fabric, node *string) *AciConnection {
	// Check if we have a connection in the cache
	val, ok := connectionCache[cacheName(fabricConfig.FabricName, node)]
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
		fabricConfig:     fabricConfig,
		activeController: new(int),
		URLMap:           urlMap,
		Headers:          headers,
		Client:           *httpClient,
		Node:             node,
	}
	connectionCache[cacheName(fabricConfig.FabricName, node)] = con
	return connectionCache[cacheName(fabricConfig.FabricName, node)]
}

// login get the existing token if valid or do a full /login
func (c *AciConnection) login(ctx context.Context) error {

	err, done := c.tokenProcessing(ctx)
	if done {
		return err
	}
	return c.loginProcessing(ctx)

}

// loginProcessing do a full /login
func (c *AciConnection) loginProcessing(ctx context.Context) error {
	c.tokenMutex.Lock()
	defer c.tokenMutex.Unlock()
	if c.Node != nil {
		return c.nodeLogin(ctx)
	} else {
		return c.apicLogin(ctx)
	}
}

func (c *AciConnection) apicLogin(ctx context.Context) error {
	for i, controller := range c.fabricConfig.Apic {

		response, status, err := c.doPostJSON(ctx, "login", fmt.Sprintf("%s%s", controller, c.URLMap["login"]),
			[]byte(fmt.Sprintf("{\"aaaUser\":{\"attributes\":{\"name\":\"%s\",\"pwd\":\"%s\"}}}", c.fabricConfig.Username, c.fabricConfig.Password)))

		if err != nil || status != 200 {

			err = fmt.Errorf("failed to login to %s, try next apic", controller)

			log.WithFields(log.Fields{
				LogFieldRequestID: ctx.Value(LogFieldRequestID),
				"fabric":          fmt.Sprintf("%v", c.fabricConfig.FabricName),
				"token":           "login",
				"controller":      controller,
			}).Error(err)
		} else {
			c.newToken(response)

			*c.activeController = i
			log.WithFields(log.Fields{
				LogFieldRequestID: ctx.Value(LogFieldRequestID),
				LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
				"token":           "login",
				"controller":      controller,
			}).Info(fmt.Sprintf("Using apic %s", controller))
			return nil
		}
	}
	return fmt.Errorf("failed to login to any apic controllers")
}

func (c *AciConnection) nodeLogin(ctx context.Context) error {
	// Node query
	response, status, err := c.doPostJSON(ctx, "login", fmt.Sprintf("%s%s", *c.Node, c.URLMap["login"]),
		[]byte(fmt.Sprintf("{\"aaaUser\":{\"attributes\":{\"name\":\"%s\",\"pwd\":\"%s\"}}}", c.fabricConfig.Username, c.fabricConfig.Password)))

	if err != nil || status != 200 {
		err = fmt.Errorf("failed to login to %s", *c.Node)
		log.WithFields(log.Fields{
			LogFieldRequestID: ctx.Value(LogFieldRequestID),
			LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
			"token":           fmt.Sprintf("login"),
			"node":            *c.Node,
		}).Error(err)
		return fmt.Errorf("failed to login to node")
	}

	c.newToken(response)
	log.WithFields(log.Fields{
		LogFieldRequestID: ctx.Value(LogFieldRequestID),
		LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
		"token":           fmt.Sprintf("login"),
		"node":            *c.Node,
	}).Info(fmt.Sprintf("Using node"))
	return nil
}

// tokenProcessing if token are valid reuse or try to do a /refresh
func (c *AciConnection) tokenProcessing(ctx context.Context) (error, bool) {
	if c.token != nil {
		c.tokenMutex.Lock()
		defer c.tokenMutex.Unlock()
		if c.token.lifetime < time.Now().Unix() {
			log.WithFields(log.Fields{
				LogFieldRequestID: ctx.Value(LogFieldRequestID),
				LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
				"token":           fmt.Sprintf("lifetime"),
			}).Info("token reached lifetime seconds")
			return nil, false
		} else if c.token.expire < time.Now().Unix() {
			response, status, err := c.get(ctx, "refresh", fmt.Sprintf("%s%s", c.fabricConfig.Apic[*c.activeController], c.URLMap["refresh"]))
			if err != nil || status != 200 {
				log.WithFields(log.Fields{
					LogFieldRequestID: ctx.Value(LogFieldRequestID),
					LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
					"token":           fmt.Sprintf("refresh"),
				}).Warning(err)
				refreshFailedMetric.With(prometheus.Labels{
					LogFieldFabric: fmt.Sprintf("%v", c.fabricConfig.FabricName)}).Inc()
				return err, false
			} else {
				c.refreshToken(response)
				log.WithFields(log.Fields{
					LogFieldRequestID: ctx.Value(LogFieldRequestID),
					LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
					"token":           fmt.Sprintf("refresh"),
				}).Info("refresh token")
				refreshMetric.With(prometheus.Labels{
					LogFieldFabric: fmt.Sprintf("%v", c.fabricConfig.FabricName)}).Inc()
				return nil, true
			}
		} else {
			log.WithFields(log.Fields{
				LogFieldRequestID:    ctx.Value(LogFieldRequestID),
				LogFieldFabric:       fmt.Sprintf("%v", c.fabricConfig.FabricName),
				"token":              fmt.Sprintf("valid"),
				"valid_time_seconds": c.token.expire - time.Now().Unix()}).Debug("token still valid")
			return nil, true
		}
	}
	// Need to do /login
	return nil, false
}

func (c *AciConnection) newToken(response []byte) {
	token := gjson.Get(string(response), "imdata.0.aaaLogin.attributes.token").String()
	ttl := gjson.Get(string(response), "imdata.0.aaaLogin.attributes.refreshTimeoutSeconds").Int()
	lifetimeSeconds := gjson.Get(string(response), "imdata.0.aaaLogin.attributes.maximumLifetimeSeconds").Int()
	now := time.Now().Unix()
	c.token = &AciToken{
		token:    token,
		ttl:      ttl,
		expire:   now + ttl - TTLOffset,
		lifetime: now + lifetimeSeconds - TTLOffset,
	}
}

func (c *AciConnection) refreshToken(response []byte) {
	token := gjson.Get(string(response), "imdata.0.aaaLogin.attributes.token").String()
	ttl := gjson.Get(string(response), "imdata.0.aaaLogin.attributes.refreshTimeoutSeconds").Int()

	c.token = &AciToken{
		token:    token,
		ttl:      ttl,
		expire:   time.Now().Unix() + ttl - TTLOffset,
		lifetime: c.token.lifetime,
	}
}

func (c *AciConnection) GetByQuery(ctx context.Context, table string) (string, error) {
	data, _, err := c.get(ctx, table, fmt.Sprintf("%s%s", c.fabricConfig.Apic[*c.activeController], c.URLMap[table]))
	if err != nil {
		log.WithFields(log.Fields{
			LogFieldRequestID: ctx.Value(LogFieldRequestID),
			LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
		}).Error(fmt.Sprintf("Request %s failed - %s.", c.URLMap[table], err))
		return "", err
	}
	return string(data), nil
}

func (c *AciConnection) GetByClassQuery(ctx context.Context, class string, query string) (string, error) {
	if c.Node == nil {
		// A apic query
		data, _, err := c.get(ctx, class, fmt.Sprintf("%s/api/class/%s.json%s", c.fabricConfig.Apic[*c.activeController], class, query))
		if err != nil {
			log.WithFields(log.Fields{
				LogFieldRequestID: ctx.Value(LogFieldRequestID),
				LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
			}).Error(fmt.Sprintf("Class request %s failed - %s.", class, err))
			return "", err
		}
		return string(data), nil
	} else {
		// A node query
		data, _, err := c.get(ctx, class, fmt.Sprintf("%s/api/class/%s.json%s", *c.Node, class, query))
		if err != nil {
			log.WithFields(log.Fields{
				LogFieldRequestID: ctx.Value(LogFieldRequestID),
				LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
				"node":            c.Node,
			}).Error(fmt.Sprintf("Class request %s failed - %s.", class, err))
			return "", err
		}
		return string(data), nil
	}
}

func (c *AciConnection) get(ctx context.Context, label string, url string) ([]byte, int, error) {
	start := time.Now()
	//body, status, err := c.doGet(ctx, url)

	aciClient := NewAciClient(c.Client, c.Headers, c.token, c.fabricConfig.FabricName, url)

	body, status, err := aciClient.Get(ctx, url)

	responseTime := time.Since(start).Seconds()
	responseTimeMetric.With(prometheus.Labels{
		LogFieldFabric: fmt.Sprintf("%v", c.fabricConfig.FabricName),
		"class":        label,
		"method":       "GET",
		"status":       strconv.Itoa(status)}).Observe(responseTime)

	log.WithFields(log.Fields{
		"method":          "GET",
		"uri":             url,
		"class":           label,
		"status":          status,
		"length":          len(body),
		LogFieldRequestID: ctx.Value(LogFieldRequestID),
		"exec_time":       time.Since(start).Microseconds(),
		LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
	}).Info("api call fabric")
	return body, status, err
}

func (c *AciConnection) doGet(ctx context.Context, url string) ([]byte, int, error) {

	req, err := http.NewRequest("GET", url, bytes.NewBuffer([]byte{}))
	if err != nil {
		log.WithFields(log.Fields{
			LogFieldRequestID: ctx.Value(LogFieldRequestID),
			LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
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
			LogFieldRequestID: ctx.Value(LogFieldRequestID),
			LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
		}).Error(err)
		return nil, 0, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.WithFields(log.Fields{
				LogFieldRequestID: ctx.Value(LogFieldRequestID),
				LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
			}).Error(err)
			return nil, resp.StatusCode, err
		}

		return bodyBytes, resp.StatusCode, nil
	}
	return nil, resp.StatusCode, fmt.Errorf("ACI api returned %d", resp.StatusCode)
}

func (c *AciConnection) doPostJSON(ctx context.Context, label string, url string, requestBody []byte) ([]byte, int, error) {

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		log.WithFields(log.Fields{
			LogFieldRequestID: ctx.Value(LogFieldRequestID),
			LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
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
			LogFieldRequestID: ctx.Value(LogFieldRequestID),
			LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
		}).Error(err)
		return nil, 0, err
	}
	responseTime := time.Since(start).Seconds()
	var status = resp.StatusCode

	responseTimeMetric.With(prometheus.Labels{
		LogFieldFabric: fmt.Sprintf("%v", c.fabricConfig.FabricName),
		"class":        label,
		"method":       "POST",
		"status":       strconv.Itoa(status)}).Observe(responseTime)

	log.WithFields(log.Fields{
		"method":          "POST",
		"uri":             url,
		"status":          status,
		LogFieldRequestID: ctx.Value(LogFieldRequestID),
		"exec_time":       time.Since(start).Microseconds(),
		LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
	}).Info("api call fabric")

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.WithFields(log.Fields{
				LogFieldRequestID: ctx.Value(LogFieldRequestID),
				LogFieldFabric:    fmt.Sprintf("%v", c.fabricConfig.FabricName),
			}).Error(err)
			return nil, resp.StatusCode, err
		}

		return bodyBytes, resp.StatusCode, nil
	}
	return nil, resp.StatusCode, fmt.Errorf("ACI api returned %d", resp.StatusCode)
}
