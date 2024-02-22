package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/postfinance/kubenurse/pkg/constant"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

const (
	maxRetries = 15
)

type Server struct {
	kubeClient            clientset.Interface
	sharedInformerFactor  informers.SharedInformerFactory
	queue                 workqueue.RateLimitingInterface
	svcInformerSynced     cache.InformerSynced
	ingressInformerSynced cache.InformerSynced
}

type EventType string

const (
	NodeAdd       EventType = "NODE_ADD"
	NodeUpdate    EventType = "NODE_UPDATE"
	NodeDelete    EventType = "NODE_DELETE"
	ServiceAdd    EventType = "SERVICE_ADD"
	ServiceUpdate EventType = "SERVICE_UPDATE"
	ServiceDelete EventType = "SERVICE_DELETE"
	IngressAdd    EventType = "INGRESS_ADD"
	IngressUpdate EventType = "INGRESS_UPDATE"
	IngressDelete EventType = "INGRESS_DELETE"
)

type Event struct {
	Obj  interface{}
	Type EventType
}

type Diagnose struct {
	CheckType     string `json:"check_type"`
	CheckProtocal string `json:"check_protocal"`
	CheckEndpoint string `json:"check_dst_endpoint"`
}

func NewServer(client clientset.Interface,
	informerFactory informers.SharedInformerFactory) *Server {
	server := &Server{
		kubeClient:           client,
		sharedInformerFactor: informerFactory,
		queue:                workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ubinurse-watcher"),
	}
	svcInformer := informerFactory.Core().V1().Services().Informer()
	svcInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    server.addService,
		UpdateFunc: server.updateService,
		DeleteFunc: server.deleteService,
	})
	server.svcInformerSynced = svcInformer.HasSynced

	ingressInformer := informerFactory.Networking().V1().Ingresses().Informer()
	ingressInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    server.addIngress,
		UpdateFunc: server.updateIngress,
		DeleteFunc: server.deleteIngress,
	})
	server.ingressInformerSynced = ingressInformer.HasSynced

	return server
}

func (server *Server) Run(stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer server.queue.ShutDown()

	klog.Infof("starting ubinurse watcher server")
	defer klog.Infof("shutting down ubinurse watcher server")

	if !cache.WaitForNamedCacheSync("ubinurse-watcher", stopCh,
		server.svcInformerSynced, server.ingressInformerSynced) {
		return fmt.Errorf("ubinurse-watcher informer sync failed")
	}

	go wait.Until(server.worker, time.Second, stopCh)
	<-stopCh
	return nil
}

func (server *Server) worker() {
	for server.processNextWorkItem() {
	}
}

func (server *Server) processNextWorkItem() bool {
	event, quit := server.queue.Get()
	if quit {
		return false
	}
	defer server.queue.Done(event)

	err := server.dispatch(event.(*Event))
	server.handleErr(err, event)

	return true
}

func (server *Server) dispatch(event *Event) error {
	switch event.Type {
	case ServiceAdd:
		return server.onServiceAdd(event.Obj.(*corev1.Service))
	case ServiceUpdate:
		return server.onServiceUpdate(event.Obj.(*corev1.Service))
	case ServiceDelete:
		return server.onServiceDelete(event.Obj.(*corev1.Service))
	case IngressAdd:
		return server.onIngressAdd(event.Obj.(*networkingv1.Ingress))
	case IngressUpdate:
		return server.onIngressUpdate(event.Obj.(*networkingv1.Ingress))
	case IngressDelete:
		return server.onIngressDelete(event.Obj.(*networkingv1.Ingress))
	default:
		return nil
	}
}

func (server *Server) onServiceAdd(svc *corev1.Service) error {
	return server.addServiceCheck(svc)
}

func (server *Server) onServiceUpdate(svc *corev1.Service) error {
	//return server.syncDNSRecordAsWhole()
	return nil
}

func (server *Server) onServiceDelete(svc *corev1.Service) error {
	//return server.syncDNSRecordAsWhole()
	return nil
}

func (server *Server) onIngressAdd(ing *networkingv1.Ingress) error {
	//return server.syncDNSRecordAsWhole()
	return nil
}

func (server *Server) onIngressUpdate(ing *networkingv1.Ingress) error {
	//return server.syncDNSRecordAsWhole()
	return nil
}

func (server *Server) onIngressDelete(ing *networkingv1.Ingress) error {
	//return server.syncDNSRecordAsWhole()
	return nil
}

func (server *Server) addServiceCheck(svc *corev1.Service) error {
	checkPeriod := svc.Annotations[constant.UbinurseKeyInterval]
	checkProtocal := svc.Annotations[constant.UbinurseKeyProtocal]
	checkPath := svc.Annotations[constant.UbinurseKeyPath]
	checkPort := svc.Annotations[constant.UbinurseKeyPort]
	period, err := strconv.Atoi(checkPeriod)
	if err != nil {
		return err
	}
	svcName := svc.Name
	svcNamespace := svc.Namespace
	stop := make(chan struct{})
	diagnose := &Diagnose{
		CheckType:     constant.TypeService,
		CheckProtocal: checkProtocal,
		CheckEndpoint: fmt.Sprintf("%s.%s.svc:%s%s", svcName, svcNamespace, checkPort, checkPath),
	}

	go server.startCheckService(diagnose, period, stop)
	return nil
	// go wait.Until(func() {
	// 	if err := dnsctl.syncDNSRecordAsWhole(); err != nil {
	// 		klog.Errorf("failed to sync dns record, %v", err)
	// 	}
	// }, time.Duration(period)*time.Second, stop)
}

