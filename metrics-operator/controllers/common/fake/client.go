package fake

import (
	"github.com/keptn/lifecycle-toolkit/metrics-operator/apis/metrics/v1alpha2"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// NewClient returns a new controller-runtime fake Client configured with the Operator's scheme, and initialized with objs.
func NewClient(objs ...client.Object) client.Client {
	setupSchemes()
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(objs...).Build()
}

func setupSchemes() {
	runtime.Must(v1alpha2.AddToScheme(scheme.Scheme))
}
