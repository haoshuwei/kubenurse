.PHONY: kubenurse
kubenurse:
	docker build -t registry-vpc.cn-hongkong.aliyuncs.com/haoshuwei/kubenurse . -f Dockerfile
	docker push registry-vpc.cn-hongkong.aliyuncs.com/haoshuwei/kubenurse

.PHONY: watcher
watcher:
	docker build -t registry-vpc.cn-hongkong.aliyuncs.com/haoshuwei/kubenurse-watcher . -f Dockerfile.watcher
	docker push registry-vpc.cn-hongkong.aliyuncs.com/haoshuwei/kubenurse-watcher