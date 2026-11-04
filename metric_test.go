package main

import "testing"

func TestLabelValueEscaping(t *testing.T) {
	m := Metric{Labels: map[string]string{"descr": "Name \"foo\" in C:\\path\nnext"}}
	got := m.Labels2Prometheus(nil, NewMetricFormat(false, false, false))
	want := `descr="Name \"foo\" in C:\\path\nnext"`
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
