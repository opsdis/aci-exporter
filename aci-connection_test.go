package main

import (
	"encoding/json"
	"testing"
)

func TestLoginPayloadEscaping(t *testing.T) {
	c := &AciConnection{fabricConfig: &Fabric{Username: `ad"min`, Password: `p"a\ss{}`}}

	var payload struct {
		AaaUser struct {
			Attributes struct {
				Name string `json:"name"`
				Pwd  string `json:"pwd"`
			} `json:"attributes"`
		} `json:"aaaUser"`
	}
	if err := json.Unmarshal(c.loginPayload(), &payload); err != nil {
		t.Fatalf("login payload is not valid JSON: %v", err)
	}
	if payload.AaaUser.Attributes.Name != `ad"min` || payload.AaaUser.Attributes.Pwd != `p"a\ss{}` {
		t.Errorf("unexpected credentials in payload: %+v", payload)
	}
}
