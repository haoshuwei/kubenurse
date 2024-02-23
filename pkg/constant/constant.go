package constant

const (
	UbinurseKeyProtocal  = "ubinurse.ubiquant.com/protocal"
	UbinurseKeyPort      = "ubinurse.ubiquant.com/port"
	UbinurseKeyPath      = "ubinurse.ubiquant.com/path"
	UbinurseKeyInterval  = "ubinurse.ubiquant.com/interval"
	UbinurseKeyDisable   = "ubinurse.ubiquant.com/disable"
	UbinurseKeyInCluster = "ubinurse.ubiquant.com/inCluster"
	UbinurseKeyHost      = "ubinurse.ubiquant.com/host"

	TypeService = "service"
	TypeIngress = "ingress"

	UbinurseHTTPServer = "http://ubinurse:8080/diagnose"

	Pattern = `^((http://)|(https://))?([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,6}`
)
