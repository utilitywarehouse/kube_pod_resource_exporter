package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func newKubeClient(kubeContext string) (*kubernetes.Clientset, error) {
	var kubeConfig *rest.Config
	var err error
	if kubeContext == "" {
		kubeConfig, err = rest.InClusterConfig()
	} else {
		kubeConfig, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{
				CurrentContext: kubeContext,
			},
		).ClientConfig()
	}
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(kubeConfig)
}

func runOnce(kc *kubernetes.Clientset) {
	pods, err := kc.CoreV1().Pods("").List(metav1.ListOptions{})
	if err != nil {
		log.Printf("[ERROR] %+v", err)
		return
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			mContainerCPU.WithLabelValues("request", pod.Namespace, pod.Name, container.Name).Set(float64(container.Resources.Requests.Cpu().MilliValue()))
			mContainerMem.WithLabelValues("request", pod.Namespace, pod.Name, container.Name).Set(float64(container.Resources.Requests.Memory().MilliValue() / 1000))
			mContainerCPU.WithLabelValues("limit", pod.Namespace, pod.Name, container.Name).Set(float64(container.Resources.Limits.Cpu().MilliValue()))
			mContainerMem.WithLabelValues("limit", pod.Namespace, pod.Name, container.Name).Set(float64(container.Resources.Limits.Memory().MilliValue() / 1000))
		}
	}
}

var (
	kubeContext    = flag.String("kube.context", "", "kubernetes context to use when running locally (leave empty for in-cluster configuration)")
	scrapeInterval = flag.Int64("scrape.interval", 3600, "the scrape interval in seconds")

	mContainerCPU = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "container",
			Subsystem: "resources",
			Name:      "cpu_milli",
			Help:      "Containre CPU resources in millicpus",
		},
		[]string{"type", "namespace", "pod_name", "container_name"},
	)

	mContainerMem = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "container",
			Subsystem: "resources",
			Name:      "memory_bytes",
			Help:      "Container memory resources in bytes",
		},
		[]string{"type", "namespace", "pod_name", "container_name"},
	)
)

func init() {
	prometheus.MustRegister(mContainerCPU)
	prometheus.MustRegister(mContainerMem)
}

func main() {
	flag.Parse()
	kc, err := newKubeClient(*kubeContext)
	if err != nil {
		log.Fatalf("[ERROR] %+v", err)
	}
	scrapeTicker := time.NewTicker(time.Duration(*scrapeInterval) * time.Second)
	go func() {
		runOnce(kc)
		for range scrapeTicker.C {
			runOnce(kc)
		}
	}()
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	sigChannel := make(chan os.Signal, 1)
	signal.Notify(sigChannel, os.Interrupt)
	go func() {
		<-sigChannel
		log.Printf("[INFO] interrupt singal received")
		scrapeTicker.Stop()
		server.Shutdown(context.TODO())
	}()
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("[ERROR] server failed: %+v", err)
	}
}
