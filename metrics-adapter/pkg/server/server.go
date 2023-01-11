package server

import (
	"context"
	"fmt"
	"github.com/benbjohnson/clock"
	"github.com/open-feature/go-sdk/pkg/openfeature"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
	"net/http"
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

		m.server = &http.Server{Addr: ":9999"}

		http.Handle("/metrics", promhttp.Handler())
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
