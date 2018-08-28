package kubernetes

import (
	"bytes"
	"fmt"

	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
)

func (r Endpoints) String() string {
	if len(r) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for _, endpoint := range r[:len(r)-1] {
		fmt.Fprintf(&buf, fmt.Sprintf("endpoint(subsets: %v),", EndpointSubsets(endpoint.Subsets)))
	}
	fmt.Fprintf(&buf, fmt.Sprintf("endpoint(subsets: %v)", EndpointSubsets(r[len(r)-1].Subsets)))
	return buf.String()
}

type Endpoints []v1.Endpoints

func (r EndpointSubsets) String() string {
	if len(r) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for _, subset := range r[:len(r)-1] {
		fmt.Fprintf(&buf, fmt.Sprintf("subset(addresses: %v),", EndpointAddrs(subset.Addresses)))
	}
	fmt.Fprintf(&buf, fmt.Sprintf("subset(addresses: %v)", EndpointAddrs(r[len(r)-1].Addresses)))
	return buf.String()
}

type EndpointSubsets []v1.EndpointSubset

func (r EndpointAddrs) String() string {
	if len(r) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for _, addr := range r[:len(r)-1] {
		fmt.Fprintf(&buf, fmt.Sprintf("address(ip: %v, hostname: %q, nodename: %q),",
			addr.IP, addr.Hostname, safeStr(addr.NodeName)))
	}
	addr := r[len(r)-1]
	fmt.Fprintf(&buf, fmt.Sprintf("address(ip: %v, hostname: %q, nodename: %q)",
		addr.IP, addr.Hostname, safeStr(addr.NodeName)))
	return buf.String()
}

type EndpointAddrs []v1.EndpointAddress

func formatPodList(pods []v1.Pod) (result []string) {
	result = make([]string, 0, len(pods))
	for _, pod := range pods {
		result = append(result, formatPod(pod))
	}
	return result
}

func formatPod(pod v1.Pod) string {
	return fmt.Sprintf("%v/%v", pod.Namespace, pod.Name)
}

func podFields(pod v1.Pod) log.Fields {
	return log.Fields{"namespace": pod.Namespace, "name": pod.Name}
}

func safeStr(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}
