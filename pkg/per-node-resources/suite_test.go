package pernoderesources

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	clientBuilder fake.ClientBuilder
)

var _ = BeforeSuite(func() {
	clientBuilder = *fake.
		NewClientBuilder().
		WithObjects()
})

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PerNodeResourcesPlugin")
}