func (server *Server) startCheckService(diagnose *Diagnose, period int, stopCh <-chan struct{}) {
	ticker := time.NewTicker(time.Duration(period) * time.Second)
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			body, err := json.Marshal(diagnose)
			if err != nil {
				klog.Errorf("failed to check service endpoint: %s with err: %v", diagnose.CheckEndpoint, err)
				break
			}
			req, err := http.NewRequest(http.MethodPost, constant.UbinurseHTTPServer, bytes.NewReader(body))
			if err != nil {
				klog.Errorf("failed to check service endpoint: %s with err: %v", diagnose.CheckEndpoint, err)
				break
			}
			req.Header.Set("Content-Type", "application/json")
			client := http.Client{Timeout: 10 * time.Second}
			_, err = client.Do(req)
			if err != nil {
				klog.Errorf("failed to check service endpoint: %s with err: %v", diagnose.CheckEndpoint, err)
				break
			}
		}
	}
}

func (server *Server) handleErr(err error, event interface{}) {
	if err == nil {
		server.queue.Forget(event)
		return
	}

	if server.queue.NumRequeues(event) < maxRetries {
		klog.Infof("error syncing event %v: %v", event, err)
		server.queue.AddRateLimited(event)
		return
	}

	utilruntime.HandleError(err)
	klog.Infof("dropping event %q out of the queue: %v", event, err)
	server.queue.Forget(event)
}

func (server *Server) addService(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return
	}
	if svc.Annotations["ubinurse.ubiquant.com/port"] == "" || svc.Annotations["ubinurse.ubiquant.com/path"] == "" || svc.Annotations["ubinurse.ubiquant.com/protocal"] == "" {
		klog.V(2).Infof("service add event for %v/%v without ubinurse enable", svc.Namespace, svc.Name)
		return
	}
	klog.V(2).Infof("enqueue service add event for %v/%v", svc.Namespace, svc.Name)
	server.enqueue(svc, ServiceAdd)
}

func (server *Server) updateService(oldObj, newObj interface{}) {
	// TODO: add logic
	// svc, ok := obj.(*corev1.Service)
	// if !ok {
	// 	return
	// }
	// if svc.Annotations["ubinurse.ubiquant.com/port"] == "" || svc.Annotations["ubinurse.ubiquant.com/path"] == "" || svc.Annotations["ubinurse.ubiquant.com/protocal"] == "" {
	// 	return
	// }
	// klog.V(2).Infof("enqueue service add event for %v/%v", svc.Namespace, svc.Name)
	// server.enqueue(svc, ServiceUpdate)
}

func (server *Server) deleteService(obj interface{}) {
	// TODO: add logic
	// svc, ok := obj.(*corev1.Service)
	// if !ok {
	// 	return
	// }
	// if svc.Annotations["ubinurse.ubiquant.com/port"] == "" || svc.Annotations["ubinurse.ubiquant.com/path"] == "" || svc.Annotations["ubinurse.ubiquant.com/protocal"] == "" {
	// 	return
	// }
	// klog.V(2).Infof("enqueue service add event for %v/%v", svc.Namespace, svc.Name)
	// server.enqueue(svc, ServiceUpdate)
}

func (server *Server) addIngress(obj interface{}) {
	ing, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return
	}
	if ing.Annotations["ubinurse.ubiquant.com/port"] == "" || ing.Annotations["ubinurse.ubiquant.com/path"] == "" || ing.Annotations["ubinurse.ubiquant.com/protocal"] == "" {
		return
	}
	klog.V(2).Infof("enqueue ingress add event for %v/%v", ing.Namespace, ing.Name)
	server.enqueue(ing, IngressAdd)
}

func (server *Server) updateIngress(oldObj, newObj interface{}) {
	// TODO: add logic
	// svc, ok := obj.(*corev1.Service)
	// if !ok {
	// 	return
	// }
	// if svc.Annotations["ubinurse.ubiquant.com/port"] == "" || svc.Annotations["ubinurse.ubiquant.com/path"] == "" || svc.Annotations["ubinurse.ubiquant.com/protocal"] == "" {
	// 	return
	// }
	// klog.V(2).Infof("enqueue service add event for %v/%v", svc.Namespace, svc.Name)
	// server.enqueue(svc, ServiceUpdate)
}

func (server *Server) deleteIngress(obj interface{}) {
	// TODO: add logic
	// svc, ok := obj.(*corev1.Service)
	// if !ok {
	// 	return
	// }
	// if svc.Annotations["ubinurse.ubiquant.com/port"] == "" || svc.Annotations["ubinurse.ubiquant.com/path"] == "" || svc.Annotations["ubinurse.ubiquant.com/protocal"] == "" {
	// 	return
	// }
	// klog.V(2).Infof("enqueue service add event for %v/%v", svc.Namespace, svc.Name)
	// server.enqueue(svc, ServiceUpdate)
}

func (server *Server) enqueue(obj interface{}, eventType EventType) {
	e := &Event{
		Obj:  obj,
		Type: eventType,
	}
	server.queue.Add(e)
}
