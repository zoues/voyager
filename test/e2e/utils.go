package e2e

import (
	"bytes"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/appscode/errors"
	aci "github.com/appscode/k8s-addons/api"
	"github.com/appscode/log"
	ingresscontroller "github.com/appscode/voyager/pkg/controller/ingress"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/labels"
)

func (ing *IngressTestSuit) getURLs(baseIngress *aci.Ingress) ([]string, error) {
	serverAddr := make([]string, 0)
	var err error
	if ing.t.Config.ProviderName == "minikube" {
		for i := 0; i < maxRetries; i++ {
			var outputs []byte
			log.Infoln("Running Command", "`minikube", "service", ingresscontroller.VoyagerPrefix+baseIngress.Name+" --url`")
			outputs, err = exec.Command("/usr/local/bin/minikube", "service", ingresscontroller.VoyagerPrefix+baseIngress.Name, "--url").CombinedOutput()
			if err == nil {
				log.Infoln("Output\n", string(outputs))
				for _, output := range strings.Split(string(outputs), "\n") {
					if strings.HasPrefix(output, "http") {
						serverAddr = append(serverAddr, output)
					}
				}
				return serverAddr, nil
			}
			log.Infoln("minikube service returned with", err, string(outputs))
			time.Sleep(time.Second * 10)
		}
		if err != nil {
			return nil, errors.New().WithCause(err).WithMessage("Failed to load service from minikube").Internal()
		}
	} else {
		var svc *api.Service
		for i := 0; i < maxRetries; i++ {
			svc, err = ing.t.KubeClient.Core().Services(baseIngress.Namespace).Get(ingresscontroller.VoyagerPrefix + baseIngress.Name)
			if err == nil {
				if len(svc.Status.LoadBalancer.Ingress) != 0 {
					ips := make([]string, 0)
					for _, ingress := range svc.Status.LoadBalancer.Ingress {
						ips = append(ips, ingress.IP)
					}
					var ports []int32
					if len(svc.Spec.Ports) > 0 {
						for _, port := range svc.Spec.Ports {
							if port.NodePort > 0 {
								ports = append(ports, port.Port)
							}
						}
					}
					for _, port := range ports {
						for _, ip := range ips {
							var doc bytes.Buffer
							err = defaultUrlTemplate.Execute(&doc, struct {
								IP   string
								Port int32
							}{
								ip,
								port,
							})
							if err != nil {
								return nil, errors.New().WithCause(err).Internal()
							}

							u, err := url.Parse(doc.String())
							if err != nil {
								return nil, errors.New().WithCause(err).Internal()
							}

							serverAddr = append(serverAddr, u.String())
						}
					}
					return serverAddr, nil
				}
			}
			time.Sleep(time.Second * 10)
			log.Infoln("Waiting for service to be created")
		}
		if err != nil {
			return nil, errors.New().WithCause(err).Internal()
		}
	}
	return serverAddr, nil
}

func (ing *IngressTestSuit) getDaemonURLs(baseIngress *aci.Ingress) ([]string, error) {
	serverAddr := make([]string, 0)
	nodes, err := ing.t.KubeClient.Core().Nodes().List(api.ListOptions{
		LabelSelector: labels.SelectorFromSet(
			ingresscontroller.ParseNodeSelector(
				baseIngress.Annotations[ingresscontroller.DaemonNodeSelector],
			),
		),
	})
	if err != nil {
		return nil, errors.New().WithCause(err).Internal()
	}

	var svc *api.Service
	var ports []int32
	for i := 0; i < maxRetries; i++ {
		svc, err = ing.t.KubeClient.Core().Services(baseIngress.Namespace).Get(ingresscontroller.VoyagerPrefix + baseIngress.Name)
		if err == nil {
			if len(svc.Spec.Ports) > 0 {
				for _, port := range svc.Spec.Ports {
					ports = append(ports, port.Port)
				}
			}
			break
		}
		time.Sleep(time.Second * 10)
		log.Infoln("Waiting for service to be created")
	}
	if err != nil {
		return nil, errors.New().WithCause(err).Internal()
	}

	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == api.NodeLegacyHostIP || addr.Type == api.NodeExternalIP {
				for _, port := range ports {
					var doc bytes.Buffer
					err = defaultUrlTemplate.Execute(&doc, struct {
						IP   string
						Port int32
					}{
						addr.Address,
						port,
					})
					if err != nil {
						return nil, errors.New().WithCause(err).Internal()
					}

					u, err := url.Parse(doc.String())
					if err != nil {
						return nil, errors.New().WithCause(err).Internal()
					}
					serverAddr = append(serverAddr, u.String())
				}
			}
		}
	}
	return serverAddr, nil
}
