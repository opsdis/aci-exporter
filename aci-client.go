package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"strings"
	"time"
)

type AciClient interface {
	Get(ctx context.Context, url string) ([]byte, int, error)
}

func NewAciClient(client http.Client, headers map[string]string, token *AciToken, fabricName string, url string) AciClient {

	if strings.Contains(url, "order-by") {
		if viper.GetBool("HTTPClient.parallel_paging") {
			return &AciClientParallelPage{
				Client:     client,
				Headers:    headers,
				Token:      token,
				FabricName: fabricName,
				PageSize:   viper.GetInt("HTTPClient.pagesize"),
			}
		}
		return &AciClientSequentialPage{
			Client:     client,
			Headers:    headers,
			Token:      token,
			FabricName: fabricName,
			PageSize:   viper.GetInt("HTTPClient.pagesize"),
		}
	}

	return &AciClientSequential{
		Client:     client,
		Headers:    headers,
		Token:      token,
		FabricName: fabricName,
	}
}

type AciClientSequential struct {
	Client     http.Client
	Headers    map[string]string
	Token      *AciToken
	FabricName string
}

func (acs *AciClientSequential) Get(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", url, bytes.NewBuffer([]byte{}))
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acs.FabricName),
		}).Error(err)
		return nil, 0, err
	}
	for k, v := range acs.Headers {
		req.Header.Set(k, v)
	}

	cookie := http.Cookie{
		Name:       "APIC-cookie",
		Value:      acs.Token.token,
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

	resp, err := acs.Client.Do(req)
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acs.FabricName),
		}).Error(err)
		return nil, 0, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.WithFields(log.Fields{
				"requestid": ctx.Value("requestid"),
				"fabric":    fmt.Sprintf("%v", acs.FabricName),
			}).Error(err)
			return nil, resp.StatusCode, err
		}

		return bodyBytes, resp.StatusCode, nil
	}
	return nil, resp.StatusCode, fmt.Errorf("ACI api returned %d", resp.StatusCode)
}

type ACIResponse struct {
	TotalCount uint64                   `json:"totalCount"`
	ImData     []map[string]interface{} `json:"imdata"`
}

type AciClientSequentialPage struct {
	Client     http.Client
	Headers    map[string]string
	Token      *AciToken
	FabricName string
	PageSize   int
}

func (acsp *AciClientSequentialPage) Get(ctx context.Context, url string) ([]byte, int, error) {

	aciResponse := ACIResponse{
		TotalCount: 0,
		ImData:     make([]map[string]interface{}, 0, acsp.PageSize),
	}

	pagedUrl := ""

	// do a single call to get totalCount
	if strings.Contains(url, "?") {
		pagedUrl = "%s&page-size=%d&page=%d"
	} else {
		pagedUrl = "%s?page-size=%d&page=%d"
	}

	// First request to determine the total count
	bodyBytes, status, err := acsp.getPage(ctx, url, pagedUrl, 0)
	if err != nil {
		return nil, status, err
	}

	aciResponse.TotalCount = gjson.Get(string(bodyBytes), "totalCount").Uint()
	_ = json.Unmarshal(bodyBytes, &aciResponse)

	numberOfPages := aciResponse.TotalCount / uint64(acsp.PageSize)

	for ii := 1; ii < int(numberOfPages)+1; ii++ {
		bodyBytes, status, err = acsp.getPage(ctx, url, pagedUrl, ii)
		if err != nil {
			return nil, status, err
		}

		aciResponsePage := ACIResponse{
			TotalCount: 0,
			ImData:     make([]map[string]interface{}, 0, acsp.PageSize),
		}

		_ = json.Unmarshal(bodyBytes, &aciResponsePage)
		aciResponse.ImData = append(aciResponse.ImData, aciResponsePage.ImData...)
	}

	data, _ := json.Marshal(aciResponse)

	return data, status, nil

}

func (acsp *AciClientSequentialPage) getPage(ctx context.Context, url string, pagedUrl string, page int) ([]byte, int, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(pagedUrl, url, acsp.PageSize, page), bytes.NewBuffer([]byte{}))
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acsp.FabricName),
		}).Error(err)
		return nil, 0, err
	}
	for k, v := range acsp.Headers {
		req.Header.Set(k, v)
	}

	cookie := http.Cookie{
		Name:       "APIC-cookie",
		Value:      acsp.Token.token,
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

	resp, err := acsp.Client.Do(req)
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acsp.FabricName),
		}).Error(err)
		return nil, 0, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acsp.FabricName),
			"status":    resp.StatusCode,
		}).Error("Not a valid status code")
		return nil, resp.StatusCode, fmt.Errorf("ACI api returned %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acsp.FabricName),
		}).Error(err)
		return nil, resp.StatusCode, err
	}
	return bodyBytes, resp.StatusCode, nil
}

