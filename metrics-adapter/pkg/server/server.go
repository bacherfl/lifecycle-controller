package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/benbjohnson/clock"
	"github.com/gorilla/mux"
	metricsv1alpha1 "github.com/keptn/lifecycle-toolkit/metrics-operator/api/v1alpha1"
	"github.com/open-feature/go-sdk/pkg/openfeature"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"net/http"
	"os"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"strings"
	"sync"
	"time"
)

var instance *serverManager
var once sync.Once

type serverManager struct {
	server   *http.Server
	ticker   *clock.Ticker
	ofClient *openfeature.Client
}

func StartServerManager(ctx context.Context) {
	once.Do(func() {
		instance = &serverManager{
			ticker:   clock.New().Ticker(10 * time.Second),
			ofClient: openfeature.NewClient("klt"),
		}
		instance.Start(ctx)
	})
}

func (m *serverManager) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				if err := m.ShutDownServer(); err != nil {
					klog.Errorf("Error during server shutdown: %v", err)
				}
				return
			case <-m.ticker.C:
				if err := m.setup(); err != nil {
					klog.Errorf("Error during server setup: %v", err)
				}
			}
		}
	}()
}

func (m *serverManager) ShutDownServer() error {
	defer func() {
		m.server = nil
	}()
	if m.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		return m.server.Shutdown(ctx)
	}
	return nil
}

func (m *serverManager) setup() error {
	maxRetries := 3
	var serverEnabled bool
	var err error

	klog.Infof("Checking configuration of metrics server")

	for i := 0; i < maxRetries; i++ {
		serverEnabled, err = m.ofClient.BooleanValue(context.TODO(), "enable-metrics-server", false, openfeature.EvaluationContext{})
		if err == nil {
			break
		}

		if strings.Contains(err.Error(), string(openfeature.ProviderNotReadyCode)) {
			<-time.After(2 * time.Second)
			continue
		}
		break
	}

	klog.Infof("Metrics server enabled: %v", serverEnabled)

	if serverEnabled && m.server == nil {
		klog.Infof("serving metrics at localhost:9999/metrics")

		router := mux.NewRouter()
		router.Path("/metrics").Handler(promhttp.Handler())
		router.Path("/api/v1/metrics/{namespace}/{metric}").HandlerFunc(returnMetric)

		m.server = &http.Server{
			Addr:    ":9999",
			Handler: router,
		}

		go func() {
			err := m.server.ListenAndServe()
			if err != nil {
				klog.Errorf("could not start metrics server: %w", err)
			}
		}()

	} else if !serverEnabled && m.server != nil {
		if err := m.ShutDownServer(); err != nil {
			return fmt.Errorf("could not shut down metrics server: %w", err)
		}
	}
	return nil
}

func returnMetric(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	namespace := vars["namespace"]
	metric := vars["metric"]

	scheme := runtime.NewScheme()
	if err := metricsv1alpha1.AddToScheme(scheme); err != nil {
		fmt.Println("failed to add metrics to scheme: " + err.Error())
	}
	cl, err := ctrlclient.New(config.GetConfigOrDie(), ctrlclient.Options{Scheme: scheme})
	if err != nil {
		fmt.Println("failed to create client")
		os.Exit(1)
	}
	metricObj := metricsv1alpha1.Metric{}
	err = cl.Get(context.Background(), types.NamespacedName{Name: metric, Namespace: namespace}, &metricObj)
	if err != nil {
		fmt.Println("failed to list metrics" + err.Error())
	}

	data := map[string]string{
		"namespace": namespace,
		"metric":    metric,
		"value":     metricObj.Status.Value,
	}
	json.NewEncoder(w).Encode(data)
}
