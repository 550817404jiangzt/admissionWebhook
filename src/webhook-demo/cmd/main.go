package main

import (
    "flag"
    "github.com/golang/glog"
    "github.com/yaoice/webhook-demo/pkg/webhook"
    "os"
    "os/signal"
    "syscall"
)

var webHook webhook.WebHookServerParameters

func main() {
    // parse parameters
    flag.Parse()

    // init webhook api
    ws, err := webhook.NewWebhookServer(webHook)
    if err != nil {
        panic(err)
    }

    // start webhook server in new routine
    go ws.Start()
    glog.Info("Server started")

    signalChan := make(chan os.Signal, 1)
    signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
    <-signalChan

    ws.Stop()
}

func init() {
    // read parameters
    flag.IntVar(&webHook.Port, "port", 443, "The port of webhook server to listen.")
    flag.StringVar(&webHook.CertFile, "tlsCertPath", "/etc/webhook/certs/cert.pem", "The path of tls cert")
    flag.StringVar(&webHook.KeyFile, "tlsKeyPath", "/etc/webhook/certs/key.pem", "The path of tls key")
    flag.StringVar(&webHook.CfgFile, "cfgFile", "/etc/webhook/config/config.yaml", "File containing the ports configuration.")
}


