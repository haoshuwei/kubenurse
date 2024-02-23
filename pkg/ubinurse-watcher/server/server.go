package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	cachev2 "github.com/patrickmn/go-cache"
	"github.com/postfinance/kubenurse/pkg/constant"
	cachev1 "github.com/postfinance/kubenurse/pkg/ubinurse-watcher/cache"
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
	cacheManager          *cachev1.CacheManager
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
	CheckType             string `json:"check_type"`
	CheckProtocal         string `json:"check_protocal"`
	CheckEndpoint         string `json:"check_dst_endpoint"`
	CheckIngressIncluster bool   `json:"check_ingress_in_cluster"`
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

	server.cacheManager = cachev1.NewCacheManager()

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
	return server.updateServiceCheck(svc)
}

func (server *Server) onServiceDelete(svc *corev1.Service) error {
	return server.deleteServiceCheck(svc)
}

func (server *Server) onIngressAdd(ing *networkingv1.Ingress) error {
	return server.addIngressCheck(ing)
}

func (server *Server) onIngressUpdate(ing *networkingv1.Ingress) error {
	return server.updateIngressCheck(ing)
}

func (server *Server) onIngressDelete(ing *networkingv1.Ingress) error {
	return server.deleteIngressCheck(ing)
}

func (server *Server) addService(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return
	}
	if svc.Annotations["ubinurse.ubiquant.com/port"] == "" || svc.Annotations["ubinurse.ubiquant.com/path"] == "" || svc.Annotations["ubinurse.ubiquant.com/protocal"] == "" {
		klog.Infof("service add event for %v/%v without ubinurse enable", svc.Namespace, svc.Name)
		return
	}
	klog.Infof("enqueue service add event for %v/%v", svc.Namespace, svc.Name)
	server.enqueue(svc, ServiceAdd)
}

func (server *Server) updateService(oldObj, newObj interface{}) {
	oldSvc, ok := oldObj.(*corev1.Service)
	if !ok {
		return
	}
	newSvc, ok := newObj.(*corev1.Service)
	if !ok {
		return
	}

	oldProtocal := oldSvc.Annotations[constant.UbinurseKeyProtocal]
	oldPort := oldSvc.Annotations[constant.UbinurseKeyPort]
	oldPath := oldSvc.Annotations[constant.UbinurseKeyPath]
	oldInterval := oldSvc.Annotations[constant.UbinurseKeyInterval]

	newProtocal := newSvc.Annotations[constant.UbinurseKeyProtocal]
	newPort := newSvc.Annotations[constant.UbinurseKeyPort]
	newPath := newSvc.Annotations[constant.UbinurseKeyPath]
	newInterval := newSvc.Annotations[constant.UbinurseKeyInterval]

	if oldProtocal != "" && oldPort != "" && oldPath != "" && oldInterval != "" {
		// skip
		if oldProtocal == newProtocal && oldPort == newPort && oldPath == newPath && oldInterval == newInterval {
			return
		}
		// should stop old goroutine
		if newProtocal == "" || newPort == "" || newPath == "" || newInterval == "" {
			newSvc.Annotations[constant.UbinurseKeyDisable] = "true"
		}
	} else {
		// should skip stop old goroutine
		if newProtocal == "" || newPort == "" || newPath == "" || newInterval == "" {
			return
		}
	}
	klog.Infof("enqueue service update event for %v/%v", newSvc.Namespace, newSvc.Name)
	server.enqueue(newSvc, ServiceUpdate)
}

func (server *Server) deleteService(obj interface{}) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return
	}
	if svc.Annotations["ubinurse.ubiquant.com/port"] == "" || svc.Annotations["ubinurse.ubiquant.com/path"] == "" || svc.Annotations["ubinurse.ubiquant.com/protocal"] == "" {
		klog.Infof("service add event for %v/%v without ubinurse enable", svc.Namespace, svc.Name)
		return
	}
	klog.Infof("enqueue service delete event for %v/%v", svc.Namespace, svc.Name)
	server.enqueue(svc, ServiceDelete)
}

