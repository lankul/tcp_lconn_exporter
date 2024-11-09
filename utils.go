package main

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func writeConntrackInfo(filename string, output []byte) error {
	err := os.WriteFile(filename, output, 0644)
	if err != nil {
		return err
	}
	return nil
}

func readConntrackInfo(filename string) ([]string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	return lines, nil
}

func collectMetrics(ttlThreshold int, collectPackets bool) {
	for {
		outputPre, err := readConntrackInfo(connTable)
		if err != nil {
			fmt.Printf("err is %v\n", err)
			panic(err)
		}

		mapResultPre := make(map[string]bool)
		for _, line := range outputPre {
			if !strings.Contains(line, "packets") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) > 8 {
				key := fields[4] + fields[5] + fields[6] + fields[7]
				mapResultPre[key] = true
			}
		}

		cmd := exec.Command("conntrack", "-p", "tcp", "--state", "ESTABLISHED", "-L")
		output, _ := cmd.Output()
		err = writeConntrackInfo(connTable, output)
		if err != nil {
			panic(err)
		}

		lines := strings.Split(string(output), "\n")
		match := false
		for _, line := range lines {
			if !strings.Contains(line, "packets") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) > 8 {
				key := fields[4] + fields[5] + fields[6] + fields[7]
				if mapResultPre[key] {
					match = true
				}
			}

			if !match {
				continue
			}

			matches := re.FindStringSubmatch(line)
			if len(matches) != 9 {
				fmt.Printf("Unexpected match length: %d for line: %s\n", len(matches), line)
				continue
			}

			srcIP := matches[1]
			if len(ignoreSrcIPs) > 0 {
				ignoredIPs := strings.Split(ignoreSrcIPs, ",")
				ignored := make(map[string]bool)
				for _, ip := range ignoredIPs {
					ignored[ip] = true
				}
				if _, exist := ignored[srcIP]; exist {
					continue
				}
			}

			dstIP := matches[2]
			srcPort := matches[3]
			dstPort := matches[4]

			sendBytes, _ := strconv.Atoi(matches[6])
			respBytes, _ := strconv.Atoi(matches[8])

			labels := prometheus.Labels{"src_ip": srcIP, "src_pod": podInfo[srcIP], "src_port": srcPort, "dst_ip": dstIP, "dst_pod": podInfo[dstIP], "dst_port": dstPort}
			if sendBytes > 1024 {
				tcpSendBytes.With(labels).Set(float64(sendBytes))
			}
			if respBytes > 1024 {
				tcpRespBytes.With(labels).Set(float64(respBytes))
			}

			labelKey := srcIP + ":" + podInfo[srcIP] + ":" + dstIP + ":" + podInfo[dstIP] + ":" + srcPort + ":" + dstPort
			mu.Lock()
			lastUpdated[labelKey] = time.Now()
			mu.Unlock()
			if collectPackets {
				sendPackets, _ := strconv.Atoi(matches[5])
				respPackets, _ := strconv.Atoi(matches[7])
				tcpSendPackets.With(labels).Set(float64(sendPackets))
				tcpRespPackets.With(labels).Set(float64(respPackets))
			}
		}

		<-time.After(time.Duration(ttlThreshold) * time.Second)
	}
}

func parseLabelKey(key string) prometheus.Labels {
	// Simple parse, split by ":"
	parts := strings.Split(key, ":")
	return prometheus.Labels{"src_ip": parts[0], "src_pod": parts[1], "dst_ip": parts[2], "dst_pod": parts[3], "src_port": parts[4], "dst_port": parts[5]}
}

func cleanupStaleMetrics(collectPackets bool) {
	for {
		<-time.After(15 * time.Second) // Cleanup every 15 seconds
		now := time.Now()
		mu.RLock()
		for labelKey, lastTime := range lastUpdated {
			if now.Sub(lastTime) > ttl {
				labels := parseLabelKey(labelKey)
				tcpSendBytes.Delete(labels)
				tcpRespBytes.Delete(labels)
				if collectPackets {
					tcpSendPackets.Delete(labels)
					tcpRespPackets.Delete(labels)
				}
				delete(lastUpdated, labelKey)
			}
		}
		mu.RUnlock()
	}
}

func fetchPodInfoPeriodically(clientset *kubernetes.Clientset, namespaces []string, interval time.Duration) {
	for {
		tempInfo := make(map[string]string)

		for _, namespace := range namespaces {
			pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				fmt.Printf("Error fetching pods from namespace %s: %s\n", namespace, err)
				return
			}
			for _, pod := range pods.Items {
				// Make the key unique across namespaces: "namespace/podName"
				//key := fmt.Sprintf("%s/%s", namespace, pod.Name)
				tempInfo[pod.Status.PodIP] = pod.Name
			}
		}

		podInfo = tempInfo
		//fmt.Printf("podInfo is %+v\n", podInfo)

		time.Sleep(interval)
	}
}