type AciClientParallelPage struct {
	Client     http.Client
	Headers    map[string]string
	Token      *AciToken
	FabricName string
	PageSize   int
}

func (acpp *AciClientParallelPage) Get(ctx context.Context, url string) ([]byte, int, error) {

	aciResponse := ACIResponse{
		TotalCount: 0,
		ImData:     make([]map[string]interface{}, 0, acpp.PageSize),
	}

	pagedUrl := ""

	// do a single call to get totalCount
	if strings.Contains(url, "?") {
		pagedUrl = "%s&page-size=%d&page=%d"
	} else {
		pagedUrl = "%s?page-size=%d&page=%d"
	}

	// First request to determine the total count
	bodyBytes, status, err := acpp.getPage(ctx, url, pagedUrl, 0)
	if err != nil {
		return nil, status, err
	}

	aciResponse.TotalCount = gjson.Get(string(bodyBytes), "totalCount").Uint()
	_ = json.Unmarshal(bodyBytes, &aciResponse)

	numberOfPages := aciResponse.TotalCount / uint64(acpp.PageSize)
	ch := make(chan ACIResponse)
	for ii := 1; ii < int(numberOfPages)+1; ii++ {
		go acpp.getParallelPage(ctx, url, pagedUrl, ii, ch)
		log.Info(fmt.Sprintf("Send page %d", ii))
	}
	for i := 1; i < int(numberOfPages)+1; i++ {
		comm := <-ch
		for _, imData := range comm.ImData {
			aciResponse.ImData = append(aciResponse.ImData, imData)
		}
		log.Info(fmt.Sprintf("Fetched page %d", i))
	}

	data, _ := json.Marshal(aciResponse)

	return data, status, nil

}

func (acpp *AciClientParallelPage) getPage(ctx context.Context, url string, pagedUrl string, page int) ([]byte, int, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(pagedUrl, url, acpp.PageSize, page), bytes.NewBuffer([]byte{}))
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acpp.FabricName),
		}).Error(err)
		return nil, 0, err
	}
	for k, v := range acpp.Headers {
		req.Header.Set(k, v)
	}

	cookie := http.Cookie{
		Name:       "APIC-cookie",
		Value:      acpp.Token.token,
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

	resp, err := acpp.Client.Do(req)
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acpp.FabricName),
		}).Error(err)
		return nil, 0, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acpp.FabricName),
			"status":    resp.StatusCode,
		}).Error("Not a valid status code")
		return nil, resp.StatusCode, fmt.Errorf("ACI api returned %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acpp.FabricName),
		}).Error(err)
		return nil, resp.StatusCode, err
	}
	return bodyBytes, resp.StatusCode, nil
}

func (acpp *AciClientParallelPage) getParallelPage(ctx context.Context, url string, pagedUrl string, page int, ch chan ACIResponse) {
	aciResponse := ACIResponse{
		TotalCount: 0,
		ImData:     make([]map[string]interface{}, 0, acpp.PageSize),
	}

	req, err := http.NewRequest("GET", fmt.Sprintf(pagedUrl, url, acpp.PageSize, page), bytes.NewBuffer([]byte{}))
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acpp.FabricName),
		}).Error(err)
		ch <- aciResponse
		return
	}
	for k, v := range acpp.Headers {
		req.Header.Set(k, v)
	}

	cookie := http.Cookie{
		Name:       "APIC-cookie",
		Value:      acpp.Token.token,
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

	resp, err := acpp.Client.Do(req)
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acpp.FabricName),
		}).Error(err)
		ch <- aciResponse
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acpp.FabricName),
			"status":    resp.StatusCode,
		}).Error("Not a valid status code")
		ch <- aciResponse
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithFields(log.Fields{
			"requestid": ctx.Value("requestid"),
			"fabric":    fmt.Sprintf("%v", acpp.FabricName),
		}).Error(err)
		ch <- aciResponse
		return
	}
	_ = json.Unmarshal(bodyBytes, &aciResponse)
	ch <- aciResponse
	return
}
