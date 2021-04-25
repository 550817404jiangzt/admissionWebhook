package webhook

import (
    "context"
    "crypto/sha256"
    "crypto/tls"
    "encoding/json"
    "fmt"
    "github.com/ghodss/yaml"
    "github.com/golang/glog"
    "io/ioutil"
    "k8s.io/api/admission/v1beta1"
    admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
    corev1 "k8s.io/api/core/v1"
    extensions_v1beta1 "k8s.io/api/extensions/v1beta1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/serializer"
    "k8s.io/kubernetes/pkg/apis/core/v1"
    "net/http"
    "strconv"
    "strings"
    "sync"
)

//const (
//    admissionWebhookAnnotationValidateKey = "admission-webhook.hundsun.com/validate"
//)

var (
    once   sync.Once
    ws     *webHookServer
    err    error
)

var (
    runtimeScheme = runtime.NewScheme()
    codecs        = serializer.NewCodecFactory(runtimeScheme)
    deserializer  = codecs.UniversalDeserializer()
    ignoredNamespaces = []string{""}
)

func init() {
    _ = corev1.AddToScheme(runtimeScheme)
    _ = admissionregistrationv1beta1.AddToScheme(runtimeScheme)
    _ = v1.AddToScheme(runtimeScheme)
}

func NewWebhookServer(webHook WebHookServerParameters) (WebHookServerInt, error) {
    once.Do(func() {
        ws, err = newWebHookServer(webHook)
    })
    return ws, err
}

func newWebHookServer(webHook WebHookServerParameters) (*webHookServer, error) {
    // load tls cert/key file
    tlsCertKey, err := tls.LoadX509KeyPair(webHook.CertFile, webHook.KeyFile)
    if err != nil {
        return nil, err
    }

    ws := &webHookServer{
        server: &http.Server{
            Addr:      fmt.Sprintf(":%v", webHook.Port),
            TLSConfig: &tls.Config{Certificates: []tls.Certificate{tlsCertKey}},
        },
    }

    // add routes
    mux := http.NewServeMux()
    mux.HandleFunc("/mutating", ws.serve)
    mux.HandleFunc("/validating", ws.serve)
    ws.server.Handler = mux
    ws.configFile = webHook.CfgFile
    // ws.config = config
    return ws, nil
}


func (ws *webHookServer) Start() {
    if err := ws.server.ListenAndServeTLS("", ""); err != nil {
        glog.Errorf("Failed to listen and serve webhook server: %v", err)
    }
}

func (ws *webHookServer) Stop() {
    glog.Infof("Got OS shutdown signal, shutting down wenhook server gracefully...")
    ws.server.Shutdown(context.Background())
}

// validate deployments and services
func (whsvr *webHookServer) validating(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
    req := ar.Request
    allowed := true
    var result *metav1.Status
    var (
        svcType                         corev1.ServiceType
        Ports                           []corev1.ServicePort
        resourceNamespace, resourceName string
    )

    glog.Infof("AdmissionReview1 for Kind=%v, Namespace=%v Name=%v (%v) UID=%v patchOperation=%v UserInfo=%v",
        req.Kind, req.Namespace, req.Name, req.UID, req.Operation, req.UserInfo)
    config, err := loadConfig(ws.configFile)
    if err != nil {
        glog.Errorf("Could not open file: %v", err)
        return &v1beta1.AdmissionResponse{
            Result: &metav1.Status{
                Message: err.Error(),
            },
        }
    }
    switch req.Kind.Kind {
    case "Service":
        var service corev1.Service
        if err := json.Unmarshal(req.Object.Raw, &service); err != nil {
            glog.Errorf("Could not unmarshal raw object: %v", err)
            return &v1beta1.AdmissionResponse{
                Result: &metav1.Status{
                    Message: err.Error(),
                },
            }
        }
        svcType = service.Spec.Type
        Ports = service.Spec.Ports
        glog.Infof("Service Type: %s", svcType)
        if svcType == "NodePort" {
            //不允许使用的端口列表
            rejectivePortsSplit := strings.Split(config.RejectivePorts, ",")
            if len(rejectivePortsSplit) >0 {
                endfor := false
                for _, port := range Ports {
                    nodePortStr := strconv.FormatInt(int64(port.NodePort), 10)
                    for _, rejectivePortStr := range rejectivePortsSplit {
                        glog.Infof("nodePortStr: %s, rejectivePortStr: %s", nodePortStr, rejectivePortStr)
                        if ret := strings.Compare(nodePortStr,rejectivePortStr); ret == 0 {
                            glog.Infof("nodePort " + rejectivePortStr + " is not allowed")
                            sr := metav1.StatusReason("nodePort " + rejectivePortStr + " is not allowed")
                            allowed = false
                            result = &metav1.Status{
                                Reason: sr ,
                            }
                            endfor = true
                            break
                        }
                    }
                    if endfor {
                        break
                    }
                }
            }
        }
    case "Ingress":
        var ingress extensions_v1beta1.Ingress
        glog.Infof("kind: Ingress")
        if err := json.Unmarshal(req.Object.Raw, &ingress); err != nil {
            glog.Errorf("Could not unmarshal raw object: %v", err)
            return &v1beta1.AdmissionResponse{
                Result: &metav1.Status{
                    Message: err.Error(),
                },
            }
        }
        resourceName, resourceNamespace = ingress.Name, ingress.Namespace
        // modified by jiangzt@hundsun.com at 20210424
        if !validationRequired(config.IngressValidate, ingress) {
            glog.Infof("Skipping validation for %s/%s due to policy check", resourceNamespace, resourceName)
            return &v1beta1.AdmissionResponse{
                Allowed: true,
            }
        }
        sr := metav1.StatusReason("Illegal host, "+ resourceName +"  for it's in special namespace:"+resourceNamespace+" is not allowed")
        allowed = false
        result = &metav1.Status{
            Reason: sr ,
        }
    }

    return &v1beta1.AdmissionResponse{
        Allowed: allowed,
        Result:  result,
    }
}