func (server *Server) addIngress(obj interface{}) {
	ing, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return
	}
	if ing.Annotations[constant.UbinurseKeyHost] == "" || ing.Annotations[constant.UbinurseKeyPath] == "" || ing.Annotations[constant.UbinurseKeyInterval] == "" {
		return
	}
	klog.Infof("enqueue ingress add event for %v/%v", ing.Namespace, ing.Name)
	server.enqueue(ing, IngressAdd)
}

func (server *Server) updateIngress(oldObj, newObj interface{}) {
	oldIng, ok := oldObj.(*networkingv1.Ingress)
	if !ok {
		return
	}
	newIng, ok := newObj.(*networkingv1.Ingress)
	if !ok {
		return
	}

	oldHost := oldIng.Annotations[constant.UbinurseKeyHost]
	oldPath := oldIng.Annotations[constant.UbinurseKeyPath]
	oldInterval := oldIng.Annotations[constant.UbinurseKeyInterval]

	newHost := newIng.Annotations[constant.UbinurseKeyHost]
	newPath := newIng.Annotations[constant.UbinurseKeyPath]
	newInterval := newIng.Annotations[constant.UbinurseKeyInterval]

	if oldHost != "" && oldPath != "" && oldInterval != "" {
		// skip
		if oldHost == newHost && oldPath == newPath && oldInterval == newInterval {
			return
		}
		// should stop old goroutine
		if newHost == "" || newPath == "" || newInterval == "" {
			newIng.Annotations[constant.UbinurseKeyDisable] = "true"
		}
	} else {
		// should skip stop old goroutine
		if newHost == "" || newPath == "" || newInterval == "" {
			return
		}
	}
	klog.Infof("enqueue ingress update event for %v/%v", newIng.Namespace, newIng.Name)
	server.enqueue(newIng, IngressUpdate)
}

func (server *Server) deleteIngress(obj interface{}) {
	ing, ok := obj.(*networkingv1.Ingress)
	if !ok {
		return
	}
	if ing.Annotations[constant.UbinurseKeyHost] == "" || ing.Annotations[constant.UbinurseKeyPath] == "" || ing.Annotations[constant.UbinurseKeyInterval] == "" {
		return
	}
	klog.Infof("enqueue ingress delete event for %v/%v", ing.Namespace, ing.Name)
	server.enqueue(ing, IngressDelete)
}

func (server *Server) enqueue(obj interface{}, eventType EventType) {
	e := &Event{
		Obj:  obj,
		Type: eventType,
	}
	server.queue.Add(e)
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
	ep := fmt.Sprintf("%s.%s.svc:%s%s", svcName, svcNamespace, checkPort, checkPath)
	diagnose := &Diagnose{
		CheckType:     constant.TypeService,
		CheckProtocal: checkProtocal,
		CheckEndpoint: ep,
	}
	channelName := fmt.Sprintf("%s-%s-svc", svcNamespace, svcName)
	stopCh := make(chan bool)
	server.cacheManager.Cache.Set(channelName, stopCh, cachev2.NoExpiration)

	go server.startCheckService(diagnose, period, stopCh)
	return nil
}

func (server *Server) updateServiceCheck(svc *corev1.Service) error {
	checkDisable := svc.Annotations[constant.UbinurseKeyDisable]
	server.deleteServiceCheck(svc)
	// only delete service check
	if checkDisable == "true" {
		return nil
	}
	return server.addServiceCheck(svc)
}

func (server *Server) deleteServiceCheck(svc *corev1.Service) error {
	svcName := svc.Name
	svcNamespace := svc.Namespace

	channelName := fmt.Sprintf("%s-%s-svc", svcNamespace, svcName)
	stopChFromCache, found := server.cacheManager.Cache.Get(channelName)
	if !found {
		return nil
	}
	stopCh, ok := stopChFromCache.(chan bool)
	if !ok {
		klog.Errorf("failed to get stopCh form cache")
		return nil
	}
	stopCh <- true
	server.cacheManager.Cache.Delete(channelName)
	return nil
}

