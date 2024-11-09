package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"reflect"
	"testing"
)

func Test_writeConntrackInfo(t *testing.T) {
	type args struct {
		filename string
		output   []byte
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := writeConntrackInfo(tt.args.filename, tt.args.output); (err != nil) != tt.wantErr {
				t.Errorf("writeConntrackInfo() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_parseLabelKey(t *testing.T) {
	type args struct {
		key string
	}
	tests := []struct {
		name string
		args args
		want prometheus.Labels
	}{
		{"first test", args{key: "10.11.61.101:mservice-bos-11:10.11.61.102:mservice-bos-12:1233:4321"}, prometheus.Labels{
			"src_ip":   "10.11.61.101",
			"src_pod":  "mservice-bos-11",
			"dst_ip":   "10.11.61.102",
			"dst_pod":  "mservice-bos-12",
			"src_port": "1233",
			"dst_port": "4321",
		},
		},
		{"second test", args{key: "10.11.62.101:mservice-bos-11:10.11.61.102:mservice-bos-12:1233:4321"}, prometheus.Labels{
			"src_ip":   "10.11.62.101",
			"src_pod":  "mservice-bos-11",
			"dst_ip":   "10.11.61.102",
			"dst_pod":  "mservice-bos-12",
			"src_port": "1233",
			"dst_port": "4321",
		},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseLabelKey(tt.args.key); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseLabelKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
