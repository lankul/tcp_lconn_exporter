package main

import (
	"fmt"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	ignoreSrcIPs string
	pattern      = `ESTABLISHED src=(\S+) dst=(\S+) sport=(\d+) dport=(\d+) packets=(\d+) bytes=(\d+) src=\S+ dst=\S+ sport=\d+ dport=\d+ packets=(\d+) bytes=(\d+)`
	re           = regexp.MustCompile(pattern)

	tcpSendBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "tcp_send_bytes_total",
		Help: "Total number of sent TCP bytes",
	}, []string{"src_ip", "src_pod", "src_port", "dst_ip", "dst_pod", "dst_port"})

	tcpRespBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "tcp_resp_bytes_total",
		Help: "Total number of response TCP bytes",
	}, []string{"src_ip", "src_pod", "src_port", "dst_ip", "dst_pod", "dst_port"})

	tcpSendPackets *prometheus.GaugeVec
	tcpRespPackets *prometheus.GaugeVec

	lastUpdated = make(map[string]time.Time)
	mu          sync.RWMutex
	ttl         = 15 * time.Second

	podInfo = make(map[string]string) // map[podIP]podName

	connTable = "conntrack.txt"
)

func main() {
	ttlThreshold := pflag.Int("ttl-threshold", 10, "Consider connections greater than ttl threshold to be long connections")
	collectPackets := pflag.Bool("collect-packets", false, "Flag to collect packets metrics, (default false)")
	pflag.StringVar(&ignoreSrcIPs, "ignore-src-ips", "", "Comma-separated list of src IPs to ignore. Default is none.")

	kubeconfig := pflag.String("kubeconfig", "/root/.kube/config", "absolute path to the kubeconfig file")
	namespaces := pflag.String("namespaces", "default", "Comma-separated list of Kubernetes namespaces to fetch pods from")
	duration := pflag.String("interval", "30m", "Duration to sleep between pod info fetches")

	metricsPort := pflag.String("port", "9149", "Port to expose /metrics endpoint")
	metricsEndpoint := pflag.String("metrics-endpoint", "/metrics", "Endpoint for exposing metrics")

	pflag.Parse()

	// Parse the provided duration
	interval, err := time.ParseDuration(*duration)
	if err != nil {
		panic(err)
	}

	// Split the namespaces by comma
	//fmt.Printf("namespaces is %s\n", *namespaces)
	namespaceList := strings.Split(*namespaces, ",")
	//fmt.Println(namespaceList)

	// Set up the kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	// Create the Kubernetes client
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	if _, err = os.Stat(connTable); os.IsNotExist(err) {
		fmt.Println("first writing conntrack information...")
		cmd := exec.Command("conntrack", "-p", "tcp", "--state", "ESTABLISHED", "-L")
		output, _ := cmd.Output()
		if err := writeConntrackInfo(connTable, output); err != nil {
			fmt.Printf("Error writing conntrack information: %v\n", err)
			return
		}
		fmt.Println("first Conntrack information saved to file.")
	}

	go fetchPodInfoPeriodically(clientset, namespaceList, interval)

	prometheus.MustRegister(tcpSendBytes, tcpRespBytes)
	if *collectPackets {
		tcpSendPackets = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tcp_send_packets_total",
			Help: "Total packets sent",
		}, []string{"src_ip", "src_pod", "src_port", "dst_ip", "dst_pod", "dst_port"})

		tcpRespPackets = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "tcp_resp_packets_total",
			Help: "Total packets responded",
		}, []string{"src_ip", "src_pod", "src_port", "dst_ip", "dst_pod", "dst_port"})

		prometheus.MustRegister(tcpSendPackets, tcpRespPackets)
	}

	go collectMetrics(*ttlThreshold, *collectPackets)
	go cleanupStaleMetrics(*collectPackets)

	http.Handle(*metricsEndpoint, promhttp.Handler())

	fmt.Printf("Starting exporter on :%s%s\ncollectPackets: %t\nwith ttl threshold %d...\n", *metricsPort, *metricsEndpoint, *collectPackets, *ttlThreshold)
	if err := http.ListenAndServe(":"+*metricsPort, nil); err != nil {
		panic(err)
	}
}
