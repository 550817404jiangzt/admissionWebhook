### Kubernetes 集群部署 Validating Webhook

##### 部署前提条件

- Kubernetes 集群版本至少为 v1.16
- 启用了 ValidatingAdmissionWebhook 控制器

```powershell
#检查集群信息
kubectl version
kubectl get pods `kubectl get po  -n kube-system|grep kube-apiserver|awk '{print $1}'` -n kube-system -o yaml | grep ValidatingAdmissionWebhook
kubectl api-versions | grep admission
#当出现admissionregistration.k8s.io说明集群支持，进行下一步。
```

* 开启ValidatingAdmissionWebhook 控制器方法

```powershell
#编辑/etc/kubernetes/manifests/kube-apiserver.yaml
......
spec:
  containers:
  - command:
    - kube-apiserver
......
    - --enable-admission-plugins=NodeRestriction,MutatingAdmissionWebhook,ValidatingAdmissionWebhook
......
```

##### 上传镜像

```powershell
docker load -i admission-webhook_v2.tar
docker images |grep webhook
#默认的镜像名称registry.hundsun.com/hcs/admission-webhook:v2
#如果使用的是其他的registry中 比如docker.io，做如下retag操作
docker tag registry.hundsun.com/hcs/admission-webhook:v2  docker.io/hundsun/admission-webhook:v2
```

##### 准备证书

* 因为 Admission Webhook 只允许 https 协议并且需要提供证书信息，所以需要我们提前准备并生成

```powershell
#确认jq工具是否安装
which jq
#未安装的话，安装jq工具，解压jq.tar.gz
tar -xzvf jq.tar.gz -C /usr/local/bin/
```

* 利用脚本(istio团队提供的)生成CertificateSigningRequest，再生成secret(给后面的webhook-api使用)

```powershell
chmod +x ./webhook-create-signed-cert.sh ./webhook-patch-ca-bundle.sh
sh ./webhook-create-signed-cert.sh
# 配置 ValidatingWebhookConfiguration,该脚本运行依赖于 jq，并且对脚本webhook-patch-ca-bundle.sh做了局部修改
# select(.name == "kubernetes")
cat ./validatingwebhook.yaml | ./webhook-patch-ca-bundle.sh > ./validatingwebhook-ca-bundle.yaml
# 执行完成后，可以看到 validatingwebhook-ca-bundle.yaml 的 caBundle 字段已经被替换。
```

##### 部署webhook-api

* 证书创建成功后，部署configmap、 Deployment 和 Services

```powershell
#admission-webhook的相关资源部署在kube-system中
#deployment.yaml中默认的镜像名称registry.hundsun.com/hcs/admission-webhook:v2
#如果需要使用其他registry的话，需要手工修改deployment.yaml中image属性
kubectl create -f validatingwebhook-ca-bundle.yaml
kubectl create -f configmap.yaml
kubectl create -f deployment.yaml
kubectl create -f service.yaml
```

##### 端口配置说明

```powershell
#更新完configmap.yaml，执行kubectl apply -f configmap.yaml之后，会有一定的更新延迟，延迟时间取决于kubelet的sync-frequency配置，默认值是1min
#cat configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: admission-webhook-example-configmap
data:
  config.yaml: |
    rejectivePorts: 22,123,3306,80,443,9099,8081,4822,8822,8823,4567,4568,4444,843,8820,8824,18881,18882,18822,18086,18088,18005,6443,10256,10249,2379,2380,10255,10250,10251,10252,27812,12305,18989,4443,1514 #需要过滤的端口以英文逗号分隔
```

