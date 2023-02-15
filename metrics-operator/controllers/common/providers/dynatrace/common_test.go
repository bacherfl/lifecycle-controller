package dynatrace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	metrics "github.com/keptn/lifecycle-toolkit/metrics-operator/apis/metrics/v1alpha2"
	"github.com/keptn/lifecycle-toolkit/metrics-operator/controllers/common/fake"
	"github.com/stretchr/testify/require"
)

func TestGetSecret_NoKeyDefined(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(dtpayload))
		require.Nil(t, err)
	}))
	defer svr.Close()
	fakeClient := fake.NewClient()

	p := metrics.KeptnMetricProvider{
		Spec: metrics.KeptnMetricProviderSpec{
			TargetServer: svr.URL,
		},
	}
	r, e := getDTSecret(context.TODO(), p, fakeClient)
	require.NotNil(t, e)
	require.ErrorIs(t, e, ErrSecretKeyRefNotDefined)
	require.Empty(t, r)
}

func TestGetSecret_NoSecret(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(dtpayload))
		require.Nil(t, err)
	}))
	defer svr.Close()
	fakeClient := fake.NewClient()

	p := metrics.KeptnMetricProvider{
		Spec: metrics.KeptnMetricProviderSpec{
			TargetServer: svr.URL,
		},
	}
	r, e := getDTSecret(context.TODO(), p, fakeClient)
	require.NotNil(t, e)
	require.ErrorIs(t, e, ErrSecretKeyRefNotDefined)
	require.Empty(t, r)
}

func TestGetSecret_NoTokenData(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(dtpayload))
		require.Nil(t, err)
	}))
	defer svr.Close()
	fakeClient := fake.NewClient()

	p := metrics.KeptnMetricProvider{
		Spec: metrics.KeptnMetricProviderSpec{
			TargetServer: svr.URL,
		},
	}
	r, e := getDTSecret(context.TODO(), p, fakeClient)
	require.NotNil(t, e)
	require.ErrorIs(t, e, ErrSecretKeyRefNotDefined)
	require.Empty(t, r)
}
