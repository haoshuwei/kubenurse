package informers

import (
	"k8s.io/client-go/informers"
)

func RegisterInformersForUbinurseWatcher(informerFactory informers.SharedInformerFactory) {
	informerFactory.Core().V1().Services()
	informerFactory.Networking().V1().Ingresses()
}
