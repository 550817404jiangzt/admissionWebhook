package webhook

import (
    "k8s.io/api/admission/v1beta1"
    "net/http"
)

type WebHookServerInt interface {
    mutating(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse
    validating(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse
    Start()
    Stop()
}

// Webhook Server parameters
type WebHookServerParameters struct {
    Port           int    // webhook server port
    CertFile       string // path to the x509 certificate for https
    KeyFile        string // path to the x509 private key matching `CertFile`
    CfgFile        string
}

type webHookServer struct {
    server *http.Server
    configFile string
    config    *Config
}

type Config struct {
    RejectivePorts string `yaml:"rejectivePorts"`
    IngressValidate []IngressValidate `yaml:"ingressValidate"`
}

type IngressValidate struct {
    Namespace string `yaml:"namespace"`
    Host string `yaml:"host"`
}