// main mutation process
func (whsvr *webHookServer) mutating(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
    req := ar.Request
    glog.Infof("Skipping validation for %s/%s due to policy check", req.Namespace, req.Name)
    return &v1beta1.AdmissionResponse{
        Allowed: true,
    }
}

// Serve method for webhook server
func (whsvr *webHookServer) serve(w http.ResponseWriter, r *http.Request) {
    var body []byte
    if r.Body != nil {
        if data, err := ioutil.ReadAll(r.Body); err == nil {
            body = data
        }
    }
    if len(body) == 0 {
        glog.Error("empty body")
        http.Error(w, "empty body", http.StatusBadRequest)
        return
    }

    // verify the content type is accurate
    contentType := r.Header.Get("Content-Type")
    if contentType != "application/json" {
        glog.Errorf("Content-Type=%s, expect application/json", contentType)
        http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
        return
    }

    var admissionResponse *v1beta1.AdmissionResponse
    ar := v1beta1.AdmissionReview{}
    if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
        glog.Errorf("Can't decode body: %v", err)
        admissionResponse = &v1beta1.AdmissionResponse{
            Result: &metav1.Status{
                Message: err.Error(),
            },
        }
    } else {
        fmt.Println(r.URL.Path)
        if r.URL.Path == "/mutating" {
            admissionResponse = whsvr.mutating(&ar)
        } else if r.URL.Path == "/validating" {
            admissionResponse = whsvr.validating(&ar)
        }
    }

    admissionReview := v1beta1.AdmissionReview{}
    if admissionResponse != nil {
        admissionReview.Response = admissionResponse
        if ar.Request != nil {
            admissionReview.Response.UID = ar.Request.UID
        }
    }

    resp, err := json.Marshal(admissionReview)
    if err != nil {
        glog.Errorf("Can't encode response: %v", err)
        http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
    }
    glog.Infof("Ready to write reponse ...")
    if _, err := w.Write(resp); err != nil {
        glog.Errorf("Can't write response: %v", err)
        http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
    }
}

func loadConfig(configFile string) (*Config, error) {
    data, err := ioutil.ReadFile(configFile)
    if err != nil {
        return nil, err
    }
    glog.Infof("New configuration: sha256sum %x", sha256.Sum256(data))

    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, err
    }

    return &cfg, nil
}

// added by jiangzt@hundsun.com at 2020.12.30
func validationRequired(ignoredList []IngressValidate, ingress extensions_v1beta1.Ingress) bool {
    required := admissionRequired(ignoredList, ingress)
    glog.Infof("Validation policy for %v/%v: required:%v", ingress.Namespace, ingress.Name, required)
    return required
}

// added by jiangzt@hundsun.com at 2020.12.30
// modifed by jiangzt@hundsun.com at 2021.04.25 去掉对Annotations的处理支持
func admissionRequired(ignoredList []IngressValidate, ingress extensions_v1beta1.Ingress) bool {
    //只允许配置的ingress在配置的Namespace中创建,不允许在其他的namespace中创建,
    //例如：dev.hundsun.com 只能创建在hep-saas中，其他namespace不允许创建dev.hundsun.com
    ingressRules := ingress.Spec.Rules
    for _, ingressValidate := range ignoredList {
        for _, ingressRule := range ingressRules {
            if ingressRule.Host == ingressValidate.Host {
                if ingress.Namespace == ingressValidate.Namespace {
                    glog.Infof("Skip validation host [%v] for %v for it's in special namespace:%v", ingressRule.Host, ingress.Name, ingress.Namespace)
                    return false
                } else {
                    glog.Infof("Illegal host [%v] for %v for it's in special namespace:%v", ingressRule.Host, ingress.Name, ingress.Namespace)
                }
            } else {
                glog.Infof("Skip validation host [%v] for %v for it's in special namespace:%v", ingressRule.Host, ingress.Name, ingress.Namespace)
                return false
            }
        }
    }
    //annotations := metadata.GetAnnotations()
    //if annotations == nil {
    //   annotations = map[string]string{}
    //}
    //
    //var required bool
    //switch strings.ToLower(annotations[admissionAnnotationKey]) {
    //default:
    //   required = true
    //case "n", "no", "false", "off":
    //   required = false
    //}
    //return required
    return true
}