func (server *Server) startCheckService(diagnose *Diagnose, period int, stopCh <-chan bool) {
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
			}
			klog.Infof("check service endpoint: %s", diagnose.CheckEndpoint)
		}
	}
}

func (server *Server) addIngressCheck(ing *networkingv1.Ingress) error {
	var checkProtocal string
	var inClusterIngress bool
	checkPeriod := ing.Annotations[constant.UbinurseKeyInterval]
	checkHost := ing.Annotations[constant.UbinurseKeyHost]
	checkPath := ing.Annotations[constant.UbinurseKeyPath]
	checkInCluster := ing.Annotations[constant.UbinurseKeyInCluster]

	if checkInCluster == "" {
		checkInCluster = "true"
	}
	period, err := strconv.Atoi(checkPeriod)
	if err != nil {
		return err
	}
	ingName := ing.Name
	ingNamespace := ing.Namespace
	if checkInCluster == "true" {
		checkProtocal = "http"
		inClusterIngress = true
	} else {
		checkProtocal = "https"
		inClusterIngress = false
	}
	ep := fmt.Sprintf("%s%s", checkHost, checkPath)
	diagnose := &Diagnose{
		CheckType:             constant.TypeIngress,
		CheckProtocal:         checkProtocal,
		CheckEndpoint:         ep,
		CheckIngressIncluster: inClusterIngress,
	}
	channelName := fmt.Sprintf("%s-%s-ing", ingNamespace, ingName)
	stopCh := make(chan bool)
	server.cacheManager.Cache.Set(channelName, stopCh, cachev2.NoExpiration)

	go server.startCheckIngress(diagnose, period, stopCh)
	return nil
}

func (server *Server) updateIngressCheck(ing *networkingv1.Ingress) error {
	checkDisable := ing.Annotations[constant.UbinurseKeyDisable]
	server.deleteIngressCheck(ing)
	// only delete service check
	if checkDisable == "true" {
		return nil
	}
	return server.addIngressCheck(ing)
}

func (server *Server) deleteIngressCheck(ing *networkingv1.Ingress) error {
	ingName := ing.Name
	ingNamespace := ing.Namespace

	channelName := fmt.Sprintf("%s-%s-ing", ingNamespace, ingName)
	stopChFromCache, found := server.cacheManager.Cache.Get(channelName)
	if !found {
		return nil
	}
	stopCh, ok := stopChFromCache.(chan bool)
	if !ok {
		klog.Errorf("failed to get stopCh form cache")
		return nil
	}
	stopCh <- true
	server.cacheManager.Cache.Delete(channelName)
	return nil
}

func (server *Server) startCheckIngress(diagnose *Diagnose, period int, stopCh <-chan bool) {
	ticker := time.NewTicker(time.Duration(period) * time.Second)
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			body, err := json.Marshal(diagnose)
			if err != nil {
				klog.Errorf("failed to check ingress endpoint: %s with err: %v", diagnose.CheckEndpoint, err)
				break
			}
			req, err := http.NewRequest(http.MethodPost, constant.UbinurseHTTPServer, bytes.NewReader(body))
			if err != nil {
				klog.Errorf("failed to check ingress endpoint: %s with err: %v", diagnose.CheckEndpoint, err)
				break
			}
			req.Header.Set("Content-Type", "application/json")
			client := http.Client{Timeout: 5 * time.Second}
			_, err = client.Do(req)
			if err != nil {
				klog.Errorf("failed to check ingress endpoint: %s with err: %v", diagnose.CheckEndpoint, err)
			}
			klog.Infof("check ingress endpoint: %s", diagnose.CheckEndpoint)
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